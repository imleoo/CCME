/**
 * 基础功能测试
 */

import { CascadeMemorySystem } from '../core/CascadeMemorySystem';
import { LayerState } from '../types';

describe('CascadeMemorySystem', () => {
  let system: CascadeMemorySystem;
  
  beforeEach(() => {
    system = new CascadeMemorySystem();
  });
  
  afterEach(() => {
    system.clear();
  });
  
  describe('Event Ingestion', () => {
    test('should ingest a single event', () => {
      const event = system.ingest({
        content: { message: 'Hello, world!' },
        source: 'test',
        contextId: 'ctx1',
        tags: ['greeting']
      });
      
      expect(event.id).toBeDefined();
      expect(event.layerState).toBe(LayerState.SHORT_TERM);
      expect(event.vector).toHaveLength(384);
      expect(event.scores.rawSalience).toBeGreaterThan(0);
    });
    
    test('should ingest batch events', () => {
      const events = system.ingestBatch([
        {
          content: { message: 'Event 1' },
          source: 'test',
          contextId: 'ctx1',
          tags: []
        },
        {
          content: { message: 'Event 2' },
          source: 'test',
          contextId: 'ctx1',
          tags: []
        }
      ]);
      
      expect(events).toHaveLength(2);
      expect(events[0].id).not.toBe(events[1].id);
    });
  });
  
  describe('Retrieval', () => {
    test('should retrieve events by context', () => {
      system.ingest({
        content: { message: 'Test 1' },
        source: 'test',
        contextId: 'ctx1',
        tags: []
      });
      
      system.ingest({
        content: { message: 'Test 2' },
        source: 'test',
        contextId: 'ctx2',
        tags: []
      });
      
      const results = system.retrieve({
        contextId: 'ctx1',
        topK: 10
      });
      
      expect(results.length).toBeGreaterThan(0);
      results.forEach(r => {
        expect(r.event.metadata.contextId).toBe('ctx1');
      });
    });
    
    test('should retrieve events by tags', () => {
      system.ingest({
        content: { message: 'Tagged event' },
        source: 'test',
        contextId: 'ctx1',
        tags: ['important']
      });
      
      const results = system.retrieve({
        tags: ['important'],
        topK: 10
      });
      
      expect(results.length).toBeGreaterThan(0);
    });
  });
  
  describe('System Stats', () => {
    test('should return system statistics', () => {
      system.ingest({
        content: { message: 'Test' },
        source: 'test',
        contextId: 'ctx1',
        tags: []
      });
      
      const stats = system.getStats();
      
      expect(stats.layer0.size).toBe(1);
      expect(stats.layer1.size).toBe(0);
      expect(stats.layer2.size).toBe(0);
      expect(stats.totalEvents).toBe(1);
    });
  });
  
  describe('Maintenance Cycle', () => {
    test('should run maintenance cycle', async () => {
      // 添加一些高分事件
      for (let i = 0; i < 5; i++) {
        system.ingest({
          content: { message: `High value event ${i}` },
          source: 'test',
          contextId: 'ctx1',
          reward: 0.9,
          tags: ['important']
        });
      }
      
      const result = await system.runMaintenanceCycle();
      
      expect(result.replay).toBeDefined();
      expect(result.forgetting).toBeDefined();
    });
  });
  
  describe('Event Deletion', () => {
    test('should delete event manually', () => {
      const event = system.ingest({
        content: { message: 'To be deleted' },
        source: 'test',
        contextId: 'ctx1',
        tags: []
      });
      
      const deleted = system.deleteEvent(event.id);
      expect(deleted).toBe(true);
      
      const retrieved = system.getEvent(event.id);
      expect(retrieved).toBeUndefined();
    });
  });
});
