# CLAUDE.md

Guidance for Claude Code working with this repository.

## Project Overview

**ChronoCascade Memory Engine (CCME)** is a bio-inspired three-layer cascaded memory system written in **Go 1.22+**. It persists every memory event as a Markdown file (YAML frontmatter + body) and uses **SQLite** as the index/query layer. The original TypeScript implementation (with Redis + Postgres backends) is archived under `legacy/typescript/` for reference only.

In addition to the time-decay cascade (L0/L1/L2 events), the engine carries four *session-side* surfaces inspired by the agent-OS memory system at `legacy/reference/wmgaid_nnyn_agent_os`:

- **SessionContext** — running state of one chat session (history + agents + turn state)
- **UserProfile** — structured long-term portrait (patterns, preferences, active plan)
- **SessionSummary** — rolling, replace-on-write summary of a session's history
- **ChatMessage** — append-only audit log (SQLite-only, not retrieval-bearing)

All public API methods accept `context.Context`. `*core.CascadeMemorySystem` implements the `types.Manager` interface.

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
├── index.db                            # SQLite (see below)
├── l0/<uuid>.md                        # Layer 0 short-term events
├── l1/<uuid>.md                        # Layer 1 mid-term events
├── l2/<uuid>.md                        # Layer 2 long-term events
├── schemas/<id>.md                     # Consolidated Layer 2 schemas
├── profiles/<user_id>.md               # UserProfile documents
├── sessions/<user_id>/<sid>.md         # SessionContext documents
└── summaries/<user_id>/<sid>.md        # SessionSummary documents
```

SQLite tables: `events`, `tags`, `associations`, `schemas`, `schema_sources`, `user_profiles`, `session_contexts`, `session_summaries`, `chat_messages`.

Each markdown file owns the canonical event state (frontmatter for fields, body for content). SQLite is rebuilt-from-disk-friendly: it caches hot fields, vectors (as BLOB), tag JOINs, and the association graph.

### Code Layout
```
cmd/ccme/main.go                    # CLI demo runner
internal/
├── config/config.go                # Default tuning knobs
├── core/
│   ├── system.go                   # CascadeMemorySystem facade (Manager impl)
│   ├── encoder.go                  # EventEncoder
│   ├── gates.go                    # Promotion decisions
│   ├── replay.go                   # Decay + promote + replay worker
│   ├── forgetting.go               # Prune policies
│   ├── logger.go                   # ExplainabilityLogger
│   └── system_test.go              # Smoke tests
├── storage/
│   ├── markdown.go                 # Event MD codec + file IO
│   ├── sqlite.go                   # Index schema + queries (ctx-aware)
│   ├── store.go                    # Shared interface + ranking helpers
│   ├── shortterm.go                # Layer 0 store
│   ├── midterm.go                  # Layer 1 store + association graph
│   ├── longterm.go                 # Layer 2 store + schema consolidation
│   ├── profile.go                  # UserProfile MD+SQLite store
│   ├── session.go                  # SessionContext + SessionSummary store
│   └── chatlog.go                  # Append-only chat audit log
├── types/
│   ├── types.go                    # Event / Scores / RetrievalQuery
│   ├── session.go                  # Message, SessionContext, SessionSummary
│   ├── profile.go                  # UserProfile + Pattern/Preferences/Plan
│   ├── chat.go                     # ChatMessage
│   └── manager.go                  # Manager interface + RawEvent + IngestResult
└── util/{vector.go, time.go}       # Vector math, clock abstraction
```

### Key Algorithms
- **L0→L1 promotion**: `α·salience + β·repeat + γ·reward ≥ 0.7` (weights 0.4 / 0.3 / 0.3).
- **L1→L2 promotion**: `0.3·layer1 + 0.3·centrality + 0.2·replay + 0.2·stability ≥ 0.8`.
- **Decay**: `score = score × exp(−rate × Δt)`. L1 also adds `0.2 × centrality` as structural boost.
- **Retrieval**: SQLite filters by layer/context/user/session/tags; cosine ranking done in-memory over float64 vectors. User isolation is enforced when `RetrievalQuery.UserID` is set.
- **Repetition detection**: on L0 insert, dot-product against unit-normalised vectors ≥ 0.85 within `RepeatWindow` and within the same `UserID` reinforces instead of inserting.
- **Association graph**: built per-user — events with different `UserID` are never linked.
- **Schema consolidation**: L2 events with pairwise dot ≥ 0.8 cluster into a Schema; source events are then removed.
- **Rolling summary**: `UpsertSessionSummary` is delete-then-insert per `(user_id, session_id)` — only the latest rolling window survives.

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
- Markdown frontmatter is the source of truth for events, profiles, sessions, and summaries; SQLite rows are derivable. Never write data only to SQLite without persisting the matching `.md` file. Exception: `chat_messages` is SQLite-only by design.
- Every public method on `core.CascadeMemorySystem` accepts `context.Context` as its first argument. Background goroutines (e.g. `IngestAsync`) detach to `context.Background()` to avoid corrupting half-written state if the caller cancels.
- Program against `types.Manager` when you don't need cascade-internals — that's the contract that's intended to be substitutable.

## Extension Points
1. **Real embeddings**: replace `EventEncoder.simpleEmbedding` with a model-backed encoder.
2. **sqlite-vec KNN**: swap `Index.Search` + cosine ranking for native vector search. Requires `mattn/go-sqlite3` + cgo + the `vec0` extension; see README §7.
3. **External ANN**: implement an alternate `storage.Index` against Faiss/Milvus.
4. **Adaptive tuning**: feed `ExplainabilityLogger` summaries into a parameter optimiser.

## Notes for Contributors
- Maintain the biological metaphor in documentation/comments.
- `legacy/typescript/` is read-only reference; do not edit it.
- Build artifacts (`bin/`, `memory/`, `*.db`) should not be committed — add to `.gitignore` if needed.
