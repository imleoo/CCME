import { Event, RetrievalQuery, RetrievalResult, LayerState } from '../types';
import { EventEncoder, RawEvent } from './EventEncoder';
import { ShortTermBuffer } from '../storage/ShortTermBuffer';
import { MidTermStore } from '../storage/MidTermStore';
import { LongTermStore } from '../storage/LongTermStore';
import { ReplayWorker, ReplayStats } from './ReplayWorker';
import { ForgettingService, ForgettingStats } from './ForgettingService';
import { ExplainabilityLogger } from './ExplainabilityLogger';
import { SystemConfig, DEFAULT_CONFIG } from '../config/constants';

/**
 * System Statistics
 */
export interface SystemStats {
  layer0: {
    size: number;
    capacity: number;
    utilization: number;
  };
  layer1: {
    size: number;
    capacity: number;
    utilization: number;
    associations: number;
  };
  layer2: {
    size: number;
    capacity: number;
    utilization: number;
    schemas: number;
  };
  totalEvents: number;
  lastReplayTime: number;
}

/**
 * ChronoCascade Memory Engine
 * Temporal Cascaded Memory Engine - Main controller that integrates all components
 */
export class CascadeMemorySystem {
  private encoder: EventEncoder;
  private shortTermBuffer: ShortTermBuffer;
  private midTermStore: MidTermStore;
  private longTermStore: LongTermStore;
  private replayWorker: ReplayWorker;
  private forgettingService: ForgettingService;
  private logger: ExplainabilityLogger;
  
  private config: SystemConfig;
  
  constructor(config: Partial<SystemConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
    
    // Initialize components
    this.encoder = new EventEncoder(this.config.VECTOR_DIM);
    this.shortTermBuffer = new ShortTermBuffer(
      this.config.CAPACITY.shortTerm,
      this.config.TAU[0],
      this.config.DECAY_RATES[0]
    );
    this.midTermStore = new MidTermStore(
      this.config.CAPACITY.midTerm,
      this.config.TAU[1],
      this.config.DECAY_RATES[1]
    );
    this.longTermStore = new LongTermStore(
      this.config.CAPACITY.longTerm,
      this.config.TAU[2],
      this.config.DECAY_RATES[2]
    );
    
    this.replayWorker = new ReplayWorker(
      this.shortTermBuffer,
      this.midTermStore,
      this.longTermStore
    );
    
    this.forgettingService = new ForgettingService(
      this.shortTermBuffer,
      this.midTermStore,
      this.longTermStore
    );
    
    this.logger = new ExplainabilityLogger();
  }
  
  /**
   * Ingest raw event
   */
  ingest(raw: RawEvent): Event {
    const event = this.encoder.encode(raw);
    this.shortTermBuffer.add(event);
    return event;
  }
  
  /**
   * Batch ingest
   */
  ingestBatch(rawEvents: RawEvent[]): Event[] {
    const events = this.encoder.encodeBatch(rawEvents);
    this.shortTermBuffer.addBatch(events);
    return events;
  }
  
  /**
   * Retrieve memories
   */
  retrieve(query: RetrievalQuery): RetrievalResult[] {
    const results: RetrievalResult[] = [];
    
    // Retrieve from all layers
    if (query.layer === undefined || query.layer === LayerState.SHORT_TERM) {
      results.push(...this.shortTermBuffer.search(query));
    }
    
    if (query.layer === undefined || query.layer === LayerState.MID_TERM) {
      results.push(...this.midTermStore.search(query));
    }
    
    if (query.layer === undefined || query.layer === LayerState.LONG_TERM) {
      results.push(...this.longTermStore.search(query));
    }
    
    // Merge and sort
    results.sort((a, b) => {
      if (a.similarity !== undefined && b.similarity !== undefined) {
        return b.similarity - a.similarity;
      }
      return 0;
    });
    
    // Return topK
    const k = query.topK || 10;
    return results.slice(0, k);
  }
  
  /**
   * Manually trigger replay cycle
   */
  async runReplayCycle(): Promise<ReplayStats> {
    const stats = await this.replayWorker.executeReplayCycle();
    
    // Log to logger
    this.logger.logReplay(
      { id: 'system' } as Event,
      0,
      `Replay cycle completed. Promotions: L0->L1: ${stats.layer0Promotions}, L1->L2: ${stats.layer1Promotions}`
    );
    
    return stats;
  }
  
  /**
   * Manually trigger forgetting cycle
   */
  runForgetCycle(): ForgettingStats {
    const stats = this.forgettingService.executeForgetCycle();
    
    // Log to logger
    this.logger.logForgetting(
      'system',
      LayerState.SHORT_TERM,
      'low_score' as any,
      `Forget cycle completed. Total pruned: ${stats.totalPruned}`
    );
    
    return stats;
  }
  
  /**
   * Run complete maintenance cycle (replay + forgetting)
   */
  async runMaintenanceCycle(): Promise<{ replay: ReplayStats; forgetting: ForgettingStats }> {
    const replay = await this.runReplayCycle();
    const forgetting = this.runForgetCycle();
    
    return { replay, forgetting };
  }
  
  /**
   * Get system statistics
   */
  getStats(): SystemStats {
    const layer0Stats = this.shortTermBuffer.getStats();
    const layer1Stats = this.midTermStore.getStats();
    const layer2Stats = this.longTermStore.getExtendedStats();
    const layer1Associations = this.midTermStore.getAssociationStats();
    
    return {
      layer0: {
        size: layer0Stats.size,
        capacity: layer0Stats.capacity,
        utilization: layer0Stats.utilizationRate
      },
      layer1: {
        size: layer1Stats.size,
        capacity: layer1Stats.capacity,
        utilization: layer1Stats.utilizationRate,
        associations: layer1Associations.totalAssociations
      },
      layer2: {
        size: layer2Stats.size,
        capacity: layer2Stats.capacity,
        utilization: layer2Stats.utilizationRate,
        schemas: layer2Stats.schemaCount
      },
      totalEvents: layer0Stats.size + layer1Stats.size + layer2Stats.size,
      lastReplayTime: this.replayWorker.getNextReplayTime()
    };
  }
  
  /**
   * Get log summary
   */
  getLogSummary() {
    return this.logger.generateStatsSummary();
  }
  
  /**
   * Get event history
   */
  getEventHistory(eventId: string) {
    return this.logger.getEventLogs(eventId);
  }
  
  /**
   * Export system state
   */
  exportState() {
    return {
      stats: this.getStats(),
      logSummary: this.getLogSummary(),
      config: this.config
    };
  }
  
  /**
   * Manually delete event
   */
  deleteEvent(eventId: string): boolean {
    return this.forgettingService.manualDelete(eventId);
  }
  
  /**
   * Clear all data
   */
  clear(): void {
    this.shortTermBuffer.clear();
    this.midTermStore.clear();
    this.longTermStore.clear();
    this.logger.clearLogs();
  }
  
  /**
   * Get specific event
   */
  getEvent(eventId: string): Event | undefined {
    return this.shortTermBuffer.get(eventId) ||
           this.midTermStore.get(eventId) ||
           this.longTermStore.get(eventId);
  }
  
  /**
   * Auto consolidate long-term memory into Schema
   */
  consolidateLongTermMemory(): number {
    const schemas = this.longTermStore.autoConsolidate(3, 0.8);
    
    for (const schema of schemas) {
      this.logger.logConsolidation(
        schema.id,
        schema.consolidatedFrom,
        schema.summary
      );
    }
    
    return schemas.length;
  }
}
