import { Pool, PoolClient } from 'pg';
import pgvector from 'pgvector/pg';

export class PostgresClient {
  private pool: Pool;

  constructor() {
    this.pool = new Pool({
      host: process.env.PG_HOST || 'localhost',
      port: parseInt(process.env.PG_PORT || '5432'),
      user: process.env.PG_USER || 'postgres',
      password: process.env.PG_PASSWORD || 'postgres',
      database: process.env.PG_DATABASE || 'chronocascade',
      max: 20,
      idleTimeoutMillis: 30000,
    });

    // Register pgvector types
    this.pool.on('connect', async (client) => {
      await pgvector.registerType(client);
    });
  }

  async connect(): Promise<PoolClient> {
    return this.pool.connect();
  }

  async query(text: string, params?: any[]) {
    return this.pool.query(text, params);
  }

  async end(): Promise<void> {
    await this.pool.end();
  }

  async healthCheck(): Promise<boolean> {
    try {
      const client = await this.pool.connect();
      try {
        await client.query('SELECT 1');
        return true;
      } finally {
        client.release();
      }
    } catch (e) {
      return false;
    }
  }

  getPool(): Pool {
    return this.pool;
  }
}
