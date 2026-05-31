import { Event, ForgettingReason } from '../types';
import { ShortTermBuffer } from '../storage/ShortTermBuffer';
import { MidTermStore } from '../storage/MidTermStore';
import { LongTermStore } from '../storage/LongTermStore';
import { now, timeDiffInSeconds } from '../utils/time';
import { DEFAULT_CONFIG } from '../config/constants';

/**
 * Forgetting Statistics
 */
export interface ForgettingStats {
  layer0Pruned: number;
  layer1Pruned: number;
  layer2Pruned: number;
  totalPruned: number;
  reasons: Map<ForgettingReason, number>;
}

/**
 * Forgetting/Pruning Service
 * Implements garbage collection based on hierarchy, score, age, and utility
 */
export class ForgettingService {
  private shortTermBuffer: ShortTermBuffer;
  private midTermStore: MidTermStore;
  private longTermStore: LongTermStore;
  private config = DEFAULT_CONFIG;
  
  constructor(
    shortTermBuffer: ShortTermBuffer,
    midTermStore: MidTermStore,
    longTermStore: LongTermStore
  ) {
    this.shortTermBuffer = shortTermBuffer;
    this.midTermStore = midTermStore;
    this.longTermStore = longTermStore;
  }
  
  /**
   * Execute complete forgetting cycle
   */
  async executeForgetCycle(): Promise<ForgettingStats> {
    const stats: ForgettingStats = {
      layer0Pruned: 0,
      layer1Pruned: 0,
      layer2Pruned: 0,
      totalPruned: 0,
      reasons: new Map()
    };
    
    // 1. Process Layer 0 forgetting
    stats.layer0Pruned = await this.pruneLayer0(stats.reasons);
    
    // 2. Process Layer 1 forgetting
    stats.layer1Pruned = await this.pruneLayer1(stats.reasons);
    
    // 3. Process Layer 2 forgetting
    stats.layer2Pruned = await this.pruneLayer2(stats.reasons);
    
    stats.totalPruned = stats.layer0Pruned + stats.layer1Pruned + stats.layer2Pruned;
    
    return stats;
  }
  
  /**
   * Prune Layer 0
   * - Expired events
   * - Low score events
   * - Capacity limit
   */
  private async pruneLayer0(reasons: Map<ForgettingReason, number>): Promise<number> {
    let pruned = 0;
    const toRemove: string[] = [];
    
    // 1. Remove expired events
    const expired = await this.shortTermBuffer.getExpiredEvents();
    for (const event of expired) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.EXPIRED);
      this.markAsPruned(event, ForgettingReason.EXPIRED);
    }
    
    // 2. Remove low score events
    const lowScoreThreshold = 0.1;
    const allEvents = await this.shortTermBuffer.getAll();
    const lowScoreEvents = allEvents.filter(e => {
      const score = e.scores.layer0Score || e.scores.rawSalience;
      return score < lowScoreThreshold && !toRemove.includes(e.id);
    });
    
    for (const event of lowScoreEvents) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.LOW_SCORE);
      this.markAsPruned(event, ForgettingReason.LOW_SCORE);
    }
    
    // 3. Capacity limit: if still over capacity, remove lowest scoring ones
    const capacity = this.config.CAPACITY.shortTerm;
    const currentSize = await this.shortTermBuffer.size();
    const excess = currentSize - toRemove.length - capacity;
    
    if (excess > 0) {
       // Filter out already marked for removal
       const candidates = allEvents.filter(e => !toRemove.includes(e.id));
       // Sort by score ascending
       candidates.sort((a, b) => {
         const scoreA = a.scores.layer0Score || a.scores.rawSalience;
         const scoreB = b.scores.layer0Score || b.scores.rawSalience;
         return scoreA - scoreB;
       });
       
       const toPrune = candidates.slice(0, excess);
       for (const event of toPrune) {
         toRemove.push(event.id);
         this.recordReason(reasons, ForgettingReason.CAPACITY);
         this.markAsPruned(event, ForgettingReason.CAPACITY);
       }
    }
    
    // Execute removal
    for (const id of toRemove) {
      if (await this.shortTermBuffer.remove(id)) {
        pruned++;
      }
    }
    
    return pruned;
  }
  
  /**
   * Prune Layer 1
   */
  private async pruneLayer1(reasons: Map<ForgettingReason, number>): Promise<number> {
    let pruned = 0;
    const toRemove: string[] = [];
    
    // 1. Remove expired events
    const expired = await this.midTermStore.getExpiredEvents();
    for (const event of expired) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.EXPIRED);
      this.markAsPruned(event, ForgettingReason.EXPIRED);
    }
    
    // 2. Remove low score and low centrality events
    const lowScoreThreshold = 0.2;
    const lowCentralityThreshold = 0.1;
    
    const allEvents = await this.midTermStore.getAll();

    const lowUtilityEvents = allEvents.filter(e => {
      if (toRemove.includes(e.id)) return false;
      
      const score = e.scores.layer1Score || e.scores.layer0Score || e.scores.rawSalience;
      const centrality = this.midTermStore.getCentrality(e.id);
      
      return score < lowScoreThreshold && centrality < lowCentralityThreshold;
    });
    
    for (const event of lowUtilityEvents) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.LOW_UTILITY);
      this.markAsPruned(event, ForgettingReason.LOW_UTILITY);
    }
    
    // 3. Capacity limit
    const capacity = this.config.CAPACITY.midTerm;
    const currentSize = await this.midTermStore.size();
    const excess = currentSize - toRemove.length - capacity;
    
    if (excess > 0) {
      const remaining = allEvents
        .filter(e => !toRemove.includes(e.id))
        .sort((a, b) => {
          const scoreA = a.scores.layer1Score || a.scores.layer0Score || a.scores.rawSalience;
          const scoreB = b.scores.layer1Score || b.scores.layer0Score || b.scores.rawSalience;
          const centralityA = this.midTermStore.getCentrality(a.id);
          const centralityB = this.midTermStore.getCentrality(b.id);
          
          return (scoreA + centralityA) - (scoreB + centralityB);
        })
        .slice(0, excess);
      
      for (const event of remaining) {
        toRemove.push(event.id);
        this.recordReason(reasons, ForgettingReason.CAPACITY);
        this.markAsPruned(event, ForgettingReason.CAPACITY);
      }
    }
    
    // Execute removal
    for (const id of toRemove) {
      if (await this.midTermStore.remove(id)) {
        pruned++;
      }
    }
    
    return pruned;
  }
  
  /**
   * Prune Layer 2
   */
  private async pruneLayer2(reasons: Map<ForgettingReason, number>): Promise<number> {
    let pruned = 0;
    const toRemove: string[] = [];
    
    // Layer 2 has very slow decay, but we can prune based on very low utility
    // or extreme capacity constraints
    
    const lowScoreThreshold = 0.05; // Very low threshold
    const allEvents = await this.longTermStore.getAll();
    
    const lowUtilityEvents = allEvents.filter(e => {
      const score = e.scores.layer2Score || e.scores.layer1Score || e.scores.rawSalience;
      return score < lowScoreThreshold;
    });
    
    for (const event of lowUtilityEvents) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.LOW_UTILITY);
      this.markAsPruned(event, ForgettingReason.LOW_UTILITY);
    }
    
    // Execute removal
    for (const id of toRemove) {
      if (await this.longTermStore.remove(id)) {
        pruned++;
      }
    }
    
    return pruned;
  }
  
  private recordReason(reasons: Map<ForgettingReason, number>, reason: ForgettingReason): void {
    reasons.set(reason, (reasons.get(reason) || 0) + 1);
  }
  
  private markAsPruned(event: Event, reason: ForgettingReason): void {
    event.history.push({
      action: 'pruned',
      ts: now(),
      reason: reason
    });
  }
  
  /**
   * Manual delete
   */
  async manualDelete(eventId: string): Promise<boolean> {
    let deleted = await this.shortTermBuffer.remove(eventId);
    if (!deleted) deleted = await this.midTermStore.remove(eventId);
    if (!deleted) deleted = await this.longTermStore.remove(eventId);
    return deleted;
  }
}
