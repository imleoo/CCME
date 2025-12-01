# ChronoCascade Memory Engine

Bio-inspired Temporal Cascaded Memory Engine

## Overview

ChronoCascade Memory Engine is a bio-inspired temporal cascaded memory engine that simulates the "molecular timer cascade" process from short-term to long-term memory. Based on the latest Nature research findings, the system combines **salience evaluation + repetition detection** with **cascade gates** to automatically determine memory promotion or forgetting.

## Biological Mapping

### Three-Layer Architecture

| Layer | Biological Correspondence | Time Scale | Characteristics |
|-------|--------------------------|------------|------------------|
| **Layer 0** | Thalamus/CAMTA1 | Hours~Days | Fast write, easy decay |
| **Layer 1** | TCF4/Structural Support | Days~Weeks | Structured, association enhancement |
| **Layer 2** | ASH1L/Chromatin Remodeling | Weeks~Months | Persistent, Schema formation |

### Core Mechanisms

1. **Cascade Gates**: Simulates the gradual transformation process of biological memory
2. **Repetition**: Repeated events gain higher promotion priority
3. **Replay**: Periodic replay of important memories to enhance retention
4. **Decay**: Each layer decays at different rates
5. **Structural Support**: Mid-term layer establishes associations between events

## Core Features

✅ **Three-Layer Hierarchical Storage**: Separation of short-term, mid-term, and long-term memory  
✅ **Automatic Promotion Mechanism**: Based on multiple factors including salience, repetition, reward  
✅ **Replay Consolidation**: Periodic replay to enhance important memories  
✅ **Intelligent Forgetting**: Garbage collection based on score, age, and utility  
✅ **Explainability**: Complete promotion/forgetting logging system  
✅ **Vector Retrieval**: Supports semantic similarity search  
✅ **Schema Integration**: Automatic compression and abstraction of long-term memory  

## Quick Start

### Installation

```bash
npm install
```

### Basic Usage

```typescript
import { CascadeMemorySystem } from './src';

// Create ChronoCascade engine instance
const system = new CascadeMemorySystem();

// 1. Ingest event
const event = system.ingest({
  content: { message: 'User prefers dark mode' },
  source: 'user_interaction',
  contextId: 'user_123',
  tags: ['preference', 'ui'],
  reward: 0.8  // Optional: reward signal
});

// 2. Retrieve memories
const results = system.retrieve({
  contextId: 'user_123',
  tags: ['preference'],
  topK: 5
});

// 3. Run maintenance cycle (promotion + forgetting)
const stats = await system.runMaintenanceCycle();
console.log(`Promotions: ${stats.replay.layer0Promotions}`);
console.log(`Forgotten: ${stats.forgetting.totalPruned}`);

// 4. View system statistics
const systemStats = system.getStats();
console.log(`Total events: ${systemStats.totalEvents}`);
console.log(`Layer 0: ${systemStats.layer0.size}`);
console.log(`Layer 1: ${systemStats.layer1.size}`);
console.log(`Layer 2: ${systemStats.layer2.size}`);
```

## Architecture Design

```
┌─────────────────────────────────────────────────────┐
│           CascadeMemorySystem (Main Controller)      │
├─────────────────────────────────────────────────────┤
│  EventEncoder  │  ReplayWorker  │  ForgettingService │
└─────────────────────────────────────────────────────┘
                        ↓
        ┌───────────────┼───────────────┐
        ↓               ↓               ↓
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ Layer 0      │ │ Layer 1      │ │ Layer 2      │
│ ShortTerm    │→│ MidTerm      │→│ LongTerm     │
│ Buffer       │ │ Store        │ │ Store        │
├──────────────┤ ├──────────────┤ ├──────────────┤
│ - Fast write │ │ - Association│ │ - Schema     │
│ - Repeat det.│ │ - Centrality │ │ - Compression│
│ - Time decay │ │ - Structural │ │ - Slow decay │
└──────────────┘ └──────────────┘ └──────────────┘
```

## Key Parameters

### Time Constants (TAU)
```typescript
TAU: [
  24 * 3600,      // Layer 0: 1 day
  7 * 24 * 3600,  // Layer 1: 7 days
  30 * 24 * 3600  // Layer 2: 30 days
]
```

### Promotion Thresholds
```typescript
PROMO_THRESH: [
  0.7,  // Layer 0 → 1
  0.8   // Layer 1 → 2
]
```

### Salience Weights
```typescript
SALIENCE_WEIGHTS: {
  alpha: 0.4,  // Raw salience
  beta: 0.3,   // Repetition factor
  gamma: 0.3   // Reward signal
}
```

## API Documentation

### CascadeMemorySystem

#### Core Methods

- `ingest(raw: RawEvent): Event` - Ingest single event
- `ingestBatch(raws: RawEvent[]): Event[]` - Batch ingest
- `retrieve(query: RetrievalQuery): RetrievalResult[]` - Retrieve memories
- `runMaintenanceCycle()` - Run maintenance cycle
- `getStats(): SystemStats` - Get system statistics
- `getLogSummary(): StatsSummary` - Get log summary
- `deleteEvent(id: string): boolean` - Manually delete event
- `clear()` - Clear all data

### Event Structure

```typescript
interface Event {
  id: string;
  vector: number[];        // Embedding vector
  metadata: EventMetadata;
  layerState: LayerState;  // Current layer
  scores: Scores;          // Layer scores
  history: HistoryEntry[]; // History trail
  createdAt: number;
  lastAccessedAt: number;
}
```

### Retrieval Query

```typescript
interface RetrievalQuery {
  vector?: number[];      // Vector similarity query
  contextId?: string;     // Context filter
  tags?: string[];        // Tag filter
  minScore?: number;      // Minimum score
  layer?: LayerState;     // Specific layer
  topK?: number;          // Return count
}
```

## Testing

```bash
# Run all tests
npm test

# Run tests with watch
npm run test:watch
```

## Development

```bash
# Build
npm run build

# Development mode
npm run dev
```

## Use Cases

### 1. Conversational Assistant
User expresses preferences multiple times → From short-term to long-term, eventually becomes user profile schema

### 2. Task-based Agent
Discovers effective strategies across multiple tasks → Promotes memories and reuses in similar situations

### 3. Knowledge Management System
Important documents/knowledge points promoted to long-term storage through repeated access and high ratings

## Performance Metrics

- **Retention Efficiency**: Ratio of memories promoted to long-term layer / total ingested events
- **Retrieval Accuracy**: Relevance of retrieval results
- **Forgetting Precision**: Impact of deleted memories on performance
- **Storage Cost**: Capacity utilization of each layer

## Design Principles

1. **Biologically Inspired**: Strictly maps molecular mechanisms of biological memory formation
2. **Explainability**: Every promotion/forgetting has clear reasoning
3. **Efficiency First**: Maximizes long-term utility under cost constraints
4. **Tunable Parameters**: Supports online adjustment of strategies and thresholds

## Extension Points

- [ ] Real vector embedding model integration (BERT/Sentence Transformers)
- [ ] Distributed storage backend (Redis/PostgreSQL)
- [ ] ANN vector indexing (Faiss/Milvus)
- [ ] Real-time monitoring dashboard
- [ ] A/B testing framework
- [ ] Adaptive parameter tuning

## References

1. Nature (2024): "Thalamocortical transcriptional gates coordinate memory formation"
2. Medical Xpress: "How the brain decides what to remember"
3. ScienceDaily: "Why some memories last a lifetime"

## License

MIT

## Contributing

Issues and Pull Requests are welcome!
