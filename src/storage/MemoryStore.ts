import { Event, LayerState, RetrievalQuery, RetrievalResult } from '../types';
import { cosineSimilarity } from '../utils/vector';
import { timeDiffInSeconds, now } from '../utils/time';

/**
 * Base memory storage interface
 * Common interface for storage at each layer
 */
export interface IMemoryStore {
  add(event: Event): void | Promise<void>;
  get(id: string): Event | undefined | Promise<Event | undefined>;
  getAll(): Event[] | Promise<Event[]>;
  remove(id: string): boolean | Promise<boolean>;
  size(): number | Promise<number>;
  clear(): void | Promise<void>;
  search(query: RetrievalQuery): RetrievalResult[] | Promise<RetrievalResult[]>;
}

/**
 * Memory storage base class
 * Provides common storage and retrieval functionality
 */
export class MemoryStore implements IMemoryStore {
  protected events: Map<string, Event>;
  protected capacity: number;
  protected layer: LayerState;
  
  constructor(capacity: number, layer: LayerState) {
    this.events = new Map();
    this.capacity = capacity;
    this.layer = layer;
  }
  
  /**
   * Add event
   */
  add(event: Event): void | Promise<void> {
    if (this.events.size >= this.capacity && !this.events.has(event.id)) {
      throw new Error(`Layer ${this.layer} capacity exceeded (${this.capacity})`);
    }
    
    event.lastAccessedAt = now();
    this.events.set(event.id, event);
  }
  
  /**
   * Get single event
   */
  get(id: string): Event | undefined | Promise<Event | undefined> {
    const event = this.events.get(id);
    if (event) {
      event.lastAccessedAt = now();
    }
    return event;
  }
  
  /**
   * Get all events
   */
  getAll(): Event[] | Promise<Event[]> {
    return Array.from(this.events.values());
  }
  
  /**
   * Remove event
   */
  remove(id: string): boolean | Promise<boolean> {
    return this.events.delete(id);
  }
  
  /**
   * Get current size
   */
  size(): number | Promise<number> {
    return this.events.size;
  }
  
  /**
   * Clear storage
   */
  clear(): void | Promise<void> {
    this.events.clear();
  }
  
  /**
   * Search events
   */
  search(query: RetrievalQuery): RetrievalResult[] | Promise<RetrievalResult[]> {
    let candidates = Array.from(this.events.values());
    
    // Filter: layer
    if (query.layer !== undefined) {
      candidates = candidates.filter(e => e.layerState === query.layer);
    }
    
    // Filter: context ID
    if (query.contextId) {
      candidates = candidates.filter(e => e.metadata.contextId === query.contextId);
    }
    
    // Filter: tags
    if (query.tags && query.tags.length > 0) {
      candidates = candidates.filter(e => 
        query.tags!.some(tag => e.metadata.tags.includes(tag))
      );
    }
    
    // Filter: minimum score
    if (query.minScore !== undefined) {
      candidates = candidates.filter(e => {
        const score = this.getCurrentScore(e);
        return score >= query.minScore!;
      });
    }
    
    // Vector similarity search
    let results: RetrievalResult[];
    if (query.vector) {
      results = candidates.map(event => {
        const similarity = cosineSimilarity(query.vector!, event.vector);
        return {
          event,
          similarity,
          retrievalReason: 'vector_similarity'
        };
      });
      
      // Sort by similarity descending
      results.sort((a, b) => (b.similarity || 0) - (a.similarity || 0));
    } else {
      // Sort by score descending
      results = candidates.map(event => ({
        event,
        retrievalReason: 'filter_match'
      }));
      
      results.sort((a, b) => 
        this.getCurrentScore(b.event) - this.getCurrentScore(a.event)
      );
    }
    
    // Return topK
    const k = query.topK || 10;
    return results.slice(0, k);
  }
  
  /**
   * Get current score of event
   */
  protected getCurrentScore(event: Event): number {
    switch (this.layer) {
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
   * Batch add
   */
  addBatch(events: Event[]): void {
    events.forEach(e => this.add(e));
  }
  
  /**
   * Batch remove
   */
  removeBatch(ids: string[]): number {
    let count = 0;
    ids.forEach(id => {
      if (this.remove(id)) count++;
    });
    return count;
  }
  
  /**
   * Get statistics
   */
  async getStats() {
    const events = await this.getAll();
    const size = await this.size();
    const scores = events.map((e: Event) => this.getCurrentScore(e));
    
    return {
      layer: this.layer,
      size: size,
      capacity: this.capacity,
      utilizationRate: size / this.capacity,
      avgScore: scores.length > 0 ? scores.reduce((a: number, b: number) => a + b, 0) / scores.length : 0,
      maxScore: scores.length > 0 ? Math.max(...scores) : 0,
      minScore: scores.length > 0 ? Math.min(...scores) : 0
    };
  }
}
