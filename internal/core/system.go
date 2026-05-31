package core

import (
	"sort"

	"chronocascade/internal/config"
	"chronocascade/internal/storage"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// SystemStats is the top-level snapshot returned by GetStats().
type SystemStats struct {
	Layer0         types.LayerStats
	Layer1         types.LayerStats
	Layer2         types.LayerStats
	Layer1Edges    int
	Layer2Schemas  int
	TotalEvents    int
	LastReplayTime int64
}

// CascadeMemorySystem is the public facade that wires all components together.
type CascadeMemorySystem struct {
	cfg     config.Config
	clock   util.Clock
	idx     *storage.Index
	short   *storage.ShortTermBuffer
	mid     *storage.MidTermStore
	long    *storage.LongTermStore
	encoder *EventEncoder
	gates   *CascadeGates
	replay  *ReplayWorker
	forget  *ForgettingService
	logger  *ExplainabilityLogger
}

// New constructs the system and opens the SQLite index plus markdown layout.
func New(cfg config.Config) (*CascadeMemorySystem, error) {
	return NewWithClock(cfg, util.SystemClock{})
}

// NewWithClock is the testable variant accepting an injectable clock.
func NewWithClock(cfg config.Config, clock util.Clock) (*CascadeMemorySystem, error) {
	idx, err := storage.OpenIndex(cfg.Storage.SQLitePath)
	if err != nil {
		return nil, err
	}
	short := storage.NewShortTermBuffer(cfg, idx, clock)
	mid := storage.NewMidTermStore(cfg, idx, clock)
	long := storage.NewLongTermStore(cfg, idx, clock)
	gates := NewCascadeGates(cfg, clock)
	replay := NewReplayWorker(cfg, clock, short, mid, long, gates)
	forget := NewForgettingService(cfg, short, mid, long)
	logger := NewExplainabilityLogger(clock, 10000)
	encoder := NewEventEncoder(cfg, clock)
	return &CascadeMemorySystem{
		cfg: cfg, clock: clock, idx: idx,
		short: short, mid: mid, long: long,
		encoder: encoder, gates: gates,
		replay: replay, forget: forget, logger: logger,
	}, nil
}

// Close releases the SQLite handle. Markdown files stay on disk.
func (s *CascadeMemorySystem) Close() error { return s.idx.Close() }

// Ingest accepts a single raw event and stores it in Layer 0.
func (s *CascadeMemorySystem) Ingest(raw RawEvent) (*types.Event, error) {
	e := s.encoder.Encode(raw)
	if err := s.short.Add(e); err != nil {
		return nil, err
	}
	return e, nil
}

// IngestBatch is the batch version of Ingest.
func (s *CascadeMemorySystem) IngestBatch(raws []RawEvent) ([]*types.Event, error) {
	out := make([]*types.Event, 0, len(raws))
	for _, r := range raws {
		e, err := s.Ingest(r)
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Retrieve queries all (or one specific) layer and returns the merged top-K results.
func (s *CascadeMemorySystem) Retrieve(q types.RetrievalQuery) ([]types.RetrievalResult, error) {
	layers := []storage.Store{s.short, s.mid, s.long}
	if q.Layer != nil {
		switch *q.Layer {
		case types.ShortTerm:
			layers = []storage.Store{s.short}
		case types.MidTerm:
			layers = []storage.Store{s.mid}
		case types.LongTerm:
			layers = []storage.Store{s.long}
		}
	}
	var merged []types.RetrievalResult
	for _, layer := range layers {
		r, err := layer.Search(q)
		if err != nil {
			return nil, err
		}
		merged = append(merged, r...)
	}
	if len(q.Vector) > 0 {
		sort.SliceStable(merged, func(i, j int) bool {
			return merged[i].Similarity > merged[j].Similarity
		})
	}
	k := q.TopK
	if k <= 0 {
		k = 10
	}
	if k > len(merged) {
		k = len(merged)
	}
	return merged[:k], nil
}

// RunReplayCycle is the manual trigger for the consolidation worker.
func (s *CascadeMemorySystem) RunReplayCycle() (ReplayStats, error) {
	stats, err := s.replay.ExecuteReplayCycle()
	if err == nil {
		// Surface a single audit log so the explainability summary tracks cycles.
		s.logger.LogReplay(&types.Event{ID: "system", LayerState: types.ShortTerm}, 0,
			"replay cycle: L0→L1="+itoa(stats.Layer0Promotions)+", L1→L2="+itoa(stats.Layer1Promotions))
	}
	return stats, err
}

// RunForgetCycle is the manual trigger for pruning.
func (s *CascadeMemorySystem) RunForgetCycle() (ForgettingStats, error) {
	stats, err := s.forget.ExecuteForgetCycle()
	if err == nil {
		s.logger.LogForgetting("system", types.ShortTerm, types.ForgetLowScore,
			"forget cycle pruned "+itoa(stats.TotalPruned))
	}
	return stats, err
}

// RunMaintenanceCycle is replay+forget back-to-back.
func (s *CascadeMemorySystem) RunMaintenanceCycle() (ReplayStats, ForgettingStats, error) {
	r, err := s.RunReplayCycle()
	if err != nil {
		return r, ForgettingStats{}, err
	}
	f, err := s.RunForgetCycle()
	return r, f, err
}

// GetStats returns a fresh system-wide snapshot.
func (s *CascadeMemorySystem) GetStats() (SystemStats, error) {
	l0, err := s.short.GetStats()
	if err != nil {
		return SystemStats{}, err
	}
	l1, err := s.mid.GetStats()
	if err != nil {
		return SystemStats{}, err
	}
	l2, err := s.long.GetStats()
	if err != nil {
		return SystemStats{}, err
	}
	edges, err := s.mid.TotalAssociations()
	if err != nil {
		return SystemStats{}, err
	}
	schemas, err := s.long.SchemaCount()
	if err != nil {
		return SystemStats{}, err
	}
	return SystemStats{
		Layer0:         l0,
		Layer1:         l1,
		Layer2:         l2,
		Layer1Edges:    edges,
		Layer2Schemas:  schemas,
		TotalEvents:    l0.Size + l1.Size + l2.Size,
		LastReplayTime: s.replay.LastReplayTime(),
	}, nil
}

// GetLogSummary returns the explainability summary.
func (s *CascadeMemorySystem) GetLogSummary() StatsSummary { return s.logger.GenerateStatsSummary() }

// GetEventHistory returns audit entries for a single event id.
func (s *CascadeMemorySystem) GetEventHistory(id string) []LogEntry {
	return s.logger.GetEventLogs(id)
}

// DeleteEvent removes a single event by id from whichever layer holds it.
func (s *CascadeMemorySystem) DeleteEvent(id string) (bool, error) {
	return s.forget.ManualDelete(id)
}

// GetEvent fetches an event by id across all layers.
func (s *CascadeMemorySystem) GetEvent(id string) (*types.Event, error) {
	if e, err := s.short.Get(id); err != nil || e != nil {
		return e, err
	}
	if e, err := s.mid.Get(id); err != nil || e != nil {
		return e, err
	}
	return s.long.Get(id)
}

// Clear wipes every layer, every association, every schema, every markdown file.
func (s *CascadeMemorySystem) Clear() error {
	if err := s.short.Clear(); err != nil {
		return err
	}
	if err := s.mid.Clear(); err != nil {
		return err
	}
	if err := s.long.Clear(); err != nil {
		return err
	}
	s.logger.ClearLogs()
	return s.idx.ClearAll()
}

// ConsolidateLongTermMemory runs schema clustering on Layer 2 and returns the
// number of new schemas created.
func (s *CascadeMemorySystem) ConsolidateLongTermMemory() (int, error) {
	schemas, err := s.long.AutoConsolidate(3, 0.8)
	if err != nil {
		return 0, err
	}
	for _, sch := range schemas {
		s.logger.LogConsolidation(sch.ID, sch.ConsolidatedFrom, sch.Summary)
	}
	return len(schemas), nil
}

func itoa(n int) string {
	// tiny inlined int→string to keep system.go free of fmt imports
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
