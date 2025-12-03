# ChronoCascade Memory Engine - Persistence Layer Design

## 📋 Overview

This design implements a **hybrid three-tier persistence architecture** that maps to the biological memory hierarchy:

| Layer | Technology | Reason | Characteristics |
|-------|-----------|---------|-----------------|
| **Layer 0** | In-Memory | High-speed buffer | Volatile, 10K capacity, sub-ms latency |
| **Layer 1** | Redis + RediSearch | Fast semi-persistent cache | Vector search, association graph, 5K capacity |
| **Layer 2** | PostgreSQL + pgvector | Durable storage | Persistent, compressed schemas, 1K capacity |

---

## 🏗️ Architecture Design

### System Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│         CascadeMemorySystem (Main Controller)           │
└─────────────────────────────────────────────────────────┘
                        ↓
        ┌───────────────┼───────────────┐
        ↓               ↓               ↓
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│   Layer 0    │ │   Layer 1    │ │   Layer 2    │
│ ShortTerm    │→│  MidTerm     │→│  LongTerm    │
│   Buffer     │ │   Store      │ │   Store      │
├──────────────┤ ├──────────────┤ ├──────────────┤
│  In-Memory   │ │    Redis     │ │ PostgreSQL   │
│    Map       │ │ + RediSearch │ │ + pgvector   │
├──────────────┤ ├──────────────┤ ├──────────────┤
│ • No persist │ │ • RDB/AOF    │ │ • ACID       │
│ • Fast R/W   │ │ • Vector idx │ │ • WAL/MVCC   │
│ • Volatile   │ │ • Graph data │ │ • Schemas    │
└──────────────┘ └──────────────┘ └──────────────┘
```

---

## 📊 Layer 0: In-Memory Storage (Current)

**Technology**: JavaScript `Map<string, Event>`

### Characteristics
- ✅ Already implemented
- ⚡ Ultra-fast: O(1) read/write
- 💨 Volatile: Data lost on restart
- 🎯 Purpose: Hot buffer for recent events

### No Changes Required
```typescript
// Keep existing ShortTermBuffer.ts implementation
export class ShortTermBuffer extends MemoryStore {
  protected events: Map<string, Event>; // In-memory only
  // ... existing code
}
```

---

## 🔴 Layer 1: Redis + RediSearch

### Technology Stack
- **Redis 7.x**: Core key-value store
- **RediSearch 2.x**: Vector similarity search module
- **redis-om**: Node.js ORM for Redis

### Why Redis for Layer 1?
| Feature | Benefit |
|---------|---------|
| RediSearch Vector Index | KNN search with HNSW/FLAT algorithms |
| Sub-millisecond latency | ~1-5ms for vector queries |
| Graph capabilities | Store association edges efficiently |
| Persistence options | RDB snapshots + AOF for durability |
| TTL support | Auto-expiration aligns with decay model |

### Data Model

#### 1. Event Storage (Redis Hash)
```
Key: event:{eventId}
Hash Fields:
  - id: string
  - vector: JSON string (384-dim array)
  - metadata: JSON string
  - layerState: 1
  - scores: JSON string
  - history: JSON string
  - createdAt: timestamp
  - lastAccessedAt: timestamp
```

#### 2. Vector Index (RediSearch)
```
Index: idx:layer1:vectors
Schema:
  - vector: VECTOR (HNSW, FLOAT32, DIM=384, DISTANCE_METRIC=COSINE)
  - contextId: TAG
  - tags: TAG (multi-value)
  - layer1Score: NUMERIC (sortable)
  - createdAt: NUMERIC
```

#### 3. Association Graph (Redis Set)
```
Key: assoc:{eventId}
Type: Set
Members: [associatedEventId1, associatedEventId2, ...]
```

#### 4. Context Index (Redis Set)
```
Key: ctx:{contextId}
Type: Set
Members: [eventId1, eventId2, ...]
```

### Operations

#### Write Path
```typescript
async add(event: Event): Promise<void> {
  // 1. Store event hash
  await redis.hset(`event:${event.id}`, {
    id: event.id,
    vector: JSON.stringify(event.vector),
    metadata: JSON.stringify(event.metadata),
    // ...
  });
  
  // 2. Add to vector index (automatic)
  // RediSearch indexes on HSET
  
  // 3. Build associations
  await this.buildAssociations(event);
  
  // 4. Add to context index
  await redis.sadd(`ctx:${event.metadata.contextId}`, event.id);
  
  // 5. Set TTL (optional: align with TAU)
  await redis.expire(`event:${event.id}`, DEFAULT_CONFIG.TAU[1]);
}
```

#### Read Path
```typescript
async search(query: RetrievalQuery): Promise<RetrievalResult[]> {
  // Vector similarity search
  if (query.vector) {
    const results = await redis.call('FT.SEARCH', 
      'idx:layer1:vectors',
      `*=>[KNN ${query.topK || 10} @vector $query_vec]`,
      'PARAMS', 2, 'query_vec', Buffer.from(new Float32Array(query.vector).buffer),
      'DIALECT', 2
    );
    return parseResults(results);
  }
  
  // Context/tag filtering
  // ...
}
```

#### Association Building
```typescript
async buildAssociations(event: Event): Promise<void> {
  // Find similar events
  const similar = await this.search({
    vector: event.vector,
    topK: 20,
    minScore: 0.7
  });
  
  // Create bidirectional links
  for (const result of similar) {
    await redis.sadd(`assoc:${event.id}`, result.event.id);
    await redis.sadd(`assoc:${result.event.id}`, event.id);
  }
}
```

### Persistence Configuration
```redis
# redis.conf
save 900 1      # RDB: save if 1 key changed in 15 min
save 300 10     # RDB: save if 10 keys changed in 5 min
save 60 10000   # RDB: save if 10K keys changed in 1 min

appendonly yes  # AOF: enable append-only file
appendfsync everysec  # AOF: fsync every second
```

---

## 🟢 Layer 2: PostgreSQL + pgvector

### Technology Stack
- **PostgreSQL 15+**: ACID-compliant relational database
- **pgvector 0.5+**: Vector similarity extension
- **node-postgres (pg)**: Node.js PostgreSQL client
- **TypeORM/Prisma**: Optional ORM layer

### Why PostgreSQL for Layer 2?
| Feature | Benefit |
|---------|---------|
| ACID compliance | Guaranteed durability and consistency |
| pgvector extension | Efficient vector storage and indexing |
| JSONB support | Store complex metadata efficiently |
| Advanced indexing | IVFFlat/HNSW indexes for large-scale search |
| Schema enforcement | Data integrity and validation |
| Partitioning | Archive old schemas by time |

### Database Schema

#### 1. Events Table
```sql
CREATE TABLE layer2_events (
    id UUID PRIMARY KEY,
    vector vector(384) NOT NULL,  -- pgvector type
    metadata JSONB NOT NULL,
    layer_state SMALLINT DEFAULT 2,
    scores JSONB NOT NULL,
    history JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_accessed_at TIMESTAMPTZ NOT NULL,
    promotion_eligible_at TIMESTAMPTZ
);

-- Vector similarity index (HNSW for better performance)
CREATE INDEX idx_layer2_vector ON layer2_events 
USING hnsw (vector vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Context filtering index
CREATE INDEX idx_layer2_context ON layer2_events 
USING GIN ((metadata -> 'contextId'));

-- Score filtering index
CREATE INDEX idx_layer2_score ON layer2_events 
((scores ->> 'layer2Score')::FLOAT DESC);

-- Time-based index
CREATE INDEX idx_layer2_created ON layer2_events (created_at DESC);
```

#### 2. Schemas Table
```sql
CREATE TABLE layer2_schemas (
    id UUID PRIMARY KEY,
    summary TEXT NOT NULL,
    consolidated_from UUID[] NOT NULL,  -- Array of source event IDs
    vector vector(384) NOT NULL,
    importance FLOAT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_updated_at TIMESTAMPTZ NOT NULL,
    metadata JSONB  -- Store aggregated metadata
);

-- Vector index for schemas
CREATE INDEX idx_schema_vector ON layer2_schemas 
USING hnsw (vector vector_cosine_ops);

-- Importance ranking
CREATE INDEX idx_schema_importance ON layer2_schemas (importance DESC);

-- Source events lookup (GIN index for array containment)
CREATE INDEX idx_schema_sources ON layer2_schemas 
USING GIN (consolidated_from);
```

#### 3. Event-Schema Mapping Table
```sql
CREATE TABLE event_schema_mapping (
    event_id UUID REFERENCES layer2_events(id) ON DELETE CASCADE,
    schema_id UUID REFERENCES layer2_schemas(id) ON DELETE CASCADE,
    consolidated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (event_id, schema_id)
);

CREATE INDEX idx_mapping_schema ON event_schema_mapping (schema_id);
```

### Operations

#### Write Path
```typescript
async add(event: Event): Promise<void> {
  await pool.query(`
    INSERT INTO layer2_events (
      id, vector, metadata, scores, history, 
      created_at, last_accessed_at
    ) VALUES ($1, $2, $3, $4, $5, $6, $7)
    ON CONFLICT (id) DO UPDATE SET
      last_accessed_at = EXCLUDED.last_accessed_at,
      scores = EXCLUDED.scores,
      history = EXCLUDED.history
  `, [
    event.id,
    `[${event.vector.join(',')}]`,  // pgvector format
    JSON.stringify(event.metadata),
    JSON.stringify(event.scores),
    JSON.stringify(event.history),
    new Date(event.createdAt),
    new Date(event.lastAccessedAt)
  ]);
}
```

#### Vector Search
```typescript
async search(query: RetrievalQuery): Promise<RetrievalResult[]> {
  const results = await pool.query(`
    SELECT 
      id, vector, metadata, scores, history,
      created_at, last_accessed_at,
      1 - (vector <=> $1::vector) AS similarity
    FROM layer2_events
    WHERE 
      ($2::TEXT IS NULL OR metadata->>'contextId' = $2)
      AND ($3::FLOAT IS NULL OR (scores->>'layer2Score')::FLOAT >= $3)
    ORDER BY vector <=> $1::vector
    LIMIT $4
  `, [
    `[${query.vector.join(',')}]`,
    query.contextId || null,
    query.minScore || null,
    query.topK || 10
  ]);
  
  return results.rows.map(row => ({
    event: this.parseEvent(row),
    similarity: row.similarity,
    retrievalReason: 'vector_similarity'
  }));
}
```

#### Schema Consolidation
```typescript
async consolidateToSchema(events: Event[]): Promise<SchemaEntry> {
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    
    // 1. Create schema
    const schema = this.buildSchema(events);
    await client.query(`
      INSERT INTO layer2_schemas 
      (id, summary, consolidated_from, vector, importance, created_at, last_updated_at)
      VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, [
      schema.id,
      schema.summary,
      schema.consolidatedFrom,
      `[${schema.vector.join(',')}]`,
      schema.importance,
      new Date(schema.createdAt),
      new Date(schema.lastUpdatedAt)
    ]);
    
    // 2. Create mappings
    for (const eventId of schema.consolidatedFrom) {
      await client.query(`
        INSERT INTO event_schema_mapping (event_id, schema_id)
        VALUES ($1, $2)
      `, [eventId, schema.id]);
    }
    
    // 3. Archive/delete original events (optional)
    await client.query(`
      DELETE FROM layer2_events WHERE id = ANY($1)
    `, [schema.consolidatedFrom]);
    
    await client.query('COMMIT');
    return schema;
  } catch (err) {
    await client.query('ROLLBACK');
    throw err;
  } finally {
    client.release();
  }
}
```

### Partitioning Strategy (Optional for Scale)
```sql
-- Partition by creation time for efficient archival
CREATE TABLE layer2_events_2024_q1 PARTITION OF layer2_events
FOR VALUES FROM ('2024-01-01') TO ('2024-04-01');

CREATE TABLE layer2_events_2024_q2 PARTITION OF layer2_events
FOR VALUES FROM ('2024-04-01') TO ('2024-07-01');
-- ...
```

---

## 🔧 Implementation Plan

### Phase 1: Core Infrastructure (Week 1-2)

#### 1.1 Create Storage Interfaces
```typescript
// src/storage/interfaces/IPersistentStore.ts
export interface IPersistentStore extends IMemoryStore {
  connect(): Promise<void>;
  disconnect(): Promise<void>;
  flush(): Promise<void>;
  healthCheck(): Promise<boolean>;
}
```

#### 1.2 Redis Client Setup
```typescript
// src/storage/clients/RedisClient.ts
import { createClient } from 'redis';
import { SchemaFieldTypes, VectorAlgorithms } from '@redis/search';

export class RedisClient {
  private client: ReturnType<typeof createClient>;
  
  async connect() {
    this.client = createClient({
      url: process.env.REDIS_URL || 'redis://localhost:6379'
    });
    await this.client.connect();
    await this.createVectorIndex();
  }
  
  private async createVectorIndex() {
    try {
      await this.client.ft.create('idx:layer1:vectors', {
        '$.vector': {
          type: SchemaFieldTypes.VECTOR,
          ALGORITHM: VectorAlgorithms.HNSW,
          TYPE: 'FLOAT32',
          DIM: 384,
          DISTANCE_METRIC: 'COSINE'
        },
        '$.metadata.contextId': SchemaFieldTypes.TAG,
        '$.scores.layer1Score': SchemaFieldTypes.NUMERIC
      }, {
        ON: 'JSON',
        PREFIX: 'event:'
      });
    } catch (err) {
      // Index already exists
    }
  }
}
```

#### 1.3 PostgreSQL Client Setup
```typescript
// src/storage/clients/PostgresClient.ts
import { Pool } from 'pg';

export class PostgresClient {
  private pool: Pool;
  
  async connect() {
    this.pool = new Pool({
      host: process.env.PG_HOST || 'localhost',
      port: parseInt(process.env.PG_PORT || '5432'),
      database: process.env.PG_DATABASE || 'chronocascade',
      user: process.env.PG_USER || 'postgres',
      password: process.env.PG_PASSWORD,
      max: 20
    });
    
    await this.initSchema();
  }
  
  private async initSchema() {
    // Create tables and indexes
    await this.pool.query('CREATE EXTENSION IF NOT EXISTS vector');
    // Execute schema SQL...
  }
}
```

### Phase 2: Layer 1 Implementation (Week 2-3)

#### 2.1 RedisMidTermStore
```typescript
// src/storage/RedisMidTermStore.ts
export class RedisMidTermStore extends MidTermStore implements IPersistentStore {
  private redisClient: RedisClient;
  
  async add(event: Event): Promise<void> {
    // 1. In-memory operation (for fast access)
    super.add(event);
    
    // 2. Persist to Redis
    await this.redisClient.json.set(`event:${event.id}`, '$', {
      id: event.id,
      vector: event.vector,
      metadata: event.metadata,
      scores: event.scores,
      // ...
    });
    
    // 3. Build associations in Redis
    await this.persistAssociations(event);
  }
  
  async search(query: RetrievalQuery): Promise<RetrievalResult[]> {
    // Primary: Search Redis
    const results = await this.redisVectorSearch(query);
    
    // Fallback: In-memory search (if Redis unavailable)
    if (!results.length) {
      return super.search(query);
    }
    
    return results;
  }
  
  async loadFromRedis(): Promise<void> {
    // On startup: Load events from Redis to memory
    const keys = await this.redisClient.keys('event:*');
    for (const key of keys) {
      const data = await this.redisClient.json.get(key);
      const event = this.deserializeEvent(data);
      super.add(event);  // Load into in-memory Map
    }
  }
}
```

### Phase 3: Layer 2 Implementation (Week 3-4)

#### 3.1 PostgresLongTermStore
```typescript
// src/storage/PostgresLongTermStore.ts
export class PostgresLongTermStore extends LongTermStore implements IPersistentStore {
  private pgClient: PostgresClient;
  
  async add(event: Event): Promise<void> {
    // 1. In-memory cache (optional LRU cache)
    super.add(event);
    
    // 2. Persist to PostgreSQL
    await this.pgClient.query(`
      INSERT INTO layer2_events (...)
      VALUES (...)
      ON CONFLICT (id) DO UPDATE ...
    `, [/* params */]);
  }
  
  async consolidateToSchema(events: Event[]): Promise<SchemaEntry> {
    // Create schema in database with transaction
    const schema = await this.pgClient.withTransaction(async (client) => {
      // Insert schema, create mappings, delete events
      // ...
    });
    
    // Update in-memory state
    return super.consolidateToSchema(events);
  }
  
  async search(query: RetrievalQuery): Promise<RetrievalResult[]> {
    // Primary: PostgreSQL vector search
    const results = await this.pgVectorSearch(query);
    return results;
  }
  
  async loadCache(): Promise<void> {
    // On startup: Load top-K events into memory for fast access
    const topEvents = await this.pgClient.query(`
      SELECT * FROM layer2_events
      ORDER BY (scores->>'layer2Score')::FLOAT DESC
      LIMIT 100
    `);
    // ...
  }
}
```

### Phase 4: Cascade Promotion Logic (Week 4)

#### 4.1 Update CascadeGates
```typescript
// src/core/CascadeGates.ts
export class CascadeGates {
  async promoteToLayer1(event: Event): Promise<boolean> {
    // ... existing validation logic
    
    // 1. Remove from Layer 0 (in-memory)
    this.shortTermBuffer.remove(event.id);
    
    // 2. Add to Layer 1 (Redis persistence)
    event.layerState = LayerState.MID_TERM;
    await this.midTermStore.add(event);  // Now persists to Redis
    
    return true;
  }
  
  async promoteToLayer2(event: Event): Promise<boolean> {
    // ... existing validation logic
    
    // 1. Remove from Layer 1 (Redis)
    await this.midTermStore.remove(event.id);  // Deletes from Redis
    
    // 2. Add to Layer 2 (PostgreSQL)
    event.layerState = LayerState.LONG_TERM;
    await this.longTermStore.add(event);  // Persists to PostgreSQL
    
    return true;
  }
}
```

### Phase 5: Startup & Recovery (Week 5)

#### 5.1 System Initialization
```typescript
// src/core/CascadeMemorySystem.ts
export class CascadeMemorySystem {
  async initialize(): Promise<void> {
    // 1. Connect to storage backends
    await this.midTermStore.connect();
    await this.longTermStore.connect();
    
    // 2. Load persisted data into memory
    await this.midTermStore.loadFromRedis();
    await this.longTermStore.loadCache();
    
    // 3. Verify consistency
    await this.verifyDataIntegrity();
  }
  
  async shutdown(): Promise<void> {
    // Graceful shutdown
    await this.midTermStore.flush();
    await this.longTermStore.flush();
    await this.midTermStore.disconnect();
    await this.longTermStore.disconnect();
  }
}
```

---

## 📦 Dependencies to Add

### package.json Updates
```json
{
  "dependencies": {
    "uuid": "^9.0.1",
    "redis": "^4.6.0",
    "@redis/search": "^1.1.0",
    "pg": "^8.11.0",
    "pgvector": "^0.1.0"
  },
  "devDependencies": {
    "@types/pg": "^8.10.0",
    // ... existing
  }
}
```

---

## 🔐 Configuration

### Environment Variables
```bash
# .env
# Redis Configuration
REDIS_URL=redis://localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# PostgreSQL Configuration
PG_HOST=localhost
PG_PORT=5432
PG_DATABASE=chronocascade
PG_USER=postgres
PG_PASSWORD=your_password
PG_MAX_CONNECTIONS=20

# Persistence Options
ENABLE_LAYER1_PERSISTENCE=true
ENABLE_LAYER2_PERSISTENCE=true
LAYER1_WRITE_THROUGH=true  # Write to Redis immediately
LAYER2_WRITE_BATCH=10      # Batch writes to PostgreSQL
```

### Config File
```typescript
// src/config/persistence.ts
export const PERSISTENCE_CONFIG = {
  layer1: {
    enabled: process.env.ENABLE_LAYER1_PERSISTENCE === 'true',
    writeThrough: true,  // Immediate writes vs buffered
    ttlEnabled: true,    // Use Redis TTL for auto-expiration
    snapshotInterval: 3600  // RDB snapshot interval
  },
  layer2: {
    enabled: process.env.ENABLE_LAYER2_PERSISTENCE === 'true',
    writeBatch: 10,      // Batch size for bulk inserts
    cacheSize: 100,      // LRU cache size in memory
    vacuumInterval: 86400  // PostgreSQL VACUUM interval
  }
};
```

---

## 🧪 Testing Strategy

### 1. Unit Tests
```typescript
describe('RedisMidTermStore', () => {
  it('should persist event to Redis', async () => {
    const event = createMockEvent();
    await store.add(event);
    
    const retrieved = await redisClient.json.get(`event:${event.id}`);
    expect(retrieved.id).toBe(event.id);
  });
  
  it('should restore from Redis on startup', async () => {
    // Seed Redis
    await seedRedisEvents(10);
    
    // Create new store instance
    const newStore = new RedisMidTermStore();
    await newStore.loadFromRedis();
    
    expect(newStore.size()).toBe(10);
  });
});
```

### 2. Integration Tests
```typescript
describe('Layer Promotion with Persistence', () => {
  it('should promote L0 -> L1 with Redis persistence', async () => {
    const event = await system.ingest(mockRawEvent);
    
    // Trigger promotion
    await cascadeGates.promoteToLayer1(event);
    
    // Verify in Redis
    const redisEvent = await redis.json.get(`event:${event.id}`);
    expect(redisEvent.layerState).toBe(1);
  });
  
  it('should promote L1 -> L2 with PostgreSQL persistence', async () => {
    // ...
  });
});
```

### 3. Performance Tests
```typescript
describe('Performance Benchmarks', () => {
  it('Layer 1 write throughput', async () => {
    const events = generateMockEvents(1000);
    const start = Date.now();
    
    for (const event of events) {
      await redisStore.add(event);
    }
    
    const duration = Date.now() - start;
    expect(duration).toBeLessThan(5000);  // <5s for 1K writes
  });
});
```

---

## 📈 Performance Expectations

### Latency Targets

| Operation | Layer 0 | Layer 1 (Redis) | Layer 2 (PostgreSQL) |
|-----------|---------|-----------------|----------------------|
| Write (single) | <1ms | 1-3ms | 5-10ms |
| Read (by ID) | <1ms | 1-2ms | 3-5ms |
| Vector search | N/A | 5-20ms | 20-100ms |
| Batch write (100) | <10ms | 50-100ms | 200-500ms |

### Throughput Targets

- **Layer 1 (Redis)**: 10,000+ writes/sec, 50,000+ reads/sec
- **Layer 2 (PostgreSQL)**: 1,000+ writes/sec, 10,000+ reads/sec

---

## 🚨 Failure Handling

### Redis Failure
```typescript
async add(event: Event): Promise<void> {
  try {
    await this.redisClient.json.set(...);
  } catch (err) {
    logger.error('Redis write failed, falling back to in-memory only', err);
    // Continue with in-memory storage
    super.add(event);
  }
}
```

### PostgreSQL Failure
```typescript
async add(event: Event): Promise<void> {
  try {
    await this.pgClient.query(...);
  } catch (err) {
    logger.error('PostgreSQL write failed, buffering for retry', err);
    this.writeBuffer.push(event);
    // Retry with exponential backoff
  }
}
```

---

## 🔄 Migration Path

### Step 1: Backward Compatibility
- Keep existing in-memory stores
- Add persistence as opt-in feature
- Use feature flags for gradual rollout

### Step 2: Data Migration
```typescript
async migrateToRedis(): Promise<void> {
  const events = this.midTermStore.getAll();
  for (const event of events) {
    await redisClient.json.set(`event:${event.id}`, '$', event);
  }
}
```

### Step 3: Dual-Write Period
- Write to both in-memory and persistent stores
- Compare results for consistency
- Switch reads to persistent stores after validation

---

## 📚 Future Enhancements

1. **Distributed Caching**: Redis Cluster for horizontal scaling
2. **Read Replicas**: PostgreSQL read replicas for query distribution
3. **Vector Index Optimization**: Tune HNSW parameters based on workload
4. **Compression**: Use Redis compression for vector storage
5. **Archival**: Cold storage (S3/GCS) for very old schemas
6. **Monitoring**: Prometheus metrics for latency, throughput, cache hit rates

---

## ✅ Success Criteria

- [ ] Layer 1: 99% data durability with Redis persistence
- [ ] Layer 2: 100% ACID compliance with PostgreSQL
- [ ] <10ms p99 latency for Layer 1 vector search
- [ ] <100ms p99 latency for Layer 2 vector search
- [ ] Zero data loss during graceful shutdown
- [ ] <1min recovery time after crash
- [ ] All existing tests pass with persistence enabled

---

## 📖 References

- [Redis Vector Similarity Docs](https://redis.io/docs/stack/search/reference/vectors/)
- [pgvector GitHub](https://github.com/pgvector/pgvector)
- [HNSW Algorithm Paper](https://arxiv.org/abs/1603.09320)
- [PostgreSQL MVCC](https://www.postgresql.org/docs/current/mvcc.html)
