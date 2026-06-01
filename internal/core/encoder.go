// Package core hosts the perceptual encoder, gates, replay worker,
// forgetting service, logger, and the top-level CascadeMemorySystem facade.
package core

import (
	"encoding/json"
	"math"

	"github.com/google/uuid"

	"chronocascade/internal/config"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// RawEvent is re-exported from the types package so callers depending only on
// core have a stable name. The canonical definition lives in types.RawEvent.
type RawEvent = types.RawEvent

// EventEncoder turns RawEvents into fully-populated Events.
type EventEncoder struct {
	vectorDim     int
	minWaitMillis int64
	clock         util.Clock
}

// NewEventEncoder constructs an encoder bound to the given config.
func NewEventEncoder(cfg config.Config, clock util.Clock) *EventEncoder {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &EventEncoder{
		vectorDim:     cfg.VectorDim,
		minWaitMillis: cfg.Replay.MinWaitTime.Milliseconds(),
		clock:         clock,
	}
}

// Encode produces a fresh Event from raw input.
func (e *EventEncoder) Encode(raw RawEvent) *types.Event {
	now := e.clock.NowMillis()
	vec := e.encodeVector(raw)
	salience := e.computeSalience(raw)

	tags := raw.Tags
	if tags == nil {
		tags = []string{}
	}
	score := salience
	ev := &types.Event{
		ID:     uuid.NewString(),
		Vector: vec,
		Metadata: types.EventMetadata{
			TS:              now,
			Source:          raw.Source,
			ContextID:       raw.ContextID,
			UserID:          raw.UserID,
			SessionID:       raw.SessionID,
			AgentName:       raw.AgentName,
			Reward:          raw.Reward,
			Tags:            tags,
			RepetitionCount: 0,
		},
		LayerState: types.ShortTerm,
		Scores: types.Scores{
			RawSalience: salience,
			Layer0Score: &score,
		},
		History: []types.HistoryEntry{
			{Action: types.ActionSeen, TS: now, Score: &score},
		},
		CreatedAt:           now,
		LastAccessedAt:      now,
		PromotionEligibleAt: now + e.minWaitMillis,
		Content:             raw.Content,
		ContentRaw:          contentToString(raw.Content),
	}
	return ev
}

// EncodeBatch is the slice-friendly variant.
func (e *EventEncoder) EncodeBatch(raws []RawEvent) []*types.Event {
	out := make([]*types.Event, 0, len(raws))
	for _, r := range raws {
		out = append(out, e.Encode(r))
	}
	return out
}

func (e *EventEncoder) encodeVector(raw RawEvent) []float64 {
	if len(raw.Vector) == e.vectorDim {
		return util.Normalize(raw.Vector)
	}
	return e.simpleEmbedding(raw.Content)
}

// simpleEmbedding is a deterministic, content-hashed placeholder. Real systems
// should plug in a proper embedding model; this is good enough for cosine
// rankings on demo workloads.
func (e *EventEncoder) simpleEmbedding(content any) []float64 {
	s := contentToString(content)
	vec := make([]float64, e.vectorDim)
	for _, c := range s {
		vec[int(c)%e.vectorDim] += 1
	}
	return util.Normalize(vec)
}

func (e *EventEncoder) computeSalience(raw RawEvent) float64 {
	salience := 0.5
	if raw.Reward != nil {
		salience += math.Min(math.Abs(*raw.Reward), 1) * 0.3
	}
	salience += e.estimateComplexity(raw.Content) * 0.2
	return clamp01(salience)
}

func (e *EventEncoder) estimateComplexity(content any) float64 {
	s := contentToString(content)
	switch n := len(s); {
	case n < 50:
		return 0.2
	case n < 200:
		return 0.5
	case n < 1000:
		return 0.8
	default:
		return 1.0
	}
}

func contentToString(content any) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(raw)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
