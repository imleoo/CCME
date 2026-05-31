package storage

import (
	"fmt"
	"math"

	"chronocascade/internal/config"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// MidTermStore is Layer 1: structural support with an association graph.
type MidTermStore struct {
	baseDir              string
	idx                  *Index
	capacity             int
	tauSeconds           float64
	decayRate            float64
	clock                util.Clock
	associationThreshold float64
	tagOverlap           int
}

// NewMidTermStore constructs a Layer-1 store.
func NewMidTermStore(cfg config.Config, idx *Index, clock util.Clock) *MidTermStore {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &MidTermStore{
		baseDir:              cfg.Storage.BaseDir,
		idx:                  idx,
		capacity:             cfg.Capacity.MidTerm,
		tauSeconds:           cfg.Tau[1].Seconds(),
		decayRate:            cfg.DecayRates[1],
		clock:                clock,
		associationThreshold: 0.7,
		tagOverlap:           2,
	}
}

func (m *MidTermStore) Layer() types.LayerState { return types.MidTerm }

// Add persists the event and rebuilds its associations with similar peers.
func (m *MidTermStore) Add(e *types.Event) error {
	size, err := m.idx.CountByLayer(types.MidTerm)
	if err != nil {
		return err
	}
	existing, err := m.idx.GetByID(e.ID)
	if err != nil {
		return err
	}
	if existing == nil && size >= m.capacity {
		return fmt.Errorf("%w: mid-term", ErrCapacityExceeded)
	}
	e.LayerState = types.MidTerm
	e.LastAccessedAt = m.clock.NowMillis()
	if err := WriteEventFile(m.baseDir, e); err != nil {
		return err
	}
	if err := m.idx.UpsertEvent(e, EventPath(m.baseDir, types.MidTerm, e.ID)); err != nil {
		return err
	}
	return m.buildAssociations(e)
}

func (m *MidTermStore) buildAssociations(e *types.Event) error {
	rows, err := m.idx.ListByLayer(types.MidTerm)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ID == e.ID {
			continue
		}
		associate := false
		if r.ContextID == e.Metadata.ContextID {
			associate = true
		}
		if !associate && util.DotProduct(e.Vector, r.Vector) >= m.associationThreshold {
			associate = true
		}
		if !associate && countSharedTags(e.Metadata.Tags, r.Tags) >= m.tagOverlap {
			associate = true
		}
		if associate {
			if err := m.idx.AddAssociation(e.ID, r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func countSharedTags(a, b []string) int {
	set := make(map[string]struct{}, len(a))
	for _, t := range a {
		set[t] = struct{}{}
	}
	n := 0
	for _, t := range b {
		if _, ok := set[t]; ok {
			n++
		}
	}
	return n
}

// Centrality is degree-centrality normalised by N-1.
func (m *MidTermStore) Centrality(id string) (float64, error) {
	deg, err := m.idx.NeighborCount(id)
	if err != nil {
		return 0, err
	}
	total, err := m.idx.CountByLayer(types.MidTerm)
	if err != nil || total <= 1 {
		return 0, err
	}
	return float64(deg) / float64(total-1), nil
}

// Reindex persists changes to an event without rebuilding associations.
// Used by the replay worker after score mutations.
func (m *MidTermStore) Reindex(e *types.Event) error {
	if err := WriteEventFile(m.baseDir, e); err != nil {
		return err
	}
	return m.idx.UpsertEvent(e, EventPath(m.baseDir, e.LayerState, e.ID))
}

func (m *MidTermStore) Get(id string) (*types.Event, error) {
	r, err := m.idx.GetByID(id)
	if err != nil || r == nil || r.Layer != types.MidTerm {
		return nil, err
	}
	return hydrateEvent(m.baseDir, r)
}

func (m *MidTermStore) GetAll() ([]*types.Event, error) {
	rows, err := m.idx.ListByLayer(types.MidTerm)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Event, 0, len(rows))
	for _, r := range rows {
		e, err := hydrateEvent(m.baseDir, r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *MidTermStore) Remove(id string) (bool, error) {
	r, err := m.idx.GetByID(id)
	if err != nil || r == nil || r.Layer != types.MidTerm {
		return false, err
	}
	if err := m.idx.RemoveAssociations(id); err != nil {
		return false, err
	}
	if err := RemoveEventFile(m.baseDir, types.MidTerm, id); err != nil {
		return false, err
	}
	return m.idx.DeleteEvent(id)
}

func (m *MidTermStore) Size() (int, error) {
	return m.idx.CountByLayer(types.MidTerm)
}

func (m *MidTermStore) Clear() error {
	events, err := m.GetAll()
	if err != nil {
		return err
	}
	for _, e := range events {
		_ = m.idx.RemoveAssociations(e.ID)
		_ = RemoveEventFile(m.baseDir, types.MidTerm, e.ID)
		if _, err := m.idx.DeleteEvent(e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *MidTermStore) Search(q types.RetrievalQuery) ([]types.RetrievalResult, error) {
	layer := types.MidTerm
	rows, err := m.idx.Search(&layer, q.ContextID, q.Tags)
	if err != nil {
		return nil, err
	}
	events := make([]*types.Event, 0, len(rows))
	for _, r := range rows {
		e, err := hydrateEvent(m.baseDir, r)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	events = applyMinScoreFilter(events, q)
	return rankAndTopK(events, q), nil
}

// ApplyDecay is a Layer-1 decay with structural support: highly connected
// events lose less score than isolated ones.
func (m *MidTermStore) ApplyDecay(nowMillis int64) error {
	events, err := m.GetAll()
	if err != nil {
		return err
	}
	for _, e := range events {
		age := float64(nowMillis-e.LastAccessedAt) / 1000.0
		factor := math.Exp(-m.decayRate * age)
		centrality, err := m.Centrality(e.ID)
		if err != nil {
			return err
		}
		boost := centrality * 0.2
		current := e.Scores.RawSalience
		if e.Scores.Layer1Score != nil {
			current = *e.Scores.Layer1Score
		} else if e.Scores.Layer0Score != nil {
			current = *e.Scores.Layer0Score
		}
		next := current*factor + boost
		e.Scores.Layer1Score = &next
		e.History = append(e.History, types.HistoryEntry{
			Action: types.ActionDecayed,
			TS:     nowMillis,
			Score:  &next,
			Reason: fmt.Sprintf("structural_boost: %.3f", boost),
		})
		if err := WriteEventFile(m.baseDir, e); err != nil {
			return err
		}
		if err := m.idx.UpsertEvent(e, EventPath(m.baseDir, types.MidTerm, e.ID)); err != nil {
			return err
		}
	}
	return nil
}

func (m *MidTermStore) GetExpiredEvents() ([]*types.Event, error) {
	events, err := m.GetAll()
	if err != nil {
		return nil, err
	}
	now := m.clock.NowMillis()
	var out []*types.Event
	for _, e := range events {
		if float64(now-e.CreatedAt)/1000.0 > m.tauSeconds {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *MidTermStore) GetStats() (types.LayerStats, error) {
	events, err := m.GetAll()
	if err != nil {
		return types.LayerStats{}, err
	}
	return computeLayerStats(types.MidTerm, m.capacity, events), nil
}

// TotalAssociations exposes the global edge count for system stats.
func (m *MidTermStore) TotalAssociations() (int, error) {
	return m.idx.TotalAssociations()
}
