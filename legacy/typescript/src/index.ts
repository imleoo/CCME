/**
 * ChronoCascade Memory Engine - Main Entry Point
 * Temporal Cascaded Memory Engine - Bio-inspired hierarchical memory system
 */

// Core system
export { CascadeMemorySystem, SystemStats } from './core/CascadeMemorySystem';

// Encoder
export { EventEncoder, RawEvent } from './core/EventEncoder';

// Storage layers
export { ShortTermBuffer } from './storage/ShortTermBuffer';
export { MidTermStore } from './storage/MidTermStore';
export { LongTermStore, SchemaEntry } from './storage/LongTermStore';
export { MemoryStore, IMemoryStore } from './storage/MemoryStore';

// Core components
export { CascadeGates, PromotionDecision } from './core/CascadeGates';
export { ReplayWorker, ReplayStats } from './core/ReplayWorker';
export { ForgettingService, ForgettingStats } from './core/ForgettingService';
export { ExplainabilityLogger, LogEntry, LogType, StatsSummary } from './core/ExplainabilityLogger';

// Types
export {
  Event,
  EventMetadata,
  LayerState,
  HistoryEntry,
  Scores,
  PromotionReason,
  ForgettingReason,
  RetrievalQuery,
  RetrievalResult
} from './types';

// Configuration
export { DEFAULT_CONFIG, SystemConfig } from './config/constants';

// Utilities
export { cosineSimilarity, randomVector, normalizeVector, euclideanDistance } from './utils/vector';
export { now, nowInSeconds, timeDiffInSeconds, isExpired, formatTimeDiff } from './utils/time';
