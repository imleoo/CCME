import { Event, LayerState } from '../types';
import { MemoryStore } from './MemoryStore';
import { DEFAULT_CONFIG } from '../config/constants';
import { timeDiffInSeconds, now } from '../utils/time';

/**
 * Mid-term storage (Layer 1)
 * Corresponds to TCF4 stage with structural support and connectivity enhancement
 * 
 * Characteristics:
 * - Structured storage
 * - Association links (graph structure)
 * - Supports replay consolidation
 * - Medium lifespan
 */
export class MidTermStore extends MemoryStore {
  private tau: number;
  private decayRate: number;
  private associations: Map<string, Set<string>>; // Inter-event associations
  
  constructor(
    capacity: number = DEFAULT_CONFIG.CAPACITY.midTerm,
    tau: number = DEFAULT_CONFIG.TAU[1],
    decayRate: number = DEFAULT_CONFIG.DECAY_RATES[1]
  ) {
    super(capacity, LayerState.MID_TERM);
    this.tau = tau;
    this.decayRate = decayRate;
    this.associations = new Map();
  }
  
  /**
   * Add event (build associations)
   */
  add(event: Event): void {
    super.add(event);
    
    // Initialize association set
    if (!this.associations.has(event.id)) {
      this.associations.set(event.id, new Set());
    }
    
    // Automatically build associations with similar events
    this.buildAssociations(event);
  }
  
  /**
   * Build inter-event associations
   * Based on context similarity, temporal proximity, vector similarity
   */
  private buildAssociations(event: Event): void {
    const threshold = 0.7;
    
    for (const existing of this.events.values()) {
      if (existing.id === event.id) continue;
      
      let shouldAssociate = false;
      
      // Same context
      if (event.metadata.contextId === existing.metadata.contextId) {
        shouldAssociate = true;
      }
      
      // Similar vectors
      const similarity = this.vectorSimilarity(event.vector, existing.vector);
      if (similarity >= threshold) {
        shouldAssociate = true;
      }
      
      // Shared tags
      const sharedTags = event.metadata.tags.filter(tag => 
        existing.metadata.tags.includes(tag)
      );
      if (sharedTags.length >= 2) {
        shouldAssociate = true;
      }
      
      if (shouldAssociate) {
        this.addAssociation(event.id, existing.id);
      }
    }
  }
  
  /**
   * Add association
   */
  addAssociation(eventId1: string, eventId2: string): void {
    if (!this.associations.has(eventId1)) {
      this.associations.set(eventId1, new Set());
    }
    if (!this.associations.has(eventId2)) {
      this.associations.set(eventId2, new Set());
    }
    
    this.associations.get(eventId1)!.add(eventId2);
    this.associations.get(eventId2)!.add(eventId1);
  }
  
  /**
   * Get associated events
   */
  getAssociatedEvents(eventId: string): Event[] {
    const associatedIds = this.associations.get(eventId);
    if (!associatedIds) return [];
    
    const events: Event[] = [];
    for (const id of associatedIds) {
      const event = this.events.get(id);
      if (event) events.push(event);
    }
    
    return events;
  }
  
  /**
   * Compute graph centrality (for evaluating structural importance)
   */
  getCentrality(eventId: string): number {
    const directConnections = this.associations.get(eventId)?.size || 0;
    const maxConnections = this.events.size - 1;
    
    if (maxConnections === 0) return 0;
    return directConnections / maxConnections;
  }
  
  /**
   * Apply decay (considering structural support)
   */
  applyDecay(currentTime: number = now()): void {
    for (const event of this.events.values()) {
      const ageSeconds = timeDiffInSeconds(event.lastAccessedAt);
      const decayFactor = Math.exp(-this.decayRate * ageSeconds);
      
      // Structural support can slow decay
      const centrality = this.getCentrality(event.id);
      const structuralBoost = centrality * 0.2; // Up to 20% protection
      
      const currentScore = event.scores.layer1Score || event.scores.layer0Score || event.scores.rawSalience;
      event.scores.layer1Score = currentScore * decayFactor + structuralBoost;
      
      event.history.push({
        action: 'decayed',
        ts: currentTime,
        score: event.scores.layer1Score,
        reason: `structural_boost: ${structuralBoost.toFixed(3)}`
      });
    }
  }
  
  /**
   * Remove event (also clean up associations)
   */
  remove(id: string): boolean {
    // Clean up associations
    const associations = this.associations.get(id);
    if (associations) {
      for (const associatedId of associations) {
        this.associations.get(associatedId)?.delete(id);
      }
      this.associations.delete(id);
    }
    
    return super.remove(id);
  }
  
  /**
   * 获取过期事件
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
   * Get high centrality events (promotion candidates)
   */
  getHighCentralityEvents(minCentrality: number = 0.3): Event[] {
    return this.getAll().filter(e => 
      this.getCentrality(e.id) >= minCentrality
    );
  }
  
  private vectorSimilarity(v1: number[], v2: number[]): number {
    if (v1.length !== v2.length) return 0;
    let dotProduct = 0;
    for (let i = 0; i < v1.length; i++) {
      dotProduct += v1[i] * v2[i];
    }
    return dotProduct;
  }
  
  /**
   * Get association statistics
   */
  getAssociationStats() {
    const totalAssociations = Array.from(this.associations.values())
      .reduce((sum, set) => sum + set.size, 0) / 2; // Divide by 2 because bidirectional
    
    return {
      totalEvents: this.events.size,
      totalAssociations,
      avgAssociationsPerEvent: this.events.size > 0 ? totalAssociations / this.events.size : 0
    };
  }
}
