package core

import (
	"context"
	"sort"

	"chronocascade/internal/config"
	"chronocascade/internal/storage"
	"chronocascade/internal/types"
)

// ForgettingStats summarises one prune cycle.
type ForgettingStats struct {
	Layer0Pruned int
	Layer1Pruned int
	Layer2Pruned int
	TotalPruned  int
	Reasons      map[types.ForgettingReason]int
}

// ForgettingService implements the prune policies for each layer.
type ForgettingService struct {
	cfg   config.Config
	short *storage.ShortTermBuffer
	mid   *storage.MidTermStore
	long  *storage.LongTermStore
}

// NewForgettingService constructs the service.
func NewForgettingService(cfg config.Config,
	short *storage.ShortTermBuffer, mid *storage.MidTermStore, long *storage.LongTermStore,
) *ForgettingService {
	return &ForgettingService{cfg: cfg, short: short, mid: mid, long: long}
}

// ExecuteForgetCycle prunes expired/low-utility entries across all layers.
func (f *ForgettingService) ExecuteForgetCycle(ctx context.Context) (ForgettingStats, error) {
	stats := ForgettingStats{Reasons: map[types.ForgettingReason]int{}}

	n, err := f.pruneLayer0(ctx, stats.Reasons)
	if err != nil {
		return stats, err
	}
	stats.Layer0Pruned = n

	n, err = f.pruneLayer1(ctx, stats.Reasons)
	if err != nil {
		return stats, err
	}
	stats.Layer1Pruned = n

	n, err = f.pruneLayer2(ctx, stats.Reasons)
	if err != nil {
		return stats, err
	}
	stats.Layer2Pruned = n

	stats.TotalPruned = stats.Layer0Pruned + stats.Layer1Pruned + stats.Layer2Pruned
	return stats, nil
}

// removeFn is the per-layer Remove function bound to ctx so applyRemovals can
// call it without each caller manually closing over ctx.
type removeFn func(id string) (bool, error)

func (f *ForgettingService) pruneLayer0(ctx context.Context, reasons map[types.ForgettingReason]int) (int, error) {
	expired, err := f.short.GetExpiredEvents(ctx)
	if err != nil {
		return 0, err
	}
	toRemove := map[string]types.ForgettingReason{}
	for _, e := range expired {
		toRemove[e.ID] = types.ForgetExpired
	}
	all, err := f.short.GetAll(ctx)
	if err != nil {
		return 0, err
	}
	const lowScoreThreshold = 0.1
	for _, e := range all {
		if _, dup := toRemove[e.ID]; dup {
			continue
		}
		if e.CurrentScore() < lowScoreThreshold {
			toRemove[e.ID] = types.ForgetLowScore
		}
	}
	capacity := f.cfg.Capacity.ShortTerm
	size, err := f.short.Size(ctx)
	if err != nil {
		return 0, err
	}
	excess := size - len(toRemove) - capacity
	if excess > 0 {
		var candidates []*types.Event
		for _, e := range all {
			if _, dup := toRemove[e.ID]; !dup {
				candidates = append(candidates, e)
			}
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].CurrentScore() < candidates[j].CurrentScore()
		})
		for i := 0; i < excess && i < len(candidates); i++ {
			toRemove[candidates[i].ID] = types.ForgetCapacityLimit
		}
	}
	return f.applyRemovals(func(id string) (bool, error) { return f.short.Remove(ctx, id) }, toRemove, reasons)
}

func (f *ForgettingService) pruneLayer1(ctx context.Context, reasons map[types.ForgettingReason]int) (int, error) {
	expired, err := f.mid.GetExpiredEvents(ctx)
	if err != nil {
		return 0, err
	}
	toRemove := map[string]types.ForgettingReason{}
	for _, e := range expired {
		toRemove[e.ID] = types.ForgetExpired
	}
	all, err := f.mid.GetAll(ctx)
	if err != nil {
		return 0, err
	}
	const (
		lowScoreThreshold      = 0.2
		lowCentralityThreshold = 0.1
	)
	for _, e := range all {
		if _, dup := toRemove[e.ID]; dup {
			continue
		}
		c, err := f.mid.Centrality(ctx, e.ID)
		if err != nil {
			return 0, err
		}
		if e.CurrentScore() < lowScoreThreshold && c < lowCentralityThreshold {
			toRemove[e.ID] = types.ForgetLowUtility
		}
	}
	capacity := f.cfg.Capacity.MidTerm
	size, err := f.mid.Size(ctx)
	if err != nil {
		return 0, err
	}
	excess := size - len(toRemove) - capacity
	if excess > 0 {
		type ranked struct {
			e *types.Event
			s float64
		}
		var cands []ranked
		for _, e := range all {
			if _, dup := toRemove[e.ID]; dup {
				continue
			}
			c, err := f.mid.Centrality(ctx, e.ID)
			if err != nil {
				return 0, err
			}
			cands = append(cands, ranked{e: e, s: e.CurrentScore() + c})
		}
		sort.SliceStable(cands, func(i, j int) bool { return cands[i].s < cands[j].s })
		for i := 0; i < excess && i < len(cands); i++ {
			toRemove[cands[i].e.ID] = types.ForgetCapacityLimit
		}
	}
	return f.applyRemovals(func(id string) (bool, error) { return f.mid.Remove(ctx, id) }, toRemove, reasons)
}

func (f *ForgettingService) pruneLayer2(ctx context.Context, reasons map[types.ForgettingReason]int) (int, error) {
	all, err := f.long.GetAll(ctx)
	if err != nil {
		return 0, err
	}
	const lowScoreThreshold = 0.05
	toRemove := map[string]types.ForgettingReason{}
	for _, e := range all {
		if e.CurrentScore() < lowScoreThreshold {
			toRemove[e.ID] = types.ForgetLowUtility
		}
	}
	return f.applyRemovals(func(id string) (bool, error) { return f.long.Remove(ctx, id) }, toRemove, reasons)
}

func (f *ForgettingService) applyRemovals(
	remove removeFn,
	plan map[string]types.ForgettingReason,
	reasons map[types.ForgettingReason]int,
) (int, error) {
	pruned := 0
	for id, reason := range plan {
		ok, err := remove(id)
		if err != nil {
			return pruned, err
		}
		if ok {
			pruned++
			reasons[reason]++
		}
	}
	return pruned, nil
}

// ManualDelete tries each layer in order and removes the first match.
func (f *ForgettingService) ManualDelete(ctx context.Context, id string) (bool, error) {
	if ok, err := f.short.Remove(ctx, id); err != nil || ok {
		return ok, err
	}
	if ok, err := f.mid.Remove(ctx, id); err != nil || ok {
		return ok, err
	}
	return f.long.Remove(ctx, id)
}
