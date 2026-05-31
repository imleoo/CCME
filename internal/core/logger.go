package core

import (
	"sync"

	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// LogType enumerates the kinds of log entries.
type LogType string

const (
	LogPromotion     LogType = "promotion"
	LogForgetting    LogType = "forgetting"
	LogReplay        LogType = "replay"
	LogDecay         LogType = "decay"
	LogConsolidation LogType = "consolidation"
)

// LogEntry is a single audit record.
type LogEntry struct {
	ID        string
	Timestamp int64
	Type      LogType
	EventID   string
	FromLayer *types.LayerState
	ToLayer   *types.LayerState
	Reason    string
	Score     *float64
	Details   string
}

// StatsSummary aggregates counts across all log entries.
type StatsSummary struct {
	TotalPromotions     int
	TotalForgetting     int
	TotalReplays        int
	PromotionsByReason  map[types.PromotionReason]int
	ForgettingByReason  map[types.ForgettingReason]int
	AvgPromotionScore   float64
	Layer0ToLayer1      int
	Layer1ToLayer2      int
}

// ExplainabilityLogger keeps the last N audit entries in memory.
type ExplainabilityLogger struct {
	mu     sync.Mutex
	logs   []LogEntry
	maxLen int
	clock  util.Clock
	seq    int
}

// NewExplainabilityLogger constructs a logger that retains up to maxLen entries.
func NewExplainabilityLogger(clock util.Clock, maxLen int) *ExplainabilityLogger {
	if clock == nil {
		clock = util.SystemClock{}
	}
	if maxLen <= 0 {
		maxLen = 10000
	}
	return &ExplainabilityLogger{maxLen: maxLen, clock: clock}
}

func (l *ExplainabilityLogger) add(entry LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	entry.ID = newLogID(entry.Timestamp, l.seq)
	l.logs = append(l.logs, entry)
	if len(l.logs) > l.maxLen {
		l.logs = l.logs[len(l.logs)-l.maxLen:]
	}
}

func newLogID(ts int64, seq int) string {
	return formatLogID(ts, seq)
}

// LogPromotion records a layer promotion.
func (l *ExplainabilityLogger) LogPromotion(e *types.Event, from, to types.LayerState,
	reason types.PromotionReason, score float64, details string,
) {
	fromCopy, toCopy := from, to
	scoreCopy := score
	l.add(LogEntry{
		Timestamp: l.clock.NowMillis(),
		Type:      LogPromotion,
		EventID:   e.ID,
		FromLayer: &fromCopy,
		ToLayer:   &toCopy,
		Reason:    string(reason),
		Score:     &scoreCopy,
		Details:   details,
	})
}

// LogForgetting records an event being pruned.
func (l *ExplainabilityLogger) LogForgetting(eventID string, layer types.LayerState,
	reason types.ForgettingReason, details string,
) {
	layerCopy := layer
	l.add(LogEntry{
		Timestamp: l.clock.NowMillis(),
		Type:      LogForgetting,
		EventID:   eventID,
		FromLayer: &layerCopy,
		Reason:    string(reason),
		Details:   details,
	})
}

// LogReplay records a replay/consolidation event.
func (l *ExplainabilityLogger) LogReplay(e *types.Event, scoreBoost float64, details string) {
	layer := e.LayerState
	scoreCopy := scoreBoost
	l.add(LogEntry{
		Timestamp: l.clock.NowMillis(),
		Type:      LogReplay,
		EventID:   e.ID,
		FromLayer: &layer,
		Score:     &scoreCopy,
		Reason:    "replay_consolidation",
		Details:   details,
	})
}

// LogConsolidation records a schema being created.
func (l *ExplainabilityLogger) LogConsolidation(schemaID string, sources []string, details string) {
	layer := types.LongTerm
	l.add(LogEntry{
		Timestamp: l.clock.NowMillis(),
		Type:      LogConsolidation,
		EventID:   schemaID,
		ToLayer:   &layer,
		Reason:    "schema_consolidation",
		Details:   details,
	})
	_ = sources
}

// GetEventLogs returns all log entries referencing a given event id.
func (l *ExplainabilityLogger) GetEventLogs(eventID string) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []LogEntry
	for _, e := range l.logs {
		if e.EventID == eventID {
			out = append(out, e)
		}
	}
	return out
}

// GenerateStatsSummary aggregates the audit log.
func (l *ExplainabilityLogger) GenerateStatsSummary() StatsSummary {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := StatsSummary{
		PromotionsByReason: map[types.PromotionReason]int{},
		ForgettingByReason: map[types.ForgettingReason]int{},
	}
	var totalScore float64
	var scoreCount int
	for _, e := range l.logs {
		switch e.Type {
		case LogPromotion:
			s.TotalPromotions++
			s.PromotionsByReason[types.PromotionReason(e.Reason)]++
			if e.Score != nil {
				totalScore += *e.Score
				scoreCount++
			}
			if e.FromLayer != nil && e.ToLayer != nil {
				switch {
				case *e.FromLayer == types.ShortTerm && *e.ToLayer == types.MidTerm:
					s.Layer0ToLayer1++
				case *e.FromLayer == types.MidTerm && *e.ToLayer == types.LongTerm:
					s.Layer1ToLayer2++
				}
			}
		case LogForgetting:
			s.TotalForgetting++
			s.ForgettingByReason[types.ForgettingReason(e.Reason)]++
		case LogReplay:
			s.TotalReplays++
		}
	}
	if scoreCount > 0 {
		s.AvgPromotionScore = totalScore / float64(scoreCount)
	}
	return s
}

// ClearLogs drops every stored entry.
func (l *ExplainabilityLogger) ClearLogs() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = nil
}
