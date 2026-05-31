package core

import (
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
func (f *ForgettingService) ExecuteForgetCycle() (ForgettingStats, error) {
	stats := ForgettingStats{Reasons: map[types.ForgettingReason]int{}}

	n, err := f.pruneLayer0(stats.Reasons)
	if err != nil {
		return stats, err
	}
	stats.Layer0Pruned = n

	n, err = f.pruneLayer1(stats.Reasons)
	if err != nil {
		return stats, err
	}
	stats.Layer1Pruned = n

	n, err = f.pruneLayer2(stats.Reasons)
	if err != nil {
		return stats, err
	}
	stats.Layer2Pruned = n

	stats.TotalPruned = stats.Layer0Pruned + stats.Layer1Pruned + stats.Layer2Pruned
	return stats, nil
}

func (f *ForgettingService) pruneLayer0(reasons map[types.ForgettingReason]int) (int, error) {
	expired, err := f.short.GetExpiredEvents()
	if err != nil {
		return 0, err
	}
	toRemove := map[string]types.ForgettingReason{}
	for _, e := range expired {
		toRemove[e.ID] = types.ForgetExpired
	}
	all, err := f.short.GetAll()
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
	size, err := f.short.Size()
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
	return f.applyRemovals(f.short.Remove, toRemove, reasons)
}

func (f *ForgettingService) pruneLayer1(reasons map[types.ForgettingReason]int) (int, error) {
	expired, err := f.mid.GetExpiredEvents()
	if err != nil {
		return 0, err
	}
	toRemove := map[string]types.ForgettingReason{}
	for _, e := range expired {
		toRemove[e.ID] = types.ForgetExpired
	}
	all, err := f.mid.GetAll()
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
		c, err := f.mid.Centrality(e.ID)
		if err != nil {
			return 0, err
		}
		if e.CurrentScore() < lowScoreThreshold && c < lowCentralityThreshold {
			toRemove[e.ID] = types.ForgetLowUtility
		}
	}
	capacity := f.cfg.Capacity.MidTerm
	size, err := f.mid.Size()
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
			c, err := f.mid.Centrality(e.ID)
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
	return f.applyRemovals(f.mid.Remove, toRemove, reasons)
}

func (f *ForgettingService) pruneLayer2(reasons map[types.ForgettingReason]int) (int, error) {
	all, err := f.long.GetAll()
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
	return f.applyRemovals(f.long.Remove, toRemove, reasons)
}

func (f *ForgettingService) applyRemovals(
	remove func(id string) (bool, error),
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
func (f *ForgettingService) ManualDelete(id string) (bool, error) {
	if ok, err := f.short.Remove(id); err != nil || ok {
		return ok, err
	}
	if ok, err := f.mid.Remove(id); err != nil || ok {
		return ok, err
	}
	return f.long.Remove(id)
}
