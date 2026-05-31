/**
 * Core type definitions
 * Maps the hierarchical structure of biological memory systems
 */

/**
 * Memory layer (Corresponds to different stages of biological memory)
 * 0: Short-term buffer (Thalamus/CAMTA1)
 * 1: Mid-term storage (TCF4/Structural support)
 * 2: Long-term storage (Anterior cingulate/ASH1L/Chromatin remodeling)
 */
export enum LayerState {
  SHORT_TERM = 0,  // Fast, volatile
  MID_TERM = 1,    // Structured, connectivity enhanced
  LONG_TERM = 2    // Persistent, compressed, schemaized
}

/**
 * Event metadata
 */
export interface EventMetadata {
  ts: number;              // Timestamp (milliseconds)
  source: string;          // Event source
  contextId: string;       // Context ID
  reward?: number;         // Task reward/user rating (optional)
  tags: string[];          // Tags
  repetitionCount?: number; // Repetition count (for detecting repetition signals)
}

/**
 * History entry
 */
export interface HistoryEntry {
  action: 'seen' | 'promoted' | 'decayed' | 'replayed' | 'pruned';
  ts: number;
  toLayer?: LayerState;
  reason?: string;
  score?: number;
}

/**
 * Score records
 */
export interface Scores {
  rawSalience: number;    // Raw salience
  layer0Score?: number;   // Layer 0 score
  layer1Score?: number;   // Layer 1 score
  layer2Score?: number;   // Layer 2 score
}

/**
 * Event (core data structure)
 * Corresponds to the basic unit of biological memory
 */
export interface Event {
  id: string;                    // UUID
  vector: number[];              // Embedding vector
  metadata: EventMetadata;       // Metadata
  layerState: LayerState;        // Current layer
  scores: Scores;                // Layer scores
  history: HistoryEntry[];       // History trail
  createdAt: number;             // Creation time
  lastAccessedAt: number;        // Last access time
  promotionEligibleAt?: number;  // Promotion eligibility time (minimum wait period)
}

/**
 * Promotion reason (explainability)
 */
export enum PromotionReason {
  HIGH_SALIENCE = 'high_salience',
  REPEATED_EXPOSURE = 'repeated_exposure',
  TASK_REWARD = 'task_reward',
  STRUCTURAL_SUPPORT = 'structural_support',
  REPLAY_CONSOLIDATION = 'replay_consolidation',
  MANUAL_OVERRIDE = 'manual_override'
}

/**
 * Forgetting reason
 */
export enum ForgettingReason {
  LOW_SCORE = 'low_score',
  EXPIRED = 'expired',
  CAPACITY_LIMIT = 'capacity_limit',
  LOW_UTILITY = 'low_utility',
  MANUAL_DELETE = 'manual_delete'
}

/**
 * Retrieval query
 */
export interface RetrievalQuery {
  vector?: number[];          // Vector similarity query
  contextId?: string;         // Context filter
  tags?: string[];            // Tag filter
  minScore?: number;          // Minimum score
  layer?: LayerState;         // Specific layer
  topK?: number;              // Return count
}

/**
 * Retrieval result
 */
export interface RetrievalResult {
  event: Event;
  similarity?: number;        // Similarity (if vector query)
  retrievalReason: string;    // Retrieval reason
}
