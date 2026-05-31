import { LongTermStore } from './LongTermStore';
import { IPersistentStore } from './interfaces/IPersistentStore';
import { PostgresClient } from './clients/PostgresClient';
import { Event, LayerState, RetrievalQuery, RetrievalResult } from '../types';
import pgvector from 'pgvector/pg';
import { DEFAULT_CONFIG } from '../config/constants';

export class PostgresLongTermStore extends LongTermStore implements IPersistentStore {
  private pgClient: PostgresClient;

  constructor(capacity: number, pgClient: PostgresClient) {
    super(capacity, DEFAULT_CONFIG.TAU[2], DEFAULT_CONFIG.DECAY_RATES[2]);
    this.pgClient = pgClient;
  }

  async connect(): Promise<void> {
    // Pool manages connections, but we verify connectivity
    const healthy = await this.healthCheck();
    if (!healthy) {
      throw new Error('Failed to connect to PostgreSQL');
    }
    // We do NOT load all data into memory for Layer 2 as it's meant to be huge
    // We only use memory as a cache for recently accessed items if needed
    // For this implementation, we will treat MemoryStore as a cache
  }

  async disconnect(): Promise<void> {
    await this.pgClient.end();
  }

  async flush(): Promise<void> {
    await this.pgClient.query('TRUNCATE TABLE layer2_events CASCADE');
    this.clear();
  }

  async healthCheck(): Promise<boolean> {
    return this.pgClient.healthCheck();
  }

  async loadFromPersistence(): Promise<void> {
    // No-op for Layer 2: too large to load all into memory
    // Could implement LRU cache warm-up strategies here
  }

  override async add(event: Event): Promise<void> {
    // 1. Add to memory cache
    super.add(event);

    // 2. Persist to Postgres
    try {
      await this.persistEvent(event);
    } catch (err) {
      console.error(`Failed to persist event ${event.id} to Postgres:`, err);
      super.remove(event.id);
      throw err;
    }
  }

  private async persistEvent(event: Event): Promise<void> {
    const query = `
      INSERT INTO layer2_events (
        id, vector, metadata, layer_state, scores, history, created_at, last_accessed_at, promotion_eligible_at
      ) VALUES (
        $1, $2, $3, $4, $5, $6, $7, $8, $9
      ) ON CONFLICT (id) DO UPDATE SET
        vector = EXCLUDED.vector,
        metadata = EXCLUDED.metadata,
        layer_state = EXCLUDED.layer_state,
        scores = EXCLUDED.scores,
        history = EXCLUDED.history,
        last_accessed_at = EXCLUDED.last_accessed_at,
        promotion_eligible_at = EXCLUDED.promotion_eligible_at
    `;

    const vectorString = pgvector.toSql(event.vector);

    const values = [
      event.id,
      vectorString,
      JSON.stringify(event.metadata),
      event.layerState,
      JSON.stringify(event.scores),
      JSON.stringify(event.history),
      new Date(event.createdAt),
      new Date(event.lastAccessedAt),
      event.promotionEligibleAt ? new Date(event.promotionEligibleAt) : null
    ];

    await this.pgClient.query(query, values);
  }

  override async remove(id: string): Promise<boolean> {
    const removed = super.remove(id) as boolean;
    await this.deleteFromPostgres(id);
    return true; // We assume it existed if we tried to delete it from DB, or we can check row count
  }

  private async deleteFromPostgres(id: string): Promise<void> {
    await this.pgClient.query('DELETE FROM layer2_events WHERE id = $1', [id]);
  }

  override async search(query: RetrievalQuery): Promise<RetrievalResult[]> {
    // Prefer DB search for Layer 2
    return this.searchAsync(query);
  }

  // New async search method specifically for Layer 2
  async searchAsync(query: RetrievalQuery): Promise<RetrievalResult[]> {
    if (!query.vector) {
      return []; // Text search not implemented in this snippet
    }

    const vectorString = pgvector.toSql(query.vector);
    const limit = query.topK || 10;
    
    // Basic vector similarity search
    // Note: pgvector uses <=> for cosine distance (lower is better)
    // We convert distance to similarity: 1 - distance
    let sql = `
      SELECT *, 1 - (vector <=> $1) as similarity
      FROM layer2_events
      WHERE 1 - (vector <=> $1) >= $2
    `;
    const params: any[] = [vectorString, query.minScore || 0.0];
    let paramIndex = 3;

    if (query.contextId) {
      sql += ` AND metadata->>'contextId' = $${paramIndex}`;
      params.push(query.contextId);
      paramIndex++;
    }

    sql += ` ORDER BY similarity DESC LIMIT $${paramIndex}`;
    params.push(limit);

    const result = await this.pgClient.query(sql, params);

    return result.rows.map(row => ({
      event: {
        id: row.id,
        vector: JSON.parse(row.vector), // pgvector returns string, need to parse if needed, or handle
        // actually pgvector might return array depending on driver version, 
        // assuming standard format reconstruction
        metadata: row.metadata,
        layerState: row.layer_state,
        scores: row.scores,
        history: row.history,
        createdAt: new Date(row.created_at).getTime(),
        lastAccessedAt: new Date(row.last_accessed_at).getTime(),
        promotionEligibleAt: row.promotion_eligible_at ? new Date(row.promotion_eligible_at).getTime() : undefined
      } as unknown as Event, // Casting for simplicity
      score: row.similarity,
      layer: LayerState.LONG_TERM,
      retrievalReason: 'vector_similarity'
    }));
  }
}
