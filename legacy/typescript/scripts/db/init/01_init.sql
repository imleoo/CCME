-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- 1. Events Table (Layer 2 Storage)
CREATE TABLE IF NOT EXISTS layer2_events (
    id UUID PRIMARY KEY,
    vector vector(384) NOT NULL,
    metadata JSONB NOT NULL,
    layer_state SMALLINT DEFAULT 2,
    scores JSONB NOT NULL,
    history JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_accessed_at TIMESTAMPTZ NOT NULL,
    promotion_eligible_at TIMESTAMPTZ
);

-- Vector similarity index (HNSW for better performance)
CREATE INDEX IF NOT EXISTS idx_layer2_vector ON layer2_events 
USING hnsw (vector vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Context filtering index
CREATE INDEX IF NOT EXISTS idx_layer2_context ON layer2_events 
USING GIN ((metadata -> 'contextId'));

-- Score filtering index
CREATE INDEX IF NOT EXISTS idx_layer2_score ON layer2_events 
((scores ->> 'layer2Score')::FLOAT DESC);

-- Time-based index
CREATE INDEX IF NOT EXISTS idx_layer2_created ON layer2_events (created_at DESC);

-- 2. Schemas Table (Consolidated Memories)
CREATE TABLE IF NOT EXISTS layer2_schemas (
    id UUID PRIMARY KEY,
    summary TEXT NOT NULL,
    consolidated_from UUID[] NOT NULL,
    vector vector(384) NOT NULL,
    importance FLOAT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_updated_at TIMESTAMPTZ NOT NULL,
    metadata JSONB
);

-- Vector index for schemas
CREATE INDEX IF NOT EXISTS idx_schema_vector ON layer2_schemas 
USING hnsw (vector vector_cosine_ops);

-- Importance ranking
CREATE INDEX IF NOT EXISTS idx_schema_importance ON layer2_schemas (importance DESC);

-- Source events lookup
CREATE INDEX IF NOT EXISTS idx_schema_sources ON layer2_schemas 
USING GIN (consolidated_from);

-- 3. Event-Schema Mapping Table
CREATE TABLE IF NOT EXISTS event_schema_mapping (
    event_id UUID REFERENCES layer2_events(id) ON DELETE CASCADE,
    schema_id UUID REFERENCES layer2_schemas(id) ON DELETE CASCADE,
    consolidated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (event_id, schema_id)
);

CREATE INDEX IF NOT EXISTS idx_mapping_schema ON event_schema_mapping (schema_id);
