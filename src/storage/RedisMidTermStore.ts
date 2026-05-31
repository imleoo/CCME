import { MidTermStore } from './MidTermStore';
import { IPersistentStore } from './interfaces/IPersistentStore';
import { RedisClient } from './clients/RedisClient';
import { Event, LayerState, RetrievalQuery, RetrievalResult } from '../types';
import { DEFAULT_CONFIG } from '../config/constants';

export class RedisMidTermStore extends MidTermStore implements IPersistentStore {
  private redisClient: RedisClient;

  constructor(capacity: number, redisClient: RedisClient) {
    super(capacity, DEFAULT_CONFIG.TAU[1], DEFAULT_CONFIG.DECAY_RATES[1]);
    this.redisClient = redisClient;
  }

  async connect(): Promise<void> {
    await this.redisClient.connect();
    // Load existing data from Redis to memory (cache warm-up)
    // In a real-world scenario, we might only load hot data or use Redis as the primary source
    await this.loadFromPersistence();
  }

  async disconnect(): Promise<void> {
    await this.redisClient.disconnect();
  }

  async flush(): Promise<void> {
    const client = this.redisClient.getClient();
    await client.flushDb();
    this.clear();
  }

  async healthCheck(): Promise<boolean> {
    return this.redisClient.healthCheck();
  }

  async loadFromPersistence(): Promise<void> {
    const client = this.redisClient.getClient();
    const keys = await client.keys('event:*');
    
    for (const key of keys) {
      const eventJson = await client.json.get(key);
      if (eventJson) {
        const event = eventJson as unknown as Event;
        // Only load Layer 1 events
        if (event.layerState === LayerState.MID_TERM) {
          super.add(event);
        }
      }
    }
  }

  // Override add to persist to Redis
  override async add(event: Event): Promise<void> {
    // 1. Add to memory (MemoryStore implementation)
    super.add(event);

    // 2. Persist to Redis (await for consistency)
    try {
      await this.persistEvent(event);
    } catch (err) {
      console.error(`Failed to persist event ${event.id} to Redis:`, err);
      // Rollback memory
      super.remove(event.id);
      throw err;
    }
  }

  private async persistEvent(event: Event): Promise<void> {
    const client = this.redisClient.getClient();
    await client.json.set(`event:${event.id}`, '$', event as any);
  }

  // Override remove to delete from Redis
  override async remove(id: string): Promise<boolean> {
    const removed = super.remove(id) as boolean;
    if (removed) {
      try {
        await this.deleteFromRedis(id);
      } catch (err) {
        console.error(`Failed to delete event ${id} from Redis:`, err);
        // We can't easily rollback the memory removal without the object
        // So we just throw or log. In strict system we might need 2PC or similar but this is overkill here.
        throw err;
      }
    }
    return removed;
  }

  // Explicitly expose get to resolve TS issues
  override get(id: string): Event | undefined {
    return super.get(id) as Event | undefined;
  }

  private async deleteFromRedis(id: string): Promise<void> {
    const client = this.redisClient.getClient();
    await client.del(`event:${id}`);
  }

  // Override search to use Redis RediSearch for better performance if possible
  // For now, we fallback to memory search if not fully implemented or for simplicity in this hybrid model
  override async search(query: RetrievalQuery): Promise<RetrievalResult[]> {
     return super.search(query) as RetrievalResult[];
  }
}
