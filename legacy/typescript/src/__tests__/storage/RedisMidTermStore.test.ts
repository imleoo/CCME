import { RedisMidTermStore } from '../../storage/RedisMidTermStore';
import { RedisClient } from '../../storage/clients/RedisClient';
import { Event, LayerState } from '../../types';

// Mock RedisClient
jest.mock('../../storage/clients/RedisClient');

describe('RedisMidTermStore', () => {
  let store: RedisMidTermStore;
  let mockRedisClient: jest.Mocked<RedisClient>;
  let mockClient: any;

  beforeEach(() => {
    mockClient = {
      connect: jest.fn(),
      disconnect: jest.fn(),
      json: {
        set: jest.fn(),
        get: jest.fn()
      },
      del: jest.fn(),
      keys: jest.fn().mockResolvedValue([]),
      ft: {
        create: jest.fn()
      }
    };

    mockRedisClient = new RedisClient() as jest.Mocked<RedisClient>;
    mockRedisClient.getClient = jest.fn().mockReturnValue(mockClient);
    mockRedisClient.connect = jest.fn().mockResolvedValue(undefined);

    store = new RedisMidTermStore(10, mockRedisClient);
  });

  const testEvent: Event = {
    id: 'test-1',
    vector: [0.1, 0.2],
    metadata: { source: 'test', contextId: 'ctx', ts: 1000, tags: [] },
    layerState: LayerState.MID_TERM,
    scores: { rawSalience: 1 },
    history: [],
    createdAt: 1000,
    lastAccessedAt: 1000
  };

  test('should persist to Redis on add', async () => {
    mockClient.json.set.mockResolvedValue('OK');
    
    await store.add(testEvent);
    
    expect(store.get('test-1')).toBeDefined();
    expect(mockClient.json.set).toHaveBeenCalledWith(
      'event:test-1', 
      '$', 
      expect.objectContaining({ id: 'test-1' })
    );
  });

  test('should rollback memory if Redis persistence fails', async () => {
    mockClient.json.set.mockRejectedValue(new Error('Redis error'));
    
    await expect(store.add(testEvent)).rejects.toThrow('Redis error');
    expect(store.get('test-1')).toBeUndefined();
  });

  test('should delete from Redis on remove', async () => {
    // Setup
    mockClient.json.set.mockResolvedValue('OK');
    await store.add(testEvent);
    
    mockClient.del.mockResolvedValue(1);
    
    // Act
    await store.remove('test-1');
    
    // Assert
    expect(store.get('test-1')).toBeUndefined();
    expect(mockClient.del).toHaveBeenCalledWith('event:test-1');
  });

  test('should load from persistence on connect', async () => {
    const storedEvent = { ...testEvent };
    mockClient.keys.mockResolvedValue(['event:test-1']);
    mockClient.json.get.mockResolvedValue(storedEvent);
    
    await store.connect();
    
    expect(store.get('test-1')).toBeDefined();
    expect(store.get('test-1')?.id).toBe('test-1');
  });
});
