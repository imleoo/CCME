// Package config carries the tunable parameters of the cascade.
package config

import "time"

// SalienceWeights are the α/β/γ weights used by the promotion gate.
type SalienceWeights struct {
	Alpha float64 // raw salience weight
	Beta  float64 // repetition factor weight
	Gamma float64 // reward signal weight
}

// ReplayConfig controls the periodic replay/consolidation worker.
type ReplayConfig struct {
	Frequency          time.Duration // how often to run replay
	BatchSize          int           // events processed per cycle
	MinWaitTime        time.Duration // min age before promotion is allowed
	ConsolidationBoost float64       // score bump applied on replay
}

// CapacityConfig caps how many events each layer can hold.
type CapacityConfig struct {
	ShortTerm int
	MidTerm   int
	LongTerm  int
}

// StorageConfig points at the on-disk locations used by the persistence layer.
type StorageConfig struct {
	BaseDir       string // root for markdown files
	SQLitePath    string // sqlite index path
	UseSqliteVec  bool   // reserved: enable sqlite-vec KNN (requires cgo build)
}

// Config bundles all knobs of the system.
type Config struct {
	Tau               [3]time.Duration // per-layer lifespan
	PromoThreshold    [2]float64       // L0→L1, L1→L2 promotion thresholds
	DecayRates        [3]float64       // per-second decay rates
	SalienceWeights   SalienceWeights
	Replay            ReplayConfig
	Capacity          CapacityConfig
	VectorDim         int
	RepeatWindow      time.Duration // repetition-detection window
	Storage           StorageConfig
}

// Default returns the bio-inspired defaults that match the original TS engine.
func Default() Config {
	return Config{
		Tau: [3]time.Duration{
			24 * time.Hour,
			7 * 24 * time.Hour,
			30 * 24 * time.Hour,
		},
		PromoThreshold: [2]float64{0.7, 0.8},
		DecayRates:     [3]float64{0.00002, 0.000005, 0.000001},
		SalienceWeights: SalienceWeights{
			Alpha: 0.4,
			Beta:  0.3,
			Gamma: 0.3,
		},
		Replay: ReplayConfig{
			Frequency:          time.Hour,
			BatchSize:          50,
			MinWaitTime:        time.Hour,
			ConsolidationBoost: 0.1,
		},
		Capacity: CapacityConfig{
			ShortTerm: 10000,
			MidTerm:   5000,
			LongTerm:  1000,
		},
		VectorDim:    384,
		RepeatWindow: time.Hour,
		Storage: StorageConfig{
			BaseDir:      "./memory",
			SQLitePath:   "./memory/index.db",
			UseSqliteVec: false,
		},
	}
}
