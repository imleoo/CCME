import { v4 as uuidv4 } from 'uuid';
import { Event, EventMetadata, LayerState, HistoryEntry } from '../types';
import { DEFAULT_CONFIG } from '../config/constants';
import { now } from '../utils/time';
import { randomVector, normalizeVector } from '../utils/vector';

/**
 * Raw event input
 */
export interface RawEvent {
  content: any;           // Event content (can be text, object, etc.)
  source: string;         // Source
  contextId: string;      // Context ID
  reward?: number;        // Reward signal
  tags?: string[];        // Tags
  vector?: number[];      // Optional: externally provided vector
}

/**
 * Event Encoder
 * Responsible for encoding raw events into standardized Event objects
 * Corresponds to the "perceptual encoding" stage of biological systems
 */
export class EventEncoder {
  private vectorDim: number;
  
  constructor(vectorDim: number = DEFAULT_CONFIG.VECTOR_DIM) {
    this.vectorDim = vectorDim;
  }
  
  /**
   * Encode raw event
   */
  encode(raw: RawEvent): Event {
    const timestamp = now();
    const vector = this.encodeVector(raw);
    const salience = this.computeSalience(raw);
    
    const metadata: EventMetadata = {
      ts: timestamp,
      source: raw.source,
      contextId: raw.contextId,
      reward: raw.reward,
      tags: raw.tags || [],
      repetitionCount: 0
    };
    
    const historyEntry: HistoryEntry = {
      action: 'seen',
      ts: timestamp,
      score: salience
    };
    
    const event: Event = {
      id: uuidv4(),
      vector,
      metadata,
      layerState: LayerState.SHORT_TERM,
      scores: {
        rawSalience: salience,
        layer0Score: salience
      },
      history: [historyEntry],
      createdAt: timestamp,
      lastAccessedAt: timestamp,
      promotionEligibleAt: timestamp + DEFAULT_CONFIG.REPLAY.minWaitTime * 1000
    };
    
    return event;
  }
  
  /**
   * Encode vector
   * In real applications, should use actual embedding models (e.g., BERT, Sentence Transformers)
   * Here provides simplified implementation
   */
  private encodeVector(raw: RawEvent): number[] {
    // If external vector provided, use it directly
    if (raw.vector && raw.vector.length === this.vectorDim) {
      return normalizeVector(raw.vector);
    }
    
    // Simplified implementation: generate pseudo vector based on content
    // In real applications should replace with actual embedding model
    return this.simpleEmbedding(raw.content);
  }
  
  /**
   * Simplified embedding implementation (for demonstration only)
   * In real applications should use actual embedding models
   */
  private simpleEmbedding(content: any): number[] {
    const str = JSON.stringify(content);
    const vector = new Array(this.vectorDim).fill(0);
    
    // Use simple hash method to generate vector
    for (let i = 0; i < str.length; i++) {
      const char = str.charCodeAt(i);
      const idx = char % this.vectorDim;
      vector[idx] += 1;
    }
    
    return normalizeVector(vector);
  }
  
  /**
   * Compute salience (importance)
   * Corresponds to "initial importance assessment" in biological systems
   * 
   * Considers:
   * - Reward signal strength
   * - Content complexity
   * - Emotional intensity (if applicable)
   */
  private computeSalience(raw: RawEvent): number {
    let salience = 0.5; // Base salience
    
    // Reward signal contribution
    if (raw.reward !== undefined) {
      salience += Math.min(Math.abs(raw.reward), 1) * 0.3;
    }
    
    // Content complexity contribution
    const complexity = this.estimateComplexity(raw.content);
    salience += complexity * 0.2;
    
    // Normalize to [0, 1]
    return Math.min(Math.max(salience, 0), 1);
  }
  
  /**
   * Estimate content complexity
   */
  private estimateComplexity(content: any): number {
    const str = JSON.stringify(content);
    const length = str.length;
    
    // Simple complexity estimation based on length
    if (length < 50) return 0.2;
    if (length < 200) return 0.5;
    if (length < 1000) return 0.8;
    return 1.0;
  }
  
  /**
   * Batch encoding
   */
  encodeBatch(rawEvents: RawEvent[]): Event[] {
    return rawEvents.map(raw => this.encode(raw));
  }
}
