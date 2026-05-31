
import { CascadeMemorySystem } from '../src/core/CascadeMemorySystem';
import { DEFAULT_CONFIG } from '../src/config/constants';
import { LayerState } from '../src/types';

async function verify() {
  console.log('Starting verification...');

  // 1. Initialize with 0 wait time for immediate promotion testing
  const config = {
    ...DEFAULT_CONFIG,
    REPLAY: {
      ...DEFAULT_CONFIG.REPLAY,
      minWaitTime: 0 // Allow immediate promotion
    }
  };
  const system = new CascadeMemorySystem(config);

  // 2. Test Repetition
  console.log('\n--- Testing Repetition ---');
  const eventContent = { message: 'Repeated Message' };
  
  // First ingestion
  const evt1 = system.ingest({
    content: eventContent,
    source: 'test',
    contextId: 'ctx1',
    tags: ['test']
  });
  console.log(`Ingested Event 1 ID: ${evt1.id}`);

  // Second ingestion (should detect repetition)
  const evt2 = system.ingest({
    content: eventContent,
    source: 'test',
    contextId: 'ctx1',
    tags: ['test']
  });
  console.log(`Ingested Event 2 ID: ${evt2.id}`);

  const statsAfterIngest = system.getStats();
  console.log(`Layer 0 Size: ${statsAfterIngest.layer0.size} (Expected: 1)`);
  
  // Retrieve to check repetition count
  // Note: retrieve returns RetrievalResult[], we need to check the event inside
  const results = system.retrieve({ contextId: 'ctx1' });
  if (results.length === 0) {
      console.error('❌ Retrieval failed for repetition test.');
      return;
  }
  const retrievedEvent = results[0].event;
  console.log(`Repetition Count: ${retrievedEvent.metadata.repetitionCount} (Expected: >= 1)`);

  if (statsAfterIngest.layer0.size === 1 && (retrievedEvent.metadata.repetitionCount || 0) >= 1) {
      console.log('✅ Repetition logic verified.');
  } else {
      console.error('❌ Repetition logic failed.');
      console.log('Debug:', {
          size: statsAfterIngest.layer0.size,
          repetitionCount: retrievedEvent.metadata.repetitionCount
      });
  }

  // 3. Test Promotion (Layer 0 -> Layer 1)
  console.log('\n--- Testing Promotion (Layer 0 -> Layer 1) ---');
  // Ingest a high value event
  // Threshold is 0.7. 
  // We give high reward (1.0) and repeat it a bit to ensure it passes threshold
  // Score = alpha(0.4)*salience + beta(0.3)*repeat + gamma(0.3)*reward
  // With reward 1.0, we get 0.3.
  // We need 0.4 more.
  // Let's add it a few times to boost 'repeat' factor.
  
  const promoContent = { message: "High Value Promotion Candidate" };
  for (let i = 0; i < 5; i++) {
      system.ingest({
          content: promoContent,
          source: 'test',
          contextId: 'ctx_promo',
          reward: 1.0,
          tags: ['promo']
      });
  }

  // Run maintenance
  console.log('Running Maintenance Cycle...');
  const result = await system.runMaintenanceCycle();
  
  console.log('Promotions to Layer 1:', result.replay.layer0Promotions);

  const statsAfterMaint = system.getStats();
  console.log(`Layer 1 Size: ${statsAfterMaint.layer1.size} (Expected: > 0)`);
  console.log(`Layer 0 Size: ${statsAfterMaint.layer0.size}`);
  
  if (statsAfterMaint.layer1.size > 0) {
      console.log('✅ Promotion logic verified.');
  } else {
      console.error('❌ Promotion logic failed.');
      console.log('Details:', JSON.stringify(result, null, 2));
  }

  // 4. Test Retrieval from Layer 1
  console.log('\n--- Testing Retrieval from Layer 1 ---');
  const promoResults = system.retrieve({ tags: ['promo'] });
  console.log(`Retrieved ${promoResults.length} events with tag 'promo'.`);
  
  if (promoResults.length > 0) {
      const retrievedFromLayer = promoResults[0].event.layerState;
      console.log(`Retrieved Event Layer: ${retrievedFromLayer} (Expected: ${LayerState.MID_TERM})`);
      if (retrievedFromLayer === LayerState.MID_TERM) { 
         console.log('✅ Retrieval from promoted layer verified.');
      } else {
         console.log('⚠️ Event retrieved but might not be in expected layer.');
      }
  } else {
      console.error('❌ Retrieval logic failed.');
  }
}

verify().catch(console.error);
