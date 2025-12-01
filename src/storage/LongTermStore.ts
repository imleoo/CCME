import { Event, LayerState } from '../types';
import { MemoryStore } from './MemoryStore';
import { DEFAULT_CONFIG } from '../config/constants';
import { timeDiffInSeconds, now } from '../utils/time';

/**
 * Schema entry (compressed abstract memory)
 */
export interface SchemaEntry {
  id: string;
  summary: string;           // Abstract description
  consolidatedFrom: string[]; // Source event IDs that were merged
  vector: number[];           // Merged vector
  importance: number;         // Importance score
  createdAt: number;
  lastUpdatedAt: number;
}

/**
 * Long-term storage (Layer 2)
 * Corresponds to anterior cingulate/ASH1L stage with chromatin remodeling and persistence
 * 
 * Characteristics:
 * - Sparse storage
 * - Compression and abstraction (Schemaization)
 * - Very slow decay
 * - Permanent or very long lifespan
 */
export class LongTermStore extends MemoryStore {
  private tau: number;
  private decayRate: number;
  private schemas: Map<string, SchemaEntry>; // Schema storage
  
  constructor(
    capacity: number = DEFAULT_CONFIG.CAPACITY.longTerm,
    tau: number = DEFAULT_CONFIG.TAU[2],
    decayRate: number = DEFAULT_CONFIG.DECAY_RATES[2]
  ) {
    super(capacity, LayerState.LONG_TERM);
    this.tau = tau;
    this.decayRate = decayRate;
    this.schemas = new Map();
  }
  
  /**
   * Apply decay (very slow)
   */
  applyDecay(currentTime: number = now()): void {
    for (const event of this.events.values()) {
      const ageSeconds = timeDiffInSeconds(event.lastAccessedAt);
      const decayFactor = Math.exp(-this.decayRate * ageSeconds);
      
      const currentScore = event.scores.layer2Score || 
                          event.scores.layer1Score || 
                          event.scores.rawSalience;
      
      event.scores.layer2Score = currentScore * decayFactor;
      
      event.history.push({
        action: 'decayed',
        ts: currentTime,
        score: event.scores.layer2Score
      });
    }
  }
  
  /**
   * Merge similar events into Schema
   * Corresponds to "memory integration" in biology
   */
  consolidateToSchema(events: Event[]): SchemaEntry {
    if (events.length === 0) {
      throw new Error('Cannot consolidate empty event list');
    }
    
    // Calculate average vector
    const avgVector = this.computeAverageVector(events);
    
    // Generate summary
    const summary = this.generateSummary(events);
    
    // Calculate combined importance
    const importance = this.computeSchemaImportance(events);
    
    const schema: SchemaEntry = {
      id: `schema_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      summary,
      consolidatedFrom: events.map(e => e.id),
      vector: avgVector,
      importance,
      createdAt: now(),
      lastUpdatedAt: now()
    };
    
    this.schemas.set(schema.id, schema);
    
    // Remove merged events from event storage
    events.forEach(e => this.remove(e.id));
    
    return schema;
  }
  
  /**
   * Compute average vector
   */
  private computeAverageVector(events: Event[]): number[] {
    const dim = events[0].vector.length;
    const avgVector = new Array(dim).fill(0);
    
    for (const event of events) {
      for (let i = 0; i < dim; i++) {
        avgVector[i] += event.vector[i];
      }
    }
    
    for (let i = 0; i < dim; i++) {
      avgVector[i] /= events.length;
    }
    
    // Normalize
    const norm = Math.sqrt(avgVector.reduce((sum, v) => sum + v * v, 0));
    return avgVector.map(v => v / norm);
  }
  
  /**
   * Generate summary
   */
  private generateSummary(events: Event[]): string {
    // Extract common features
    const sources = new Set(events.map(e => e.metadata.source));
    const contexts = new Set(events.map(e => e.metadata.contextId));
    const allTags = events.flatMap(e => e.metadata.tags);
    const tagCounts = new Map<string, number>();
    
    allTags.forEach(tag => {
      tagCounts.set(tag, (tagCounts.get(tag) || 0) + 1);
    });
    
    const commonTags = Array.from(tagCounts.entries())
      .filter(([_, count]) => count >= events.length / 2)
      .map(([tag, _]) => tag);
    
    return `Consolidated ${events.length} events from ${sources.size} source(s), ` +
           `${contexts.size} context(s), common tags: [${commonTags.join(', ')}]`;
  }
  
  /**
   * Compute Schema importance
   */
  private computeSchemaImportance(events: Event[]): number {
    const scores = events.map(e => 
      e.scores.layer2Score || e.scores.layer1Score || e.scores.rawSalience
    );
    
    // Use combination of max score and average score
    const maxScore = Math.max(...scores);
    const avgScore = scores.reduce((a, b) => a + b, 0) / scores.length;
    
    return 0.6 * maxScore + 0.4 * avgScore;
  }
  
  /**
   * Get all Schemas
   */
  getSchemas(): SchemaEntry[] {
    return Array.from(this.schemas.values());
  }
  
  /**
   * Get Schema
   */
  getSchema(id: string): SchemaEntry | undefined {
    return this.schemas.get(id);
  }
  
  /**
   * Find similar Schemas
   */
  findSimilarSchemas(vector: number[], threshold: number = 0.7): SchemaEntry[] {
    const similar: SchemaEntry[] = [];
    
    for (const schema of this.schemas.values()) {
      const similarity = this.vectorSimilarity(vector, schema.vector);
      if (similarity >= threshold) {
        similar.push(schema);
      }
    }
    
    return similar.sort((a, b) => b.importance - a.importance);
  }
  
  /**
   * Auto consolidate: find similar events and merge into Schema
   */
  autoConsolidate(minGroupSize: number = 3, similarityThreshold: number = 0.8): SchemaEntry[] {
    const events = this.getAll();
    if (events.length < minGroupSize) return [];
    
    const groups = this.clusterSimilarEvents(events, similarityThreshold);
    const schemas: SchemaEntry[] = [];
    
    for (const group of groups) {
      if (group.length >= minGroupSize) {
        const schema = this.consolidateToSchema(group);
        schemas.push(schema);
      }
    }
    
    return schemas;
  }
  
  /**
   * Cluster similar events
   */
  private clusterSimilarEvents(events: Event[], threshold: number): Event[][] {
    const groups: Event[][] = [];
    const assigned = new Set<string>();
    
    for (const event of events) {
      if (assigned.has(event.id)) continue;
      
      const group = [event];
      assigned.add(event.id);
      
      for (const other of events) {
        if (assigned.has(other.id)) continue;
        
        const similarity = this.vectorSimilarity(event.vector, other.vector);
        if (similarity >= threshold) {
          group.push(other);
          assigned.add(other.id);
        }
      }
      
      groups.push(group);
    }
    
    return groups;
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
   * Get statistics
   */
  getExtendedStats() {
    const baseStats = super.getStats();
    
    let avgConsolidation = 0;
    if (this.schemas.size > 0) {
      const totalConsolidated = Array.from(this.schemas.values())
        .reduce((sum, schema) => sum + schema.consolidatedFrom.length, 0);
      avgConsolidation = totalConsolidated / this.schemas.size;
    }
    
    return {
      ...baseStats,
      schemaCount: this.schemas.size,
      totalMemoryUnits: this.events.size + this.schemas.size,
      avgConsolidationRatio: avgConsolidation
    };
  }
}