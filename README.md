# ChronoCascade Memory Engine (CCME)

ChronoCascade Memory Engine 是一个基于生物学记忆机制（"分子计时器级联"）的高级记忆管理系统，专为 AI 智能体设计。它模拟了人类记忆的巩固过程，实现了从短期记忆到长期记忆的分层存储与智能晋升。

引用论文：https://www.nature.com/articles/s41586-025-09774-6

## 1. 项目概述

### 业务目标
本项目旨在为 AI Agent 提供一个具备遗忘、巩固和关联能力的记忆后端，解决传统向量数据库缺乏动态生命周期管理的问题。核心功能包括：
- **分层存储**：模拟人类的短期、中期和长期记忆。
- **智能晋升**：基于重要性（Salience）、重复度（Repetition）和奖励（Reward）自动将关键信息晋升至更持久的存储层。
- **动态遗忘**：根据记忆的生命周期和价值自动清理低价值信息。
- **可解释性**：提供详细的记忆操作日志，便于追踪记忆的形成与消亡。

### 技术栈
- **语言**: TypeScript 5.3 / Node.js
- **测试框架**: Jest 29.7
- **构建工具**: TypeScript Compiler (tsc)
- **依赖管理**: npm

### 核心架构
系统采用分层架构设计，主要包含以下组件：
- **CascadeMemorySystem**: 系统的主要入口和控制器。
- **Storage Layers**: 三层存储结构（Layer 0, Layer 1, Layer 2）。
- **EventEncoder**: 负责将原始输入转化为标准事件对象。
- **ReplayWorker**: 负责记忆的重放与晋升（Consolidation）。
- **ForgettingService**: 负责过期和低价值记忆的清理。

## 2. 业务流程说明

### 核心业务逻辑
系统围绕"记忆的生命周期"展开，主要包含三个阶段：**摄入 (Ingestion)** -> **巩固 (Consolidation)** -> **检索 (Retrieval)**。

1. **记忆摄入 (Ingestion)**
   - 外部数据通过 `ingest()` 接口输入。
   - `EventEncoder` 对数据进行编码、计算初始重要性分数。
   - 数据首先存入 **Layer 0 (ShortTermBuffer)**。
   - 如果检测到重复内容，会增加现有记忆的重复计数。

2. **维护与巩固 (Maintenance & Consolidation)**
   - 通过 `runMaintenanceCycle()` 触发。
   - **ReplayWorker** 扫描各层记忆，利用 `CascadeGates` 判断是否满足晋升条件。
     - Layer 0 -> Layer 1: 基于重复次数和重要性。
     - Layer 1 -> Layer 2: 基于长期价值和结构化关联。
   - **ForgettingService** 根据衰减算法（Decay）和容量限制清理各层记忆。

3. **记忆检索 (Retrieval)**
   - 通过 `retrieve()` 接口查询。
   - 支持基于向量相似度（Vector Similarity）、标签（Tags）和上下文 ID（Context ID）的混合检索。
   - 检索结果会根据相关性和记忆强度进行排序。

### 关键业务场景
- **重复强化**：用户多次提及同一偏好，系统识别重复并将其晋升为长期记忆。
- **闲时整理**：系统在空闲时运行维护周期，将短期高频信息转化为长期知识，并遗忘无关噪点。
- **上下文关联**：通过 Graph 结构（在 Layer 1 中）建立记忆间的关联，提升检索的上下文感知能力。

## 3. 代码结构说明

### 目录结构
```
src/
├── config/            # 系统配置参数 (constants.ts)
├── core/              # 核心业务逻辑组件
│   ├── CascadeMemorySystem.ts  # 主控制器
│   ├── CascadeGates.ts         # 晋升门控逻辑
│   ├── EventEncoder.ts         # 事件编码器
│   ├── ExplainabilityLogger.ts # 可解释性日志
│   ├── ForgettingService.ts    # 遗忘服务
│   └── ReplayWorker.ts         # 重放与晋升工作器
├── storage/           # 存储层实现
│   ├── MemoryStore.ts          # 存储基类
│   ├── ShortTermBuffer.ts      # Layer 0: 短期缓冲
│   ├── MidTermStore.ts         # Layer 1: 中期存储
│   └── LongTermStore.ts        # Layer 2: 长期存储
├── types/             # TypeScript 类型定义
├── utils/             # 工具函数 (向量计算、时间处理)
├── examples.ts        # 使用示例
└── index.ts           # 导出入口
```

### 核心类/方法职责
- **CascadeMemorySystem**: 
  - `ingest(rawEvent)`: 处理新记忆输入。
  - `runMaintenanceCycle()`: 手动触发维护（重放+遗忘）。
  - `retrieve(query)`: 统一检索接口。
- **ShortTermBuffer (Layer 0)**: 
  - 关注高吞吐和快速衰减，使用 FIFO 或时间窗口策略。
- **MidTermStore (Layer 1)**: 
  - 关注结构化关联，维护记忆间的连接图。
- **CascadeGates**: 
  - 包含具体的评分公式（如 `score = α*salience + β*repeat`）来决定记忆去留。

## 4. 使用指南

### 环境配置
需要安装 Node.js (v18+) 和 npm。

### 安装步骤
1. 克隆仓库：
   ```bash
   git clone <repository-url>
   cd chronocascade-memory-engine
   ```
2. 安装依赖：
   ```bash
   npm install
   ```
3. 编译项目：
   ```bash
   npm run build
   ```

### 典型使用示例
```typescript
import { CascadeMemorySystem } from './src';

async function main() {
  // 1. 初始化系统
  const system = new CascadeMemorySystem();

  // 2. 存入记忆
  system.ingest({
    content: { text: "用户喜欢黑色主题" },
    source: "user_settings",
    tags: ["preference", "ui"],
    reward: 1.0 // 强反馈
  });

  // 3. 运行维护周期 (触发晋升/遗忘)
  await system.runMaintenanceCycle();

  // 4. 检索记忆
  const results = system.retrieve({
    tags: ["preference"],
    topK: 1
  });
  
  console.log("Retrieved:", results);
}

main();
```

## 5. 开发规范

### 代码风格
- 遵循 TypeScript 标准编码规范。
- 缩进使用 2 个空格。
- 必须包含类型定义，避免使用 `any`。
- 核心逻辑需编写单元测试。

### 测试要求
- 运行测试：`npm test`
- 提交代码前确保所有测试通过。
- 新增核心功能需补充对应的 Jest 测试用例。
- 关注 `__tests__` 目录下的测试覆盖情况。

### 分支管理
- `main`: 主分支，保持稳定。
- `feature/*`: 功能开发分支。
- `fix/*`: 问题修复分支。
- 提交前请先执行 `npm run build` 确保编译无误。
