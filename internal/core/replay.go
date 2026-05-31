package core

import (
	"errors"
	"math"
	"sort"
	"sync"

	"chronocascade/internal/config"
	"chronocascade/internal/storage"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// ReplayStats summarises a single replay cycle.
type ReplayStats struct {
	TotalReplays     int
	Layer0Promotions int
	Layer1Promotions int
	AvgScoreBoost    float64
	DurationMillis   int64
}

// ReplayWorker drives decay, promotion, and consolidation across the cascade.
type ReplayWorker struct {
	cfg      config.Config
	clock    util.Clock
	short    *storage.ShortTermBuffer
	mid      *storage.MidTermStore
	long     *storage.LongTermStore
	gates    *CascadeGates
	mu       sync.Mutex
	running  bool
	lastTime int64
}

// NewReplayWorker wires up the worker against the three stores.
func NewReplayWorker(cfg config.Config, clock util.Clock,
	short *storage.ShortTermBuffer, mid *storage.MidTermStore, long *storage.LongTermStore,
	gates *CascadeGates,
) *ReplayWorker {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &ReplayWorker{
		cfg:   cfg,
		clock: clock,
		short: short,
		mid:   mid,
		long:  long,
		gates: gates,
	}
}

// ExecuteReplayCycle runs one full cycle: decay → promote → replay.
func (w *ReplayWorker) ExecuteReplayCycle() (ReplayStats, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ReplayStats{}, errors.New("replay cycle already running")
	}
	w.running = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	start := w.clock.NowMillis()
	stats := ReplayStats{}

	if err := w.applyDecay(start); err != nil {
		return stats, err
	}
	l0, err := w.promoteLayer0()
	if err != nil {
		return stats, err
	}
	stats.Layer0Promotions = l0

	l1, err := w.promoteLayer1()
	if err != nil {
		return stats, err
	}
	stats.Layer1Promotions = l1

	replayCount, err := w.replayConsolidation()
	if err != nil {
		return stats, err
	}
	stats.TotalReplays = replayCount

	w.lastTime = w.clock.NowMillis()
	stats.DurationMillis = w.lastTime - start
	return stats, nil
}

// LastReplayTime returns the unix-millis timestamp of the most recent cycle.
func (w *ReplayWorker) LastReplayTime() int64 { return w.lastTime }

// NextReplayTime is when the next cycle becomes due.
func (w *ReplayWorker) NextReplayTime() int64 {
	return w.lastTime + w.cfg.Replay.Frequency.Milliseconds()
}

func (w *ReplayWorker) applyDecay(now int64) error {
	if err := w.short.ApplyDecay(now); err != nil {
		return err
	}
	if err := w.mid.ApplyDecay(now); err != nil {
		return err
	}
	return w.long.ApplyDecay(now)
}

func (w *ReplayWorker) promoteLayer0() (int, error) {
	candidates, err := w.short.GetAll()
	if err != nil {
		return 0, err
	}
	type pair struct {
		event *types.Event
		d     PromotionDecision
	}
	var winners []pair
	for _, e := range candidates {
		d := w.gates.ShouldPromoteToLayer1(e)
		if d.ShouldPromote {
			winners = append(winners, pair{event: e, d: d})
		}
	}
	sort.SliceStable(winners, func(i, j int) bool { return winners[i].d.Score > winners[j].d.Score })
	count := 0
	for _, p := range winners {
		w.gates.MarkPromoted(p.event, types.MidTerm, p.d.Reason, p.d.Score)
		if _, err := w.short.Remove(p.event.ID); err != nil {
			return count, err
		}
		if err := w.mid.Add(p.event); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (w *ReplayWorker) promoteLayer1() (int, error) {
	candidates, err := w.mid.GetAll()
	if err != nil {
		return 0, err
	}
	type pair struct {
		event *types.Event
		d     PromotionDecision
	}
	var winners []pair
	for _, e := range candidates {
		d, err := w.gates.ShouldPromoteToLayer2(e, w.mid)
		if err != nil {
			return 0, err
		}
		if d.ShouldPromote {
			winners = append(winners, pair{event: e, d: d})
		}
	}
	sort.SliceStable(winners, func(i, j int) bool { return winners[i].d.Score > winners[j].d.Score })
	count := 0
	for _, p := range winners {
		w.gates.MarkPromoted(p.event, types.LongTerm, p.d.Reason, p.d.Score)
		if _, err := w.mid.Remove(p.event.ID); err != nil {
			return count, err
		}
		if err := w.long.Add(p.event); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (w *ReplayWorker) replayConsolidation() (int, error) {
	batch := w.cfg.Replay.BatchSize
	half := batch / 2
	boost := w.cfg.Replay.ConsolidationBoost

	l0, err := w.short.GetAll()
	if err != nil {
		return 0, err
	}
	l1, err := w.mid.GetAll()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, e := range topK(l0, half) {
		w.boostScore(e, boost)
		if err := w.short.Reindex(e); err != nil {
			return count, err
		}
		count++
	}
	for _, e := range topK(l1, half) {
		w.boostScore(e, boost)
		if err := w.mid.Reindex(e); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (w *ReplayWorker) boostScore(e *types.Event, boost float64) {
	now := w.clock.NowMillis()
	current := e.CurrentScore()
	next := math.Min(current+boost, 1.0)
	switch e.LayerState {
	case types.ShortTerm:
		e.Scores.Layer0Score = &next
	case types.MidTerm:
		e.Scores.Layer1Score = &next
	case types.LongTerm:
		e.Scores.Layer2Score = &next
	}
	e.LastAccessedAt = now
	e.History = append(e.History, types.HistoryEntry{
		Action: types.ActionReplayed,
		TS:     now,
		Score:  &next,
		Reason: "consolidation_replay",
	})
}

func topK(events []*types.Event, k int) []*types.Event {
	if k <= 0 || len(events) == 0 {
		return nil
	}
	dup := append([]*types.Event(nil), events...)
	sort.SliceStable(dup, func(i, j int) bool {
		return dup[i].CurrentScore() > dup[j].CurrentScore()
	})
	if k > len(dup) {
		k = len(dup)
	}
	return dup[:k]
}
