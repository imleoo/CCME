import { RedisMidTermStore } from '../src/storage/RedisMidTermStore';
import { PostgresLongTermStore } from '../src/storage/PostgresLongTermStore';
import { RedisClient } from '../src/storage/clients/RedisClient';
import { PostgresClient } from '../src/storage/clients/PostgresClient';
import { LayerState, Event } from '../src/types';
import { DEFAULT_CONFIG } from '../src/config/constants';
import { now } from '../src/utils/time';

/**
 * Integration Test for Persistence Layers
 * Note: Requires running Redis and Postgres instances (e.g. via docker-compose)
 */

async function runPersistenceTests() {
  console.log('Starting Persistence Integration Tests...');
  
  // Test Data
  const testEvent: Event = {
    id: `test-event-${Date.now()}`,
    vector: Array(384).fill(0.1),
    metadata: {
      source: 'integration-test',
      contextId: 'test-context',
      ts: Date.now(),
      tags: ['test'],
      repetitionCount: 1
    },
    layerState: LayerState.MID_TERM,
    scores: {
      rawSalience: 0.9,
      layer1Score: 0.9
    },
    history: [{
      action: 'seen',
      ts: Date.now(),
      reason: 'initial creation'
    }],
    createdAt: Date.now(),
    lastAccessedAt: Date.now(),
    promotionEligibleAt: Date.now() + 3600000
  };

  // 1. Test Redis Layer (Layer 1)
  console.log('\nTesting Redis Layer (Layer 1)...');
  const redisClient = new RedisClient();
  try {
    await redisClient.connect();
    const redisStore = new RedisMidTermStore(100, redisClient);
    
    // Add
    console.log('- Adding event to Redis...');
    await redisStore.add(testEvent);
    
    // Allow async persistence to complete
    // await new Promise(resolve => setTimeout(resolve, 500)); // No longer needed with await
    
    // Get (from memory)
    const retrievedFromMem = await redisStore.get(testEvent.id);
    if (retrievedFromMem) console.log('✓ Retrieved from memory cache');
    else console.error('✗ Failed to retrieve from memory cache');

    // Verify persistence (direct client check)
    const rawRedis = await redisClient.getClient().json.get(`event:${testEvent.id}`);
    if (rawRedis) console.log('✓ Verified persistence in Redis');
    else console.error('✗ Failed to verify persistence in Redis');

    // Clean up
    await redisStore.remove(testEvent.id);
    const deleted = await redisClient.getClient().json.get(`event:${testEvent.id}`);
    if (!deleted) console.log('✓ Verified deletion from Redis');
    else console.error('✗ Failed to delete from Redis');
    
    await redisClient.disconnect();
  } catch (e) {
    console.error('Redis Test Failed:', e);
  }

  // 2. Test Postgres Layer (Layer 2)
  console.log('\nTesting Postgres Layer (Layer 2)...');
  const pgClient = new PostgresClient();
  try {
    await pgClient.connect();
    const pgStore = new PostgresLongTermStore(100, pgClient);
    
    const layer2Event = { ...testEvent, layerState: LayerState.LONG_TERM, id: `test-l2-${Date.now()}` };
    
    // Add
    console.log('- Adding event to Postgres...');
    await pgStore.add(layer2Event);
    
    // Allow async persistence
    // await new Promise(resolve => setTimeout(resolve, 500)); // No longer needed
    
    // Verify persistence (direct SQL check)
    const result = await pgClient.query('SELECT * FROM layer2_events WHERE id = $1', [layer2Event.id]);
    if (result.rows.length > 0) console.log('✓ Verified persistence in Postgres');
    else console.error('✗ Failed to verify persistence in Postgres');
    
    // Test Vector Search (Layer 2 specific)
    console.log('- Testing Vector Search...');
    const searchResults = await pgStore.search({ // use standard search interface
      vector: layer2Event.vector,
      topK: 1
    });
    
    if (searchResults.length > 0 && searchResults[0].event.id === layer2Event.id) {
      console.log('✓ Vector search successful');
    } else {
      console.error('✗ Vector search failed or returned no results');
    }

    // Clean up
    await pgStore.remove(layer2Event.id);
    const checkDeleted = await pgClient.query('SELECT * FROM layer2_events WHERE id = $1', [layer2Event.id]);
    if (checkDeleted.rows.length === 0) console.log('✓ Verified deletion from Postgres');
    else console.error('✗ Failed to delete from Postgres');

    await pgClient.end();
  } catch (e) {
    console.error('Postgres Test Failed:', e);
    console.log('Note: Ensure Postgres is running with pgvector extension enabled.');
  }
}

// Run if called directly
if (require.main === module) {
  runPersistenceTests().catch(console.error);
}

export { runPersistenceTests };
