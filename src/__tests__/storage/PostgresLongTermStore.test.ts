import { PostgresLongTermStore } from '../../storage/PostgresLongTermStore';
import { PostgresClient } from '../../storage/clients/PostgresClient';
import { Event, LayerState } from '../../types';

// Mock PostgresClient
jest.mock('../../storage/clients/PostgresClient');

describe('PostgresLongTermStore', () => {
  let store: PostgresLongTermStore;
  let mockPgClient: jest.Mocked<PostgresClient>;

  beforeEach(() => {
    mockPgClient = new PostgresClient() as jest.Mocked<PostgresClient>;
    mockPgClient.query = jest.fn().mockResolvedValue({ rows: [] });
    mockPgClient.connect = jest.fn().mockResolvedValue({} as any);

    store = new PostgresLongTermStore(10, mockPgClient);
  });

  const testEvent: Event = {
    id: 'test-2',
    vector: [0.1, 0.2],
    metadata: { source: 'test', contextId: 'ctx', ts: 1000, tags: [] },
    layerState: LayerState.LONG_TERM,
    scores: { rawSalience: 1 },
    history: [],
    createdAt: 1000,
    lastAccessedAt: 1000
  };

  test('should persist to Postgres on add', async () => {
    mockPgClient.query.mockResolvedValue({ rows: [] } as any);
    
    await store.add(testEvent);
    
    expect(store.get('test-2')).toBeDefined(); // Should be in cache
    expect(mockPgClient.query).toHaveBeenCalledWith(
      expect.stringContaining('INSERT INTO layer2_events'),
      expect.arrayContaining(['test-2'])
    );
  });

  test('should rollback memory if Postgres persistence fails', async () => {
    mockPgClient.query.mockRejectedValue(new Error('DB error'));
    
    await expect(store.add(testEvent)).rejects.toThrow('DB error');
    expect(store.get('test-2')).toBeUndefined();
  });

  test('should delete from Postgres on remove', async () => {
    // Setup
    await store.add(testEvent);
    
    // Act
    await store.remove('test-2');
    
    // Assert
    expect(store.get('test-2')).toBeUndefined();
    expect(mockPgClient.query).toHaveBeenCalledWith(
      expect.stringContaining('DELETE FROM layer2_events'),
      ['test-2']
    );
  });

  test('should perform vector search via SQL', async () => {
    const mockRow = {
      id: 'test-2',
      vector: '[0.1, 0.2]',
      metadata: testEvent.metadata,
      layer_state: 2,
      scores: testEvent.scores,
      history: testEvent.history,
      created_at: new Date(),
      last_accessed_at: new Date(),
      similarity: 0.95
    };

    mockPgClient.query.mockResolvedValue({ rows: [mockRow] } as any);
    
    const results = await store.search({
      vector: [0.1, 0.2],
      topK: 5
    });
    
    expect(results).toHaveLength(1);
    expect(results[0].event.id).toBe('test-2');
    expect(mockPgClient.query).toHaveBeenCalledWith(
      expect.stringContaining('SELECT *, 1 - (vector <=> $1) as similarity'),
      expect.any(Array)
    );
  });
});
