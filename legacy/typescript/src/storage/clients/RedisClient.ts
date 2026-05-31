import { createClient } from 'redis';

export class RedisClient {
  private client: ReturnType<typeof createClient>;
  private isConnected: boolean = false;

  constructor() {
    this.client = createClient({
      url: process.env.REDIS_URL || 'redis://localhost:6379'
    });

    this.client.on('error', (err) => console.error('Redis Client Error', err));
  }

  async connect(): Promise<void> {
    if (this.isConnected) return;
    
    await this.client.connect();
    this.isConnected = true;
    await this.createVectorIndex();
  }

  async disconnect(): Promise<void> {
    if (!this.isConnected) return;
    
    await this.client.disconnect();
    this.isConnected = false;
  }

  getClient() {
    return this.client;
  }

  private async createVectorIndex() {
    try {
      await this.client.ft.create('idx:layer1:vectors', {
        '$.vector': {
          type: 'VECTOR',
          ALGORITHM: 'HNSW',
          TYPE: 'FLOAT32',
          DIM: 384,
          DISTANCE_METRIC: 'COSINE',
          AS: 'vector'
        },
        '$.metadata.contextId': {
          type: 'TAG',
          AS: 'contextId'
        },
        '$.scores.layer1Score': {
          type: 'NUMERIC',
          AS: 'layer1Score',
          SORTABLE: true
        }
      } as any, {
        ON: 'JSON',
        PREFIX: 'event:'
      });
      console.log('Created Redis vector index: idx:layer1:vectors');
    } catch (err: any) {
      if (err.message === 'Index already exists') {
        // Index already exists, skip
      } else {
        console.error('Failed to create Redis vector index:', err);
      }
    }
  }

  async healthCheck(): Promise<boolean> {
    try {
      return await this.client.ping() === 'PONG';
    } catch (e) {
      return false;
    }
  }
}
