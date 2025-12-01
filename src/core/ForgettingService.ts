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
  executeForgetCycle(): ForgettingStats {
    const stats: ForgettingStats = {
      layer0Pruned: 0,
      layer1Pruned: 0,
      layer2Pruned: 0,
      totalPruned: 0,
      reasons: new Map()
    };
    
    // 1. Process Layer 0 forgetting
    stats.layer0Pruned = this.pruneLayer0(stats.reasons);
    
    // 2. Process Layer 1 forgetting
    stats.layer1Pruned = this.pruneLayer1(stats.reasons);
    
    // 3. Process Layer 2 forgetting
    stats.layer2Pruned = this.pruneLayer2(stats.reasons);
    
    stats.totalPruned = stats.layer0Pruned + stats.layer1Pruned + stats.layer2Pruned;
    
    return stats;
  }
  
  /**
   * Prune Layer 0
   * - Expired events
   * - Low score events
   * - Capacity limit
   */
  private pruneLayer0(reasons: Map<ForgettingReason, number>): number {
    let pruned = 0;
    const toRemove: string[] = [];
    
    // 1. Remove expired events
    const expired = this.shortTermBuffer.getExpiredEvents();
    for (const event of expired) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.EXPIRED);
      this.markAsPruned(event, ForgettingReason.EXPIRED);
    }
    
    // 2. Remove low score events
    const lowScoreThreshold = 0.1;
    const lowScoreEvents = this.shortTermBuffer.getAll().filter(e => {
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
    const currentSize = this.shortTermBuffer.size();
    const excess = currentSize - toRemove.length - capacity;
    
    if (excess > 0) {
      const remaining = this.shortTermBuffer.getAll()
        .filter(e => !toRemove.includes(e.id))
        .sort((a, b) => {
          const scoreA = a.scores.layer0Score || a.scores.rawSalience;
          const scoreB = b.scores.layer0Score || b.scores.rawSalience;
          return scoreA - scoreB;
        })
        .slice(0, excess);
      
      for (const event of remaining) {
        toRemove.push(event.id);
        this.recordReason(reasons, ForgettingReason.CAPACITY_LIMIT);
        this.markAsPruned(event, ForgettingReason.CAPACITY_LIMIT);
      }
    }
    
    // Execute removal
    pruned = this.shortTermBuffer.removeBatch(toRemove);
    
    return pruned;
  }
  
  /**
   * Prune Layer 1
   */
  private pruneLayer1(reasons: Map<ForgettingReason, number>): number {
    let pruned = 0;
    const toRemove: string[] = [];
    
    // 1. Remove expired events
    const expired = this.midTermStore.getExpiredEvents();
    for (const event of expired) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.EXPIRED);
      this.markAsPruned(event, ForgettingReason.EXPIRED);
    }
    
    // 2. Remove low score and low centrality events
    const lowScoreThreshold = 0.2;
    const lowCentralityThreshold = 0.1;
    
    const lowUtilityEvents = this.midTermStore.getAll().filter(e => {
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
    const currentSize = this.midTermStore.size();
    const excess = currentSize - toRemove.length - capacity;
    
    if (excess > 0) {
      const remaining = this.midTermStore.getAll()
        .filter(e => !toRemove.includes(e.id))
        .sort((a, b) => {
          const scoreA = a.scores.layer1Score || a.scores.layer0Score || a.scores.rawSalience;
          const scoreB = b.scores.layer1Score || b.scores.layer0Score || b.scores.rawSalience;
          const centralityA = this.midTermStore.getCentrality(a.id);
          const centralityB = this.midTermStore.getCentrality(b.id);
          
          // Combine score and centrality
          return (scoreA + centralityA) - (scoreB + centralityB);
        })
        .slice(0, excess);
      
      for (const event of remaining) {
        toRemove.push(event.id);
        this.recordReason(reasons, ForgettingReason.CAPACITY_LIMIT);
        this.markAsPruned(event, ForgettingReason.CAPACITY_LIMIT);
      }
    }
    
    pruned = this.midTermStore.removeBatch(toRemove);
    
    return pruned;
  }
  
  /**
   * Prune Layer 2
   * Long-term layer pruning is more conservative
   */
  private pruneLayer2(reasons: Map<ForgettingReason, number>): number {
    let pruned = 0;
    const toRemove: string[] = [];
    
    // Only prune Layer 2 in extreme cases
    // 1. Remove very low score events
    const veryLowScoreThreshold = 0.1;
    
    const veryLowScoreEvents = this.longTermStore.getAll().filter(e => {
      const score = e.scores.layer2Score || 
                   e.scores.layer1Score || 
                   e.scores.rawSalience;
      return score < veryLowScoreThreshold;
    });
    
    for (const event of veryLowScoreEvents) {
      toRemove.push(event.id);
      this.recordReason(reasons, ForgettingReason.LOW_SCORE);
      this.markAsPruned(event, ForgettingReason.LOW_SCORE);
    }
    
    // 2. Capacity limit (only when severely over capacity)
    const capacity = this.config.CAPACITY.longTerm;
    const currentSize = this.longTermStore.size();
    const excess = currentSize - toRemove.length - capacity;
    
    if (excess > 0) {
      const remaining = this.longTermStore.getAll()
        .filter(e => !toRemove.includes(e.id))
        .sort((a, b) => {
          const scoreA = a.scores.layer2Score || a.scores.layer1Score || a.scores.rawSalience;
          const scoreB = b.scores.layer2Score || b.scores.layer1Score || b.scores.rawSalience;
          return scoreA - scoreB;
        })
        .slice(0, excess);
      
      for (const event of remaining) {
        toRemove.push(event.id);
        this.recordReason(reasons, ForgettingReason.CAPACITY_LIMIT);
        this.markAsPruned(event, ForgettingReason.CAPACITY_LIMIT);
      }
    }
    
    pruned = this.longTermStore.removeBatch(toRemove);
    
    return pruned;
  }
  
  /**
   * Mark event as pruned
   */
  private markAsPruned(event: Event, reason: ForgettingReason): void {
    event.history.push({
      action: 'pruned',
      ts: now(),
      reason: reason
    });
  }
  
  /**
   * Record forgetting reason statistics
   */
  private recordReason(reasons: Map<ForgettingReason, number>, reason: ForgettingReason): void {
    const count = reasons.get(reason) || 0;
    reasons.set(reason, count + 1);
  }
  
  /**
   * Manually delete event
   */
  manualDelete(eventId: string): boolean {
    // Try to delete from each layer
    if (this.shortTermBuffer.remove(eventId)) return true;
    if (this.midTermStore.remove(eventId)) return true;
    if (this.longTermStore.remove(eventId)) return true;
    
    return false;
  }
}
