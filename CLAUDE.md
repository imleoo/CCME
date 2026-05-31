# CLAUDE.md

Guidance for Claude Code working with this repository.

## Project Overview

**ChronoCascade Memory Engine (CCME)** is a bio-inspired three-layer cascaded memory system written in **Go 1.22+**. It persists every memory event as a Markdown file (YAML frontmatter + body) and uses **SQLite** as the index/query layer. The original TypeScript implementation (with Redis + Postgres backends) is archived under `legacy/typescript/` for reference only.

## Development Commands

```bash
# Build CLI demo
go build -o bin/ccme ./cmd/ccme

# Run the demo (-reset wipes the store first)
go run ./cmd/ccme -reset -dir ./memory

# Run all tests
go test ./...

# Run a single test
go test ./internal/core -run TestRetrieveByContext -v

# Update / verify module graph
go mod tidy
```

## Architecture

### Persistence Layout
```
memory/
├── index.db        # SQLite: events, tags, associations, schemas
├── l0/<uuid>.md    # Layer 0 short-term events
├── l1/<uuid>.md    # Layer 1 mid-term events
├── l2/<uuid>.md    # Layer 2 long-term events
└── schemas/<id>.md # Consolidated Layer 2 schemas
```

Each markdown file owns the canonical event state (frontmatter for fields, body for content). SQLite is rebuilt-from-disk-friendly: it caches hot fields, vectors (as BLOB), tag JOINs, and the association graph.

### Code Layout
```
cmd/ccme/main.go                    # CLI demo runner
internal/
├── config/config.go                # Default tuning knobs
├── core/
│   ├── system.go                   # CascadeMemorySystem facade (public API)
│   ├── encoder.go                  # EventEncoder
│   ├── gates.go                    # Promotion decisions
│   ├── replay.go                   # Decay + promote + replay worker
│   ├── forgetting.go               # Prune policies
│   ├── logger.go                   # ExplainabilityLogger
│   └── system_test.go              # Smoke tests
├── storage/
│   ├── markdown.go                 # YAML frontmatter codec + file IO
│   ├── sqlite.go                   # Index schema, queries, vector blob
│   ├── store.go                    # Shared interface + ranking helpers
│   ├── shortterm.go                # Layer 0 store
│   ├── midterm.go                  # Layer 1 store + association graph
│   └── longterm.go                 # Layer 2 store + schema consolidation
├── types/types.go                  # Event / Scores / RetrievalQuery
└── util/{vector.go, time.go}       # Vector math, clock abstraction
```

### Key Algorithms
- **L0→L1 promotion**: `α·salience + β·repeat + γ·reward ≥ 0.7` (weights 0.4 / 0.3 / 0.3).
- **L1→L2 promotion**: `0.3·layer1 + 0.3·centrality + 0.2·replay + 0.2·stability ≥ 0.8`.
- **Decay**: `score = score × exp(−rate × Δt)`. L1 also adds `0.2 × centrality` as structural boost.
- **Retrieval**: SQLite filters by layer/context/tags, ranking by cosine done in-memory (brute force over float64 vectors).
- **Repetition detection**: on L0 insert, dot-product against unit-normalised vectors ≥ 0.85 within `RepeatWindow` reinforces instead of inserting.
- **Schema consolidation**: L2 events with pairwise dot ≥ 0.8 cluster into a Schema; source events are then removed.

## Default Configuration
- Tau: 1d / 7d / 30d
- Capacity: 10000 / 5000 / 1000
- Vector dim: 384
- Replay period: 1 hour; min wait time before promotion: 1 hour
- Storage base dir: `./memory`, index at `./memory/index.db`

All defaults live in `internal/config/config.go::Default()` and can be overridden before calling `core.New(cfg)`.

## Testing
Tests live in `internal/core/system_test.go` and use `t.TempDir()` for an isolated SQLite + markdown store per test. They cover the five smoke scenarios (single ingest, batch, retrieve by context/tag, stats, maintenance, delete). To add tests, place `_test.go` next to the package under test and use the `core_test` external package to exercise the public API.

## Conventions
- Each store layer satisfies `storage.Store`; new layers should do the same.
- All score mutations should round-trip through `Reindex` (skips repetition/association rebuild) or `Add` (triggers them).
- Time sourcing goes through `util.Clock` — tests can inject `util.FixedClock` if needed.
- Markdown frontmatter is the source of truth; SQLite rows are derivable. Never write data only to SQLite without persisting the matching `.md` file.

## Extension Points
1. **Real embeddings**: replace `EventEncoder.simpleEmbedding` with a model-backed encoder.
2. **sqlite-vec KNN**: swap `Index.Search` + cosine ranking for native vector search. Requires `mattn/go-sqlite3` + cgo + the `vec0` extension; see README §7.
3. **External ANN**: implement an alternate `storage.Index` against Faiss/Milvus.
4. **Adaptive tuning**: feed `ExplainabilityLogger` summaries into a parameter optimiser.

## Notes for Contributors
- Maintain the biological metaphor in documentation/comments.
- `legacy/typescript/` is read-only reference; do not edit it.
- Build artifacts (`bin/`, `memory/`, `*.db`) should not be committed — add to `.gitignore` if needed.
