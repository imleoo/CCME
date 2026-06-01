package core

import (
	"context"
	"errors"
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
// It implements types.Manager.
type CascadeMemorySystem struct {
	cfg      config.Config
	clock    util.Clock
	idx      *storage.Index
	short    *storage.ShortTermBuffer
	mid      *storage.MidTermStore
	long     *storage.LongTermStore
	profiles *storage.ProfileStore
	sessions *storage.SessionStore
	chatLog  *storage.ChatLog
	encoder  *EventEncoder
	gates    *CascadeGates
	replay   *ReplayWorker
	forget   *ForgettingService
	logger   *ExplainabilityLogger
}

var _ types.Manager = (*CascadeMemorySystem)(nil)

// New constructs the system and opens the SQLite index plus markdown layout.
func New(ctx context.Context, cfg config.Config) (*CascadeMemorySystem, error) {
	return NewWithClock(ctx, cfg, util.SystemClock{})
}

// NewWithClock is the testable variant accepting an injectable clock.
func NewWithClock(ctx context.Context, cfg config.Config, clock util.Clock) (*CascadeMemorySystem, error) {
	idx, err := storage.OpenIndex(ctx, cfg.Storage.SQLitePath)
	if err != nil {
		return nil, err
	}
	short := storage.NewShortTermBuffer(cfg, idx, clock)
	mid := storage.NewMidTermStore(cfg, idx, clock)
	long := storage.NewLongTermStore(cfg, idx, clock)
	profiles := storage.NewProfileStore(cfg.Storage.BaseDir, idx, clock)
	sessions := storage.NewSessionStore(cfg.Storage.BaseDir, idx, clock)
	chatLog := storage.NewChatLog(idx, clock)
	gates := NewCascadeGates(cfg, clock)
	replay := NewReplayWorker(cfg, clock, short, mid, long, gates)
	forget := NewForgettingService(cfg, short, mid, long)
	logger := NewExplainabilityLogger(clock, 10000)
	encoder := NewEventEncoder(cfg, clock)
	return &CascadeMemorySystem{
		cfg: cfg, clock: clock, idx: idx,
		short: short, mid: mid, long: long,
		profiles: profiles, sessions: sessions, chatLog: chatLog,
		encoder: encoder, gates: gates,
		replay: replay, forget: forget, logger: logger,
	}, nil
}

// Close releases the SQLite handle. Markdown files stay on disk.
func (s *CascadeMemorySystem) Close() error { return s.idx.Close() }

// ── Cascade-side API ─────────────────────────────────────────────────────────

// Ingest accepts a single raw event and stores it in Layer 0.
func (s *CascadeMemorySystem) Ingest(ctx context.Context, raw types.RawEvent) (*types.Event, error) {
	e := s.encoder.Encode(raw)
	if err := s.short.Add(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// IngestBatch is the batch version of Ingest.
func (s *CascadeMemorySystem) IngestBatch(ctx context.Context, raws []types.RawEvent) ([]*types.Event, error) {
	out := make([]*types.Event, 0, len(raws))
	for _, r := range raws {
		e, err := s.Ingest(ctx, r)
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
	return out, nil
}

// IngestAsync writes the event in a background goroutine. The result is
// delivered on the returned channel exactly once (then closed).
//
// We snapshot ctx.Done() at call time; the background goroutine runs with a
// background context so the caller cancelling does not abort an already
// half-written ingest. If you need cancellable async writes, gate the work
// yourself before calling IngestAsync.
func (s *CascadeMemorySystem) IngestAsync(ctx context.Context, raw types.RawEvent) <-chan types.IngestResult {
	out := make(chan types.IngestResult, 1)
	go func() {
		defer close(out)
		// Use a detached context so a cancelled parent ctx doesn't corrupt
		// half-written state. The cascade is local — no remote calls to abort.
		bg := context.Background()
		e, err := s.Ingest(bg, raw)
		select {
		case out <- types.IngestResult{Event: e, Err: err}:
		case <-ctx.Done():
			// Caller stopped listening; drop the result.
		}
	}()
	return out
}

// Retrieve queries all (or one specific) layer and returns the merged top-K results.
func (s *CascadeMemorySystem) Retrieve(ctx context.Context, q types.RetrievalQuery) ([]types.RetrievalResult, error) {
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
		r, err := layer.Search(ctx, q)
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
func (s *CascadeMemorySystem) RunReplayCycle(ctx context.Context) (ReplayStats, error) {
	stats, err := s.replay.ExecuteReplayCycle(ctx)
	if err == nil {
		s.logger.LogReplay(&types.Event{ID: "system", LayerState: types.ShortTerm}, 0,
			"replay cycle: L0→L1="+itoa(stats.Layer0Promotions)+", L1→L2="+itoa(stats.Layer1Promotions))
	}
	return stats, err
}

// RunForgetCycle is the manual trigger for pruning.
func (s *CascadeMemorySystem) RunForgetCycle(ctx context.Context) (ForgettingStats, error) {
	stats, err := s.forget.ExecuteForgetCycle(ctx)
	if err == nil {
		s.logger.LogForgetting("system", types.ShortTerm, types.ForgetLowScore,
			"forget cycle pruned "+itoa(stats.TotalPruned))
	}
	return stats, err
}

// RunMaintenanceCycle is replay+forget back-to-back.
func (s *CascadeMemorySystem) RunMaintenanceCycle(ctx context.Context) (ReplayStats, ForgettingStats, error) {
	r, err := s.RunReplayCycle(ctx)
	if err != nil {
		return r, ForgettingStats{}, err
	}
	f, err := s.RunForgetCycle(ctx)
	return r, f, err
}

// GetStats returns a fresh system-wide snapshot.
func (s *CascadeMemorySystem) GetStats(ctx context.Context) (SystemStats, error) {
	l0, err := s.short.GetStats(ctx)
	if err != nil {
		return SystemStats{}, err
	}
	l1, err := s.mid.GetStats(ctx)
	if err != nil {
		return SystemStats{}, err
	}
	l2, err := s.long.GetStats(ctx)
	if err != nil {
		return SystemStats{}, err
	}
	edges, err := s.mid.TotalAssociations(ctx)
	if err != nil {
		return SystemStats{}, err
	}
	schemas, err := s.long.SchemaCount(ctx)
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
func (s *CascadeMemorySystem) DeleteEvent(ctx context.Context, id string) (bool, error) {
	return s.forget.ManualDelete(ctx, id)
}

// GetEvent fetches an event by id across all layers.
func (s *CascadeMemorySystem) GetEvent(ctx context.Context, id string) (*types.Event, error) {
	if e, err := s.short.Get(ctx, id); err != nil || e != nil {
		return e, err
	}
	if e, err := s.mid.Get(ctx, id); err != nil || e != nil {
		return e, err
	}
	return s.long.Get(ctx, id)
}

// Clear wipes every layer, every association, every schema, every markdown file.
func (s *CascadeMemorySystem) Clear(ctx context.Context) error {
	if err := s.short.Clear(ctx); err != nil {
		return err
	}
	if err := s.mid.Clear(ctx); err != nil {
		return err
	}
	if err := s.long.Clear(ctx); err != nil {
		return err
	}
	s.logger.ClearLogs()
	return s.idx.ClearAll(ctx)
}

// ConsolidateLongTermMemory runs schema clustering on Layer 2 and returns the
// number of new schemas created.
func (s *CascadeMemorySystem) ConsolidateLongTermMemory(ctx context.Context) (int, error) {
	schemas, err := s.long.AutoConsolidate(ctx, 3, 0.8)
	if err != nil {
		return 0, err
	}
	for _, sch := range schemas {
		s.logger.LogConsolidation(sch.ID, sch.ConsolidatedFrom, sch.Summary)
	}
	return len(schemas), nil
}

// ── Session-side API ─────────────────────────────────────────────────────────

// ReadSessionContext loads the running state of a chat session.
func (s *CascadeMemorySystem) ReadSessionContext(ctx context.Context, userID, sessionID string) (*types.SessionContext, error) {
	return s.sessions.ReadContext(ctx, userID, sessionID)
}

// WriteSessionContext persists a session context document.
func (s *CascadeMemorySystem) WriteSessionContext(ctx context.Context, sc *types.SessionContext) error {
	return s.sessions.WriteContext(ctx, sc)
}

// ReadUserProfile loads the structured profile for a user.
func (s *CascadeMemorySystem) ReadUserProfile(ctx context.Context, userID string) (*types.UserProfile, error) {
	return s.profiles.Read(ctx, userID)
}

// WriteUserProfile persists a structured profile.
func (s *CascadeMemorySystem) WriteUserProfile(ctx context.Context, p *types.UserProfile) error {
	if p == nil {
		return errors.New("WriteUserProfile: nil profile")
	}
	return s.profiles.Write(ctx, p)
}

// UpsertSessionSummary replaces the rolling summary for a session.
func (s *CascadeMemorySystem) UpsertSessionSummary(ctx context.Context, sum *types.SessionSummary) error {
	return s.sessions.UpsertSummary(ctx, sum)
}

// ListSessionSummaries returns rolling summaries ordered by turn range.
func (s *CascadeMemorySystem) ListSessionSummaries(ctx context.Context, userID, sessionID string) ([]*types.SessionSummary, error) {
	return s.sessions.ListSummaries(ctx, userID, sessionID)
}

// WriteChatMessage appends one chat message to the audit log.
func (s *CascadeMemorySystem) WriteChatMessage(ctx context.Context, msg *types.ChatMessage) error {
	return s.chatLog.Write(ctx, msg)
}

// ReadRecentChatMessages returns up to `limit` recent messages (most recent first).
func (s *CascadeMemorySystem) ReadRecentChatMessages(ctx context.Context, userID, sessionID string, limit int) ([]*types.ChatMessage, error) {
	return s.chatLog.Recent(ctx, userID, sessionID, limit)
}

func itoa(n int) string {
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
