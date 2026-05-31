/**
 * Usage examples: Demonstrate basic system functionality
 */

import { CascadeMemorySystem } from './index';
import { LayerState } from './types';

// Example 1: Basic usage
function basicUsage() {
  const system = new CascadeMemorySystem();
  
  // Ingest event
  const event = system.ingest({
    content: { message: 'User prefers dark mode' },
    source: 'user_interaction',
    contextId: 'user_123',
    tags: ['preference', 'ui'],
    reward: 0.8
  });
  
  return { system, event };
}

// Example 2: Repeated event promotion
async function repeatedEventPromotion() {
  const system = new CascadeMemorySystem();
  
  // User expresses same preference multiple times
  for (let i = 0; i < 5; i++) {
    system.ingest({
      content: { 
        user: 'alice',
        preference: 'vegetarian',
        context: 'food'
      },
      source: 'dialogue',
      contextId: 'alice_preferences',
      tags: ['food', 'preference'],
      reward: 0.9
    });
  }
  
  // Run maintenance cycle
  const result = await system.runMaintenanceCycle();
  
  const stats = system.getStats();
  
  return {
    promotions: result.replay.layer0Promotions,
    layer0: stats.layer0.size,
    layer1: stats.layer1.size,
    layer2: stats.layer2.size
  };
}

// Example 3: Retrieval and query
function retrievalExample() {
  const system = new CascadeMemorySystem();
  
  // Add multiple events
  system.ingest({
    content: { topic: 'AI', info: 'Machine learning basics' },
    source: 'learning',
    contextId: 'study',
    tags: ['AI', 'ML']
  });
  
  system.ingest({
    content: { topic: 'AI', info: 'Deep learning introduction' },
    source: 'learning',
    contextId: 'study',
    tags: ['AI', 'DL']
  });
  
  system.ingest({
    content: { topic: 'Cooking', info: 'How to make pasta' },
    source: 'learning',
    contextId: 'hobby',
    tags: ['cooking']
  });
  
  // Retrieve by tag
  const aiResults = system.retrieve({
    tags: ['AI'],
    topK: 10
  });
  
  // Retrieve by context
  const studyResults = system.retrieve({
    contextId: 'study',
    topK: 10
  });
  
  return {
    aiCount: aiResults.length,
    studyCount: studyResults.length
  };
}

// Example 4: Statistics and monitoring
async function statsExample() {
  const system = new CascadeMemorySystem();
  
  // Add some events
  for (let i = 0; i < 20; i++) {
    system.ingest({
      content: { id: i, value: `Event ${i}` },
      source: 'test',
      contextId: 'batch',
      tags: i % 2 === 0 ? ['even'] : ['odd'],
      reward: Math.random()
    });
  }
  
  // Run maintenance
  await system.runMaintenanceCycle();
  
  // Get statistics
  const systemStats = system.getStats();
  const logSummary = system.getLogSummary();
  
  return {
    totalEvents: systemStats.totalEvents,
    layer0Utilization: systemStats.layer0.utilization,
    totalPromotions: logSummary.totalPromotions,
    totalForgetting: logSummary.totalForgetting
  };
}

// Export examples
export {
  basicUsage,
  repeatedEventPromotion,
  retrievalExample,
  statsExample
};
