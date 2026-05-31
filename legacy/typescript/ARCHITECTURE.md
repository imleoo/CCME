# ChronoCascade Memory Engine - Project Structure

```
chronocascade-memory-engine/
├── src/
│   ├── core/                      # Core components
│   │   ├── CascadeMemorySystem.ts # Main controller
│   │   ├── EventEncoder.ts        # Event encoder
│   │   ├── CascadeGates.ts        # Cascade gates
│   │   ├── ReplayWorker.ts        # Replay consolidation worker
│   │   ├── ForgettingService.ts   # Forgetting service
│   │   └── ExplainabilityLogger.ts # Explainability logger
│   │
│   ├── storage/                   # Storage layers
│   │   ├── MemoryStore.ts         # Base storage
│   │   ├── ShortTermBuffer.ts     # Short-term buffer (Layer 0)
│   │   ├── MidTermStore.ts        # Mid-term storage (Layer 1)
│   │   └── LongTermStore.ts       # Long-term storage (Layer 2)
│   │
│   ├── types/                     # Type definitions
│   │   └── index.ts               # Core types
│   │
│   ├── config/                    # Configuration
│   │   └── constants.ts           # System constants
│   │
│   ├── utils/                     # Utility functions
│   │   ├── vector.ts              # Vector operations
│   │   └── time.ts                # Time utilities
│   │
│   ├── __tests__/                 # Test files
│   │   └── CascadeMemorySystem.test.ts
│   │
│   ├── examples.ts                # Usage examples
│   └── index.ts                   # Entry file
│
├── dist/                          # Build output
├── package.json
├── tsconfig.json
├── jest.config.js
└── README.md
```

## Module Descriptions

### Core Components (src/core/)

**CascadeMemorySystem.ts**
- Main controller integrating all subsystems
- Provides unified API interface
- Manages event lifecycle

**EventEncoder.ts**
- Encodes raw events into standard Event objects
- Generates vector embeddings (simplified implementation)
- Computes initial salience scores

**CascadeGates.ts**
- Implements hierarchical promotion logic
- Evaluates whether events meet promotion criteria
- Computes promotion scores based on multiple factors

**ReplayWorker.ts**
- Periodically replays important memories
- Executes inter-layer promotions
- Applies time decay

**ForgettingService.ts**
- Garbage collection and pruning
- Deletes low-value memories based on various strategies
- Capacity management

**ExplainabilityLogger.ts**
- Records all promotion, forgetting, and replay operations
- Generates statistical summaries
- Supports auditing and debugging

### Storage Layers (src/storage/)

**MemoryStore.ts**
- Base storage interface and implementation
- Provides generic CRUD operations
- Supports vector similarity search

**ShortTermBuffer.ts**
- Layer 0 short-term buffer
- Repetition detection and counting
- Fast decay

**MidTermStore.ts**
- Layer 1 mid-term storage
- Builds inter-event associations (graph structure)
- Computes structural centrality

**LongTermStore.ts**
- Layer 2 long-term storage
- Schema integration and compression
- Very slow decay

### Type Definitions (src/types/)

Contains all core types:
- Event, EventMetadata
- LayerState enumeration
- PromotionReason, ForgettingReason
- RetrievalQuery, RetrievalResult
- And more

### Configuration (src/config/)

System constants and default configurations:
- Time constants (TAU)
- Promotion thresholds
- Decay rates
- Capacity limits
- Replay configuration

### Utility Functions (src/utils/)

**vector.ts**
- Cosine similarity computation
- Vector normalization
- Euclidean distance

**time.ts**
- Timestamp utilities
- Time difference calculation
- Expiration checking

## Data Flow

```
Raw Event (RawEvent)
    ↓
EventEncoder.encode()
    ↓
Event (with vector, scores)
    ↓
ShortTermBuffer.add()
    ↓
[Repetition detection] → Score enhancement
    ↓
ReplayWorker.executeReplayCycle()
    ↓
CascadeGates.shouldPromoteToLayer1()
    ↓
[Meets criteria] → MidTermStore.add()
    ↓
[Build associations] → Graph structure
    ↓
ReplayWorker (execute again)
    ↓
CascadeGates.shouldPromoteToLayer2()
    ↓
[Meets criteria] → LongTermStore.add()
    ↓
[Optional] → Schema integration
```

## Maintenance Cycle

```
runMaintenanceCycle()
    ├── applyDecay() - Apply decay to all layers
    ├── processLayer0Promotions() - Layer 0 → 1
    ├── processLayer1Promotions() - Layer 1 → 2
    ├── performReplayConsolidation() - Replay consolidation
    └── executeForgetCycle() - Forgetting and pruning
```
