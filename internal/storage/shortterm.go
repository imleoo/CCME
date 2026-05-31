package storage

import (
	"math"

	"chronocascade/internal/config"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// ShortTermBuffer is the Layer-0 store: fast write, easy decay, repetition-aware.
type ShortTermBuffer struct {
	baseDir       string
	idx           *Index
	capacity      int
	tauSeconds    float64
	decayRate     float64
	repeatWindow  float64
	clock         util.Clock
	similarityThr float64
}

// NewShortTermBuffer constructs a Layer-0 buffer using the given config and index.
func NewShortTermBuffer(cfg config.Config, idx *Index, clock util.Clock) *ShortTermBuffer {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &ShortTermBuffer{
		baseDir:       cfg.Storage.BaseDir,
		idx:           idx,
		capacity:      cfg.Capacity.ShortTerm,
		tauSeconds:    cfg.Tau[0].Seconds(),
		decayRate:     cfg.DecayRates[0],
		repeatWindow:  cfg.RepeatWindow.Seconds(),
		clock:         clock,
		similarityThr: 0.85,
	}
}

func (s *ShortTermBuffer) Layer() types.LayerState { return types.ShortTerm }

// Add inserts an event. If a similar recent event already exists, it is
// reinforced (repetition count++, score boost) instead.
func (s *ShortTermBuffer) Add(e *types.Event) error {
	if similar, err := s.findSimilarRecent(e); err != nil {
		return err
	} else if similar != nil {
		return s.reinforce(similar)
	}

	size, err := s.idx.CountByLayer(types.ShortTerm)
	if err != nil {
		return err
	}
	existing, err := s.idx.GetByID(e.ID)
	if err != nil {
		return err
	}
	if existing == nil && size >= s.capacity {
		return ErrCapacityExceeded
	}

	now := s.clock.NowMillis()
	e.LastAccessedAt = now
	e.LayerState = types.ShortTerm
	if err := WriteEventFile(s.baseDir, e); err != nil {
		return err
	}
	return s.idx.UpsertEvent(e, EventPath(s.baseDir, types.ShortTerm, e.ID))
}

func (s *ShortTermBuffer) reinforce(existing *types.Event) error {
	existing.Metadata.RepetitionCount++
	now := s.clock.NowMillis()
	existing.LastAccessedAt = now
	boost := 0.1 * float64(existing.Metadata.RepetitionCount)
	score := existing.Scores.RawSalience
	if existing.Scores.Layer0Score != nil {
		score = *existing.Scores.Layer0Score
	}
	newScore := math.Min(score+boost, 1.0)
	existing.Scores.Layer0Score = &newScore
	existing.History = append(existing.History, types.HistoryEntry{
		Action: types.ActionSeen,
		TS:     now,
		Reason: "repetition",
		Score:  &newScore,
	})
	if err := WriteEventFile(s.baseDir, existing); err != nil {
		return err
	}
	return s.idx.UpsertEvent(existing, EventPath(s.baseDir, types.ShortTerm, existing.ID))
}

func (s *ShortTermBuffer) findSimilarRecent(e *types.Event) (*types.Event, error) {
	rows, err := s.idx.ListByLayer(types.ShortTerm)
	if err != nil {
		return nil, err
	}
	now := s.clock.NowMillis()
	for _, r := range rows {
		ageSec := float64(now-r.LastAccessedAt) / 1000.0
		if ageSec > s.repeatWindow {
			continue
		}
		if util.DotProduct(e.Vector, r.Vector) >= s.similarityThr {
			return hydrateEvent(s.baseDir, r)
		}
	}
	return nil, nil
}

// Reindex persists changes to an event without triggering repetition detection.
// Used by the replay worker after score mutations.
func (s *ShortTermBuffer) Reindex(e *types.Event) error {
	if err := WriteEventFile(s.baseDir, e); err != nil {
		return err
	}
	return s.idx.UpsertEvent(e, EventPath(s.baseDir, e.LayerState, e.ID))
}

// Get retrieves an event by id (or nil if absent).
func (s *ShortTermBuffer) Get(id string) (*types.Event, error) {
	r, err := s.idx.GetByID(id)
	if err != nil || r == nil || r.Layer != types.ShortTerm {
		return nil, err
	}
	return hydrateEvent(s.baseDir, r)
}

// GetAll returns every event currently in Layer 0.
func (s *ShortTermBuffer) GetAll() ([]*types.Event, error) {
	rows, err := s.idx.ListByLayer(types.ShortTerm)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Event, 0, len(rows))
	for _, r := range rows {
		e, err := hydrateEvent(s.baseDir, r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Remove deletes an event from disk and index.
func (s *ShortTermBuffer) Remove(id string) (bool, error) {
	r, err := s.idx.GetByID(id)
	if err != nil || r == nil || r.Layer != types.ShortTerm {
		return false, err
	}
	if err := RemoveEventFile(s.baseDir, types.ShortTerm, id); err != nil {
		return false, err
	}
	return s.idx.DeleteEvent(id)
}

func (s *ShortTermBuffer) Size() (int, error) {
	return s.idx.CountByLayer(types.ShortTerm)
}

// Clear removes every event in the layer.
func (s *ShortTermBuffer) Clear() error {
	events, err := s.GetAll()
	if err != nil {
		return err
	}
	for _, e := range events {
		_ = RemoveEventFile(s.baseDir, types.ShortTerm, e.ID)
		if _, err := s.idx.DeleteEvent(e.ID); err != nil {
			return err
		}
	}
	return nil
}

// Search applies tag/context filters then ranks results.
func (s *ShortTermBuffer) Search(q types.RetrievalQuery) ([]types.RetrievalResult, error) {
	layer := types.ShortTerm
	rows, err := s.idx.Search(&layer, q.ContextID, q.Tags)
	if err != nil {
		return nil, err
	}
	events := make([]*types.Event, 0, len(rows))
	for _, r := range rows {
		e, err := hydrateEvent(s.baseDir, r)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	events = applyMinScoreFilter(events, q)
	return rankAndTopK(events, q), nil
}

// ApplyDecay multiplies every layer-0 score by exp(-rate * ageSec).
func (s *ShortTermBuffer) ApplyDecay(nowMillis int64) error {
	events, err := s.GetAll()
	if err != nil {
		return err
	}
	for _, e := range events {
		age := float64(nowMillis-e.LastAccessedAt) / 1000.0
		factor := math.Exp(-s.decayRate * age)
		current := e.Scores.RawSalience
		if e.Scores.Layer0Score != nil {
			current = *e.Scores.Layer0Score
		}
		next := current * factor
		e.Scores.Layer0Score = &next
		e.History = append(e.History, types.HistoryEntry{
			Action: types.ActionDecayed,
			TS:     nowMillis,
			Score:  &next,
		})
		if err := WriteEventFile(s.baseDir, e); err != nil {
			return err
		}
		if err := s.idx.UpsertEvent(e, EventPath(s.baseDir, types.ShortTerm, e.ID)); err != nil {
			return err
		}
	}
	return nil
}

// GetExpiredEvents returns events older than tau.
func (s *ShortTermBuffer) GetExpiredEvents() ([]*types.Event, error) {
	events, err := s.GetAll()
	if err != nil {
		return nil, err
	}
	now := s.clock.NowMillis()
	var out []*types.Event
	for _, e := range events {
		if float64(now-e.CreatedAt)/1000.0 > s.tauSeconds {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *ShortTermBuffer) GetStats() (types.LayerStats, error) {
	events, err := s.GetAll()
	if err != nil {
		return types.LayerStats{}, err
	}
	return computeLayerStats(types.ShortTerm, s.capacity, events), nil
}
