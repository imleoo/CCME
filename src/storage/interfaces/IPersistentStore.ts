import { IMemoryStore } from '../MemoryStore';

/**
 * Interface for persistent storage layers (Layer 1 & Layer 2)
 */
export interface IPersistentStore extends IMemoryStore {
  connect(): Promise<void>;
  disconnect(): Promise<void>;
  flush(): Promise<void>;
  healthCheck(): Promise<boolean>;
  
  // For loading data from persistence to memory on startup
  loadFromPersistence(): Promise<void>;
}
