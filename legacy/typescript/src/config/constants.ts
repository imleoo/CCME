/**
 * System constants configuration
 * Maps time scales and thresholds of biological memory
 */

export const DEFAULT_CONFIG = {
  // Lifespan per layer (seconds) - Corresponds to time scales of biological memory
  TAU: [
    24 * 3600,      // Layer 0: 1 day (short-term - corresponds to thalamus/CAMTA1)
    7 * 24 * 3600,  // Layer 1: 7 days (mid-term - corresponds to TCF4)
    30 * 24 * 3600  // Layer 2: 30 days (long-term - corresponds to ASH1L)
  ],
  
  // Promotion thresholds - Corresponds to activation thresholds of molecular cascades
  PROMO_THRESH: [
    0.7,  // Layer 0 -> Layer 1 promotion threshold
    0.8   // Layer 1 -> Layer 2 promotion threshold
  ],
  
  // Decay rates (per layer per second)
  DECAY_RATES: [
    0.00002,  // Layer 0: Fast decay
    0.000005, // Layer 1: Medium decay
    0.000001  // Layer 2: Slow decay
  ],
  
  // Salience evaluation weights
  SALIENCE_WEIGHTS: {
    alpha: 0.4,  // Raw salience weight
    beta: 0.3,   // Repetition factor weight
    gamma: 0.3   // Reward signal weight
  },
  
  // Replay configuration
  REPLAY: {
    frequency: 3600,        // Execute replay every hour (seconds)
    batchSize: 50,          // Number of events processed per replay
    minWaitTime: 3600,      // Minimum wait time (seconds) - simulates molecular timing
    consolidationBoost: 0.1 // Score boost from replay
  },
  
  // Capacity limits
  CAPACITY: {
    shortTerm: 10000,   // Layer 0 maximum capacity
    midTerm: 5000,      // Layer 1 maximum capacity
    longTerm: 1000      // Layer 2 maximum capacity
  },
  
  // Vector dimension
  VECTOR_DIM: 384,
  
  // Repetition detection window (seconds)
  REPEAT_WINDOW: 3600,

  // Persistence Configuration
  PERSISTENCE: {
    ENABLED: true,
    REDIS: {
      url: process.env.REDIS_URL || 'redis://localhost:6379'
    },
    POSTGRES: {
      host: process.env.PG_HOST || 'localhost',
      port: parseInt(process.env.PG_PORT || '5432'),
      user: process.env.PG_USER || 'postgres',
      password: process.env.PG_PASSWORD || 'postgres',
      database: process.env.PG_DATABASE || 'chronocascade'
    }
  }
};

export type SystemConfig = typeof DEFAULT_CONFIG;
