import { Event, LayerState } from '../types';
import { DEFAULT_CONFIG } from '../config/constants';
import { now } from '../utils/time';
import { ShortTermBuffer } from '../storage/ShortTermBuffer';
import { MidTermStore } from '../storage/MidTermStore';
import { LongTermStore } from '../storage/LongTermStore';
import { CascadeGates } from './CascadeGates';

/**
 * Replay Statistics
 */
export interface ReplayStats {
  totalReplays: number;
  layer0Promotions: number;
  layer1Promotions: number;
  avgScoreBoost: number;
  duration: number;
}

/**
 * Replay/Consolidation Worker
 * Corresponds to the "consolidation window" and "transcriptional cascade" of biological memory systems
 * 
 * Responsibilities:
 * 1. Periodically replay memories
 * 2. Evaluate promotion candidates
 * 3. Execute layer transitions
 * 4. Apply decay
 */
export class ReplayWorker {
  private shortTermBuffer: ShortTermBuffer;
  private midTermStore: MidTermStore;
  private longTermStore: LongTermStore;
  private cascadeGates: CascadeGates;
  private config = DEFAULT_CONFIG;
  
  private lastReplayTime: number = 0;
  private isRunning: boolean = false;
  
  constructor(
    shortTermBuffer: ShortTermBuffer,
    midTermStore: MidTermStore,
    longTermStore: LongTermStore
  ) {
    this.shortTermBuffer = shortTermBuffer;
    this.midTermStore = midTermStore;
    this.longTermStore = longTermStore;
    this.cascadeGates = new CascadeGates();
  }
  
  /**
   * Execute complete replay cycle
   */
  async executeReplayCycle(): Promise<ReplayStats> {
    if (this.isRunning) {
      throw new Error('Replay cycle already running');
    }
    
    this.isRunning = true;
    const startTime = now();
    
    try {
      const stats: ReplayStats = {
        totalReplays: 0,
        layer0Promotions: 0,
        layer1Promotions: 0,
        avgScoreBoost: 0,
        duration: 0
      };
      
      // 1. Apply decay
      this.applyDecayToAllLayers();
      
      // 2. Process Layer 0 -> Layer 1 promotions
      const layer0Promotions = await this.processLayer0Promotions();
      stats.layer0Promotions = layer0Promotions;
      
      // 3. Process Layer 1 -> Layer 2 promotions
      const layer1Promotions = await this.processLayer1Promotions();
      stats.layer1Promotions = layer1Promotions;
      
      // 4. Execute replay consolidation
      const replayCount = await this.performReplayConsolidation();
      stats.totalReplays = replayCount;
      
      this.lastReplayTime = now();
      stats.duration = now() - startTime;
      
      return stats;
    } finally {
      this.isRunning = false;
    }
  }
  
  /**
   * Apply decay to all layers
   */
  private applyDecayToAllLayers(): void {
    const currentTime = now();
    
    this.shortTermBuffer.applyDecay(currentTime);
    this.midTermStore.applyDecay(currentTime);
    this.longTermStore.applyDecay(currentTime);
  }
  
  /**
   * Process Layer 0 -> Layer 1 promotions
   */
  private async processLayer0Promotions(): Promise<number> {
    const candidates = this.shortTermBuffer.getAll();
    
    const promotionCandidates = this.cascadeGates.evaluateCandidates(
      candidates,
      (event) => this.cascadeGates.shouldPromoteToLayer1(event, this.shortTermBuffer)
    );
    
    let promoted = 0;
    
    for (const { event, decision } of promotionCandidates) {
      try {
        // Execute promotion
        this.cascadeGates.promoteEvent(
          event,
          LayerState.SHORT_TERM,
          LayerState.MID_TERM,
          decision.reason,
          decision.score
        );
        
        // Remove from Layer 0 and add to Layer 1
        this.shortTermBuffer.remove(event.id);
        this.midTermStore.add(event);
        
        promoted++;
      } catch (error) {
        // Silently ignore promotion errors
      }
    }
    
    return promoted;
  }
  
  /**
   * Process Layer 1 -> Layer 2 promotions
   */
  private async processLayer1Promotions(): Promise<number> {
    const candidates = this.midTermStore.getAll();
    
    const promotionCandidates = this.cascadeGates.evaluateCandidates(
      candidates,
      (event) => this.cascadeGates.shouldPromoteToLayer2(event, this.midTermStore)
    );
    
    let promoted = 0;
    
    for (const { event, decision } of promotionCandidates) {
      try {
        // Execute promotion
        this.cascadeGates.promoteEvent(
          event,
          LayerState.MID_TERM,
          LayerState.LONG_TERM,
          decision.reason,
          decision.score
        );
        
        // Remove from Layer 1 and add to Layer 2
        this.midTermStore.remove(event.id);
        this.longTermStore.add(event);
        
        promoted++;
      } catch (error) {
        // Silently ignore promotion errors
      }
    }
    
    return promoted;
  }
  
  /**
   * Execute replay consolidation
   * Select important memories for "replay", enhance their scores
   */
  private async performReplayConsolidation(): Promise<number> {
    const batchSize = this.config.REPLAY.batchSize;
    const boost = this.config.REPLAY.consolidationBoost;
    
    let replayCount = 0;
    
    // Replay from Layer 0
    const layer0Events = this.selectReplayTargets(
      this.shortTermBuffer.getAll(),
      Math.floor(batchSize / 2)
    );
    
    for (const event of layer0Events) {
      this.replayEvent(event, boost);
      replayCount++;
    }
    
    // Replay from Layer 1
    const layer1Events = this.selectReplayTargets(
      this.midTermStore.getAll(),
      Math.floor(batchSize / 2)
    );
    
    for (const event of layer1Events) {
      this.replayEvent(event, boost);
      replayCount++;
    }
    
    return replayCount;
  }
  
  /**
   * Select replay targets
   * Priority: high score, repeated, rewarded events
   */
  private selectReplayTargets(events: Event[], count: number): Event[] {
    return events
      .sort((a, b) => {
        const scoreA = this.getEventScore(a);
        const scoreB = this.getEventScore(b);
        return scoreB - scoreA;
      })
      .slice(0, count);
  }
  
  /**
   * Replay single event
   */
  private replayEvent(event: Event, boost: number): void {
    // Enhance score
    switch (event.layerState) {
      case LayerState.SHORT_TERM:
        event.scores.layer0Score = Math.min(
          (event.scores.layer0Score || event.scores.rawSalience) + boost,
          1.0
        );
        break;
      case LayerState.MID_TERM:
        event.scores.layer1Score = Math.min(
          (event.scores.layer1Score || event.scores.layer0Score || event.scores.rawSalience) + boost,
          1.0
        );
        break;
      case LayerState.LONG_TERM:
        event.scores.layer2Score = Math.min(
          (event.scores.layer2Score || event.scores.layer1Score || event.scores.rawSalience) + boost,
          1.0
        );
        break;
    }
    
    // Record replay
    event.history.push({
      action: 'replayed',
      ts: now(),
      score: this.getEventScore(event),
      reason: 'consolidation_replay'
    });
    
    event.lastAccessedAt = now();
  }
  
  /**
   * Get current score of event
   */
  private getEventScore(event: Event): number {
    switch (event.layerState) {
      case LayerState.SHORT_TERM:
        return event.scores.layer0Score || event.scores.rawSalience;
      case LayerState.MID_TERM:
        return event.scores.layer1Score || event.scores.layer0Score || event.scores.rawSalience;
      case LayerState.LONG_TERM:
        return event.scores.layer2Score || event.scores.layer1Score || event.scores.rawSalience;
      default:
        return event.scores.rawSalience;
    }
  }
  
  /**
   * Get next replay recommended time
   */
  getNextReplayTime(): number {
    return this.lastReplayTime + this.config.REPLAY.frequency * 1000;
  }
  
  /**
   * Check if replay should be executed
   */
  shouldRunReplay(): boolean {
    if (this.isRunning) return false;
    return now() >= this.getNextReplayTime();
  }
}
