import { Event, LayerState, HistoryEntry } from '../types';
import { MemoryStore } from './MemoryStore';
import { DEFAULT_CONFIG } from '../config/constants';
import { timeDiffInSeconds, now } from '../utils/time';

/**
 * Short-term buffer (Layer 0)
 * Corresponds to fast, volatile storage of thalamus/CAMTA1
 * 
 * Characteristics:
 * - Fast write
 * - Time decay
 * - High capacity
 * - Short lifespan
 */
export class ShortTermBuffer extends MemoryStore {
  private tau: number; // Lifespan (seconds)
  private decayRate: number;
  
  constructor(
    capacity: number = DEFAULT_CONFIG.CAPACITY.shortTerm,
    tau: number = DEFAULT_CONFIG.TAU[0],
    decayRate: number = DEFAULT_CONFIG.DECAY_RATES[0]
  ) {
    super(capacity, LayerState.SHORT_TERM);
    this.tau = tau;
    this.decayRate = decayRate;
  }
  
  /**
   * Add event (override to support repetition detection)
   */
  add(event: Event): void {
    // Check if it's a repeated event
    const similar = this.findSimilarRecent(event);
    if (similar) {
      // Update repetition count
      similar.metadata.repetitionCount = (similar.metadata.repetitionCount || 0) + 1;
      similar.lastAccessedAt = now();
      
      // Enhance score (repetition signal)
      const boost = 0.1 * similar.metadata.repetitionCount;
      similar.scores.layer0Score = Math.min(
        (similar.scores.layer0Score || similar.scores.rawSalience) + boost,
        1.0
      );
      
      // Record history
      similar.history.push({
        action: 'seen',
        ts: now(),
        reason: 'repetition',
        score: similar.scores.layer0Score
      });
      
      return;
    }
    
    super.add(event);
  }
  
  /**
   * Find similar recent event
   */
  private findSimilarRecent(event: Event): Event | undefined {
    const threshold = 0.85; // Similarity threshold
    const windowMs = DEFAULT_CONFIG.REPEAT_WINDOW * 1000;
    const currentTime = now();
    
    for (const existing of this.events.values()) {
      const timeDiff = currentTime - existing.lastAccessedAt;
      if (timeDiff > windowMs) continue;
      
      // Check similarity
      const similarity = this.vectorSimilarity(event.vector, existing.vector);
      if (similarity >= threshold) {
        return existing;
      }
    }
    
    return undefined;
  }
  
  /**
   * Simple vector similarity calculation
   */
  private vectorSimilarity(v1: number[], v2: number[]): number {
    if (v1.length !== v2.length) return 0;
    
    let dotProduct = 0;
    for (let i = 0; i < v1.length; i++) {
      dotProduct += v1[i] * v2[i];
    }
    return dotProduct; // Assume vectors are normalized
  }
  
  /**
   * Apply time decay
   */
  applyDecay(currentTime: number = now()): void {
    for (const event of this.events.values()) {
      const ageSeconds = timeDiffInSeconds(event.lastAccessedAt);
      const decayFactor = Math.exp(-this.decayRate * ageSeconds);
      
      event.scores.layer0Score = (event.scores.layer0Score || event.scores.rawSalience) * decayFactor;
      
      // Record decay
      event.history.push({
        action: 'decayed',
        ts: currentTime,
        score: event.scores.layer0Score
      });
    }
  }
  
  /**
   * Get expired events
   */
  getExpiredEvents(): Event[] {
    const currentTime = now();
    const expired: Event[] = [];
    
    for (const event of this.events.values()) {
      const ageSeconds = timeDiffInSeconds(event.createdAt);
      if (ageSeconds > this.tau) {
        expired.push(event);
      }
    }
    
    return expired;
  }
  
  /**
   * Get high score events (promotion candidates)
   */
  getHighScoreEvents(threshold: number): Event[] {
    return this.getAll().filter(e => {
      const score = e.scores.layer0Score || e.scores.rawSalience;
      return score >= threshold;
    });
  }
  
  /**
   * Get repeated events (promotion candidates)
   */
  getRepeatedEvents(minRepetitions: number = 2): Event[] {
    return this.getAll().filter(e => 
      (e.metadata.repetitionCount || 0) >= minRepetitions
    );
  }
}
