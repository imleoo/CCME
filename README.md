# ChronoCascade Memory Engine (CCME) — Go

ChronoCascade Memory Engine 是一个基于生物学记忆机制（"分子计时器级联"）的分层记忆管理系统，专为 AI 智能体设计。它模拟了人类记忆的巩固过程，实现了从短期记忆到长期记忆的分层存储、自动晋升、重放巩固与智能遗忘。

引用论文：<https://www.nature.com/articles/s41586-025-09774-6>

> 本仓库现以 **Go 1.22+** 实现，并以 **Markdown 文件 + SQLite 索引** 作为持久化底层。
> 原 TypeScript 参考实现保留在 [`legacy/typescript/`](legacy/typescript/) 中。

## 1. 项目概述

### 业务目标

为 AI Agent 提供一个具备遗忘、巩固与关联能力的记忆后端，弥补传统向量数据库缺乏动态生命周期管理的不足。核心能力：

- **分层存储**：模拟人类的短期、中期、长期记忆。
- **智能晋升**：基于显著度 (Salience)、重复度 (Repetition)、奖励 (Reward) 自动晋升关键记忆。
- **动态遗忘**：根据生命周期、得分、容量自动清理低价值记忆。
- **可解释性**：记录每一次晋升、重放、遗忘、巩固的原因与得分。
- **会话+画像维度**：每条事件可归属 `(UserID, SessionID)`，并通过 `UserProfile`、`SessionContext`、`SessionSummary`、`ChatMessage` 四类侧面文档承载对话状态、用户画像、滚动摘要、审计日志。
- **`context.Context` 全程**：所有公开 API 接受 ctx，支持取消和超时；提供 `IngestAsync` 后台落库。

### 技术栈

- **语言**：Go 1.22+
- **存储底层**：Markdown 文件 (YAML frontmatter + body) + SQLite (`modernc.org/sqlite`，纯 Go 实现，无 cgo)
- **依赖**：`google/uuid`、`gopkg.in/yaml.v3`
- **测试**：Go 标准 `testing` 包

### 持久化布局

```
memory/
├── index.db                              # SQLite 索引
├── l0/<uuid>.md                          # Layer 0 短期记忆事件
├── l1/<uuid>.md                          # Layer 1 中期记忆事件
├── l2/<uuid>.md                          # Layer 2 长期记忆事件
├── schemas/<id>.md                       # Layer 2 巩固后的 Schema
├── profiles/<user_id>.md                 # UserProfile（结构化用户画像）
├── sessions/<user_id>/<session_id>.md    # SessionContext（会话状态 + 对话历史）
└── summaries/<user_id>/<session_id>.md   # SessionSummary（滚动摘要，单文档替换）
```

`index.db` 内表：`events / tags / associations / schemas / schema_sources / user_profiles / session_contexts / session_summaries / chat_messages`。

每个 `.md` 文件以 YAML frontmatter 保存结构化字段（id、scores、history、vector 等），文件正文保留事件原始内容，便于人类直接查阅或被其他工具索引（Obsidian 等）。`chat_messages` 作为高频 append-only 审计日志只入 SQLite，不落 MD。

## 2. 业务流程

```
RawEvent
   │
   ▼
EventEncoder ─ 计算 salience / 编码向量
   │
   ▼
ShortTermBuffer (L0) ─ 重复检测、衰减
   │              ▲
   │ replay       │
   ▼              │
CascadeGates  → 晋升 (α·salience + β·repeat + γ·reward ≥ θ)
   │
   ▼
MidTermStore (L1) ─ 关联图、结构化支持
   │
   ▼ replay + 巩固时间
CascadeGates → 晋升 (layer1 + centrality + replay + stability)
   │
   ▼
LongTermStore (L2) ─ Schema 巩固
   │
   ▼
ForgettingService ─ 过期 / 低分 / 容量上限 / 低效用 清理
```

## 3. 目录结构

```
.
├── cmd/ccme/main.go              # 命令行 demo
├── internal/
│   ├── config/                   # 默认参数 (Tau、阈值、衰减率…)
│   ├── core/                     # 主控制器
│   │   ├── system.go             # CascadeMemorySystem facade
│   │   ├── encoder.go            # EventEncoder
│   │   ├── gates.go              # CascadeGates
│   │   ├── replay.go             # ReplayWorker
│   │   ├── forgetting.go         # ForgettingService
│   │   ├── logger.go             # ExplainabilityLogger
│   │   └── system_test.go        # 冒烟测试
│   ├── storage/                  # 三层存储 + MD + SQLite
│   │   ├── markdown.go           # .md 编解码 (YAML frontmatter)
│   │   ├── sqlite.go             # SQLite 索引 schema + 查询
│   │   ├── store.go              # 通用接口与排序逻辑
│   │   ├── shortterm.go          # Layer 0
│   │   ├── midterm.go            # Layer 1 (含关联图)
│   │   └── longterm.go           # Layer 2 (含 Schema)
│   ├── types/                    # 共享数据结构
│   └── util/                     # 向量 / 时间工具
└── legacy/typescript/            # 旧 TS 实现归档
```

## 4. 快速上手

### 环境要求

- Go 1.22 或更高版本

### 运行 demo

```bash
go run ./cmd/ccme -reset -dir ./memory
```

输出示例：

```
=== ChronoCascade Memory Engine (Go) ===
[ingest] id=... layer=short-term salience=0.780
[maintenance] L0->L1=0 L1->L2=0 replays=5 pruned=0
[retrieve tag=AI] hits=2
[retrieve ctx=study] hits=2
[stats]
{ "layer0": { "size": 5, ... }, ... }
```

### 运行测试

```bash
go test ./...
```

### 编译二进制

```bash
go build -o bin/ccme ./cmd/ccme
```

## 5. 编程接口

### 5.1 Cascade 事件层

```go
import (
    "context"
    "chronocascade/internal/config"
    "chronocascade/internal/core"
    "chronocascade/internal/types"
)

ctx := context.Background()
cfg := config.Default()
cfg.Storage.BaseDir = "./memory"
cfg.Storage.SQLitePath = "./memory/index.db"

sys, err := core.New(ctx, cfg)
if err != nil { panic(err) }
defer sys.Close()

// 写入记忆（带 user / session 归属）
reward := 0.8
e, _ := sys.Ingest(ctx, types.RawEvent{
    Content:   "User prefers dark mode",
    Source:    "user_settings",
    ContextID: "user_123",
    UserID:    "alice",
    SessionID: "sess_1",
    Tags:      []string{"preference", "ui"},
    Reward:    &reward,
})

// 异步写入
ch := sys.IngestAsync(ctx, types.RawEvent{Content: "...", UserID: "alice"})
result := <-ch
_ = result.Event

// 维护周期
replay, forget, _ := sys.RunMaintenanceCycle(ctx)
_ = replay; _ = forget

// 用户隔离检索
results, _ := sys.Retrieve(ctx, types.RetrievalQuery{
    UserID: "alice",
    Tags:   []string{"preference"},
    TopK:   5,
})
for _, r := range results {
    println(r.Event.ID, r.Event.LayerState.String())
}

_, _ = sys.DeleteEvent(ctx, e.ID)
```

### 5.2 会话状态 / 用户画像 / 摘要 / 审计

```go
// 会话上下文（短期对话状态）
_ = sys.WriteSessionContext(ctx, &types.SessionContext{
    UserID: "alice", SessionID: "sess_1",
    LastAgent: "ui_tracker", TurnCount: 6,
    History: []types.Message{
        {Role: "user", Content: "switch to dark mode"},
        {Role: "assistant", Content: "Done."},
    },
})

// 结构化用户画像（中期持久）
_ = sys.WriteUserProfile(ctx, &types.UserProfile{
    UserID: "alice", DisplayName: "Alice",
    CommunicationStyle: "concise",
    Patterns: []types.ProfilePattern{
        {ID: "p1", Name: "dark mode preference", Confidence: 0.9},
    },
    Preferences: &types.ProfilePreferences{Tone: "warm"},
    ActivePlan:  &types.ProfileActivePlan{Goal: "...", Status: "in_progress"},
})

// 滚动摘要（替换式 upsert）
_ = sys.UpsertSessionSummary(ctx, &types.SessionSummary{
    UserID: "alice", SessionID: "sess_1",
    TurnRangeStart: 1, TurnRangeEnd: 6,
    Summary: "User reaffirmed dark mode + vegetarian preferences",
})

// 审计日志（不参与召回）
_ = sys.WriteChatMessage(ctx, &types.ChatMessage{
    ID: uuid.NewString(), UserID: "alice", SessionID: "sess_1",
    TurnIndex: 1, Role: "user", Content: "switch to dark mode",
})
recent, _ := sys.ReadRecentChatMessages(ctx, "alice", "sess_1", 20)
_ = recent
```

### 5.3 Manager 接口

`*core.CascadeMemorySystem` 实现了 `types.Manager`，按此接口编程便于替换 mock：

```go
var mgr types.Manager = sys
profile, _ := mgr.ReadUserProfile(ctx, "alice")
_ = profile
```

## 6. 核心算法

| 模块 | 公式 / 策略 |
|------|------------|
| 晋升 L0→L1 | `α·salience + β·repeat + γ·reward ≥ 0.7`（默认 α=0.4 β=0.3 γ=0.3） |
| 晋升 L1→L2 | `0.3·layer1 + 0.3·centrality + 0.2·replay + 0.2·stability ≥ 0.8` |
| 衰减 | `score_new = score_old × exp(−rate × Δt)` |
| 检索 | SQLite filter (layer/context/tag) + 内存 cosine 相似度 + topK |
| 重复检测 | L0 内归一化向量点积 ≥ 0.85 且时间窗 ≤ `RepeatWindow` |
| Schema 巩固 | L2 内向量聚类 (阈值 0.8) + 聚类规模 ≥ 3 |
| 用户隔离 | 检索/关联/重复检测均按 UserID 范围执行（空 UserID = 共享） |
| 滚动摘要 | `UpsertSessionSummary` 删除该 session 所有旧摘要后插入新摘要 |

## 7. 向量检索说明

当前向量排序为「**SQLite 候选过滤 → Go 内存暴力 cosine → topK**」，适合 <10 万条规模。SQLite 中以 BLOB 形式存储 float64 序列化向量。

如需扩展到更大规模，预留如下升级路径：

1. **`sqlite-vec` 扩展**：引入 `mattn/go-sqlite3` + cgo + sqlite-vec `.dylib`，把 `Index.Search` 改为 `vec_search` KNN。
2. **外置 ANN**：把 `storage.Index` 替换为 Faiss / Milvus 客户端。

由于这两条路径都需要外部依赖，本仓库默认使用纯 Go 实现，开箱即跑。

## 8. 默认配置

| 参数 | 值 |
|-----|----|
| Layer 0 / 1 / 2 lifespan (`Tau`) | 1 天 / 7 天 / 30 天 |
| 晋升阈值 | 0.7 / 0.8 |
| 衰减率 (per second) | 2e-5 / 5e-6 / 1e-6 |
| 容量上限 | 10000 / 5000 / 1000 |
| 向量维度 | 384 |
| 重复检测窗口 | 1 小时 |
| 重放周期 | 1 小时 |

可在 `internal/config/config.go` 中调整 `Default()` 返回值，或在调用 `core.New` 前自行修改 `config.Config`。

## 9. 历史与迁移说明

- `legacy/typescript/` 保留原 TS 实现（含 Redis / Postgres 持久化层），可作为业务逻辑对照参考。
- Go 实现等价覆盖了 TS 版 `CascadeMemorySystem` 的全部公共 API，但持久化目标已从 Redis + Postgres 切换为 Markdown + SQLite。
- 默认参数与 TS 版完全一致，可与 TS 版结果交叉验证。
