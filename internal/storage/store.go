package storage

import (
	"errors"
	"sort"

	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// ErrCapacityExceeded is returned when a store is full and the incoming event
// is not already present.
var ErrCapacityExceeded = errors.New("layer capacity exceeded")

// Store is the unified interface every layer implements.
type Store interface {
	Layer() types.LayerState
	Add(e *types.Event) error
	Get(id string) (*types.Event, error)
	GetAll() ([]*types.Event, error)
	Remove(id string) (bool, error)
	Size() (int, error)
	Clear() error
	Search(q types.RetrievalQuery) ([]types.RetrievalResult, error)
	ApplyDecay(nowMillis int64) error
	GetExpiredEvents() ([]*types.Event, error)
	GetStats() (types.LayerStats, error)
}

// hydrateEvent reads the markdown file pointed to by a row.
func hydrateEvent(baseDir string, r *EventRow) (*types.Event, error) {
	e, err := ReadEventFile(baseDir, r.Layer, r.ID)
	if err != nil {
		return nil, err
	}
	// Trust the DB for hot fields (score/last_accessed) so concurrent writers stay consistent.
	e.LastAccessedAt = r.LastAccessedAt
	return e, nil
}

// rankAndTopK applies vector / score-based ranking and the topK cut.
func rankAndTopK(events []*types.Event, q types.RetrievalQuery) []types.RetrievalResult {
	results := make([]types.RetrievalResult, 0, len(events))
	if len(q.Vector) > 0 {
		for _, e := range events {
			sim := util.CosineSimilarity(q.Vector, e.Vector)
			results = append(results, types.RetrievalResult{
				Event:           e,
				Similarity:      sim,
				HasSimilarity:   true,
				RetrievalReason: "vector_similarity",
			})
		}
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].Similarity > results[j].Similarity
		})
	} else {
		for _, e := range events {
			results = append(results, types.RetrievalResult{
				Event:           e,
				RetrievalReason: "filter_match",
			})
		}
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].Event.CurrentScore() > results[j].Event.CurrentScore()
		})
	}
	k := q.TopK
	if k <= 0 {
		k = 10
	}
	if k > len(results) {
		k = len(results)
	}
	return results[:k]
}

// applyMinScoreFilter drops events whose current score is below q.MinScore.
func applyMinScoreFilter(events []*types.Event, q types.RetrievalQuery) []*types.Event {
	if q.MinScore == nil {
		return events
	}
	out := events[:0]
	for _, e := range events {
		if e.CurrentScore() >= *q.MinScore {
			out = append(out, e)
		}
	}
	return out
}

// computeLayerStats builds a LayerStats summary from a list of events.
func computeLayerStats(layer types.LayerState, capacity int, events []*types.Event) types.LayerStats {
	stats := types.LayerStats{
		Layer:    layer,
		Capacity: capacity,
		Size:     len(events),
	}
	if capacity > 0 {
		stats.UtilizationRate = float64(stats.Size) / float64(capacity)
	}
	if len(events) == 0 {
		return stats
	}
	min, max, sum := events[0].CurrentScore(), events[0].CurrentScore(), 0.0
	for _, e := range events {
		s := e.CurrentScore()
		sum += s
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	stats.AvgScore = sum / float64(len(events))
	stats.MinScore = min
	stats.MaxScore = max
	return stats
}
