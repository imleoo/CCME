# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**ChronoCascade Memory Engine** is a bio-inspired temporal cascaded memory system that simulates the molecular timer cascade process from short-term to long-term memory. Based on neuroscience research, it implements a three-layer hierarchical storage architecture with automatic promotion, replay consolidation, and intelligent forgetting mechanisms.

## Development Commands

```bash
# Build TypeScript to dist/
npm run build

# Run with ts-node for development
npm run dev

# Run all tests
npm test

# Run tests in watch mode
npm run test:watch
```

## Architecture

### Biological Mapping
The system maps biological memory mechanisms to software components:
- **Layer 0 (Short-term)**: Thalamus/CAMTA1 - fast write, easy decay (`ShortTermBuffer`)
- **Layer 1 (Mid-term)**: TCF4/Structural support - associations, centrality (`MidTermStore`)
- **Layer 2 (Long-term)**: ASH1L/Chromatin remodeling - schemas, compression (`LongTermStore`)

### Core Components
```
src/
├── core/                      # Main system controllers
│   ├── CascadeMemorySystem.ts # Primary API interface
│   ├── EventEncoder.ts        # Event encoding and vector generation
│   ├── CascadeGates.ts        # Promotion decision logic
│   ├── ReplayWorker.ts        # Periodic replay consolidation
│   ├── ForgettingService.ts   # Garbage collection and pruning
│   └── ExplainabilityLogger.ts # Operation logging and statistics
├── storage/                   # Three-layer storage hierarchy
│   ├── MemoryStore.ts         # Base storage interface
│   ├── ShortTermBuffer.ts     # Layer 0: Fast buffer with repetition detection
│   ├── MidTermStore.ts        # Layer 1: Association graph building
│   └── LongTermStore.ts       # Layer 2: Schema integration
├── types/                     # TypeScript interfaces
├── config/constants.ts        # System parameters
└── utils/                     # Vector and time utilities
```

### Key Algorithms
- **Promotion Scoring**: `α × salience + β × repeat + γ × reward` (weights: 0.4, 0.3, 0.3)
- **Decay Formula**: `new_score = old_score × exp(-decay_rate × age)`
- **Vector Similarity**: Cosine similarity for semantic retrieval

### Data Flow
```
Raw Event → EventEncoder → ShortTermBuffer → [Repetition Detection]
    ↓
ReplayWorker → CascadeGates → Promotion Decisions
    ↓
MidTermStore → [Association Building] → LongTermStore → [Schema Integration]
```

## Configuration

### Time Constants (TAU)
- **Layer 0**: 24 hours (1 day)
- **Layer 1**: 7 days
- **Layer 2**: 30 days

### Promotion Thresholds
- **Layer 0 → 1**: 0.7
- **Layer 1 → 2**: 0.8

### Capacity Limits
- **Layer 0**: 10,000 entries
- **Layer 1**: 5,000 entries
- **Layer 2**: 1,000 entries

## Testing

Tests are located in `src/__tests__/` and use Jest with ts-jest. The test configuration includes coverage collection and runs all `.test.ts` files.

**Test Coverage Includes**:
- Event ingestion (single and batch)
- Retrieval by context and tags
- System statistics
- Maintenance cycle execution
- Event deletion

## TypeScript Configuration

- **Target**: ES2020
- **Module**: CommonJS
- **Output**: `dist/` directory with declaration files
- **Strict mode**: Enabled
- **Source maps**: Enabled for debugging

## Development Workflow

1. **Write code** in `src/` directory
2. **Test changes** with `npm test`
3. **Run examples** with `npm run dev`
4. **Build for production** with `npm run build`

## Key Design Patterns

1. **Object-Oriented Design**: Clear class responsibilities
2. **Dependency Injection**: Components injected via constructors
3. **Strategy Pattern**: Multiple forgetting strategies
4. **Factory Pattern**: Event encoding
5. **Observer Pattern**: Logging system

## API Usage

The primary interface is `CascadeMemorySystem` in `src/core/CascadeMemorySystem.ts`. Key methods:
- `ingest()` / `ingestBatch()` - Add events
- `retrieve()` - Search memories with vector similarity
- `runMaintenanceCycle()` - Execute promotion, replay, and forgetting
- `getStats()` - System statistics
- `getLogSummary()` - Explainability logs

## Extension Points

1. **Real vector embeddings**: Replace placeholder vectors with actual embedding models
2. **Persistent storage**: Add Redis/PostgreSQL backends
3. **ANN indexing**: Implement Faiss/Milvus for faster vector search
4. **Monitoring**: Add real-time dashboard for system metrics
5. **Adaptive tuning**: Implement reinforcement learning for parameter optimization

## Notes for Contributors

- Follow existing TypeScript patterns and naming conventions
- Maintain biological metaphor consistency in documentation
- Add tests for new functionality in `src/__tests__/`
- Update `ARCHITECTURE.md` for significant architectural changes
- Use the three-layer hierarchy appropriately when adding features