package core

import (
	"fmt"
	"math"

	"chronocascade/internal/config"
	"chronocascade/internal/storage"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// PromotionDecision is the gate's verdict on a single event.
type PromotionDecision struct {
	ShouldPromote bool
	Reason        types.PromotionReason
	Score         float64
	Details       string
}

// CascadeGates implement the molecular timer cascade — when an event has
// matured enough to graduate to the next layer.
type CascadeGates struct {
	cfg   config.Config
	clock util.Clock
}

// NewCascadeGates builds the gates with the given configuration.
func NewCascadeGates(cfg config.Config, clock util.Clock) *CascadeGates {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &CascadeGates{cfg: cfg, clock: clock}
}

// ShouldPromoteToLayer1 evaluates a Layer-0 event for promotion.
func (g *CascadeGates) ShouldPromoteToLayer1(e *types.Event) PromotionDecision {
	if e.PromotionEligibleAt > 0 && g.clock.NowMillis() < e.PromotionEligibleAt {
		return PromotionDecision{Reason: types.ReasonHighSalience, Details: "Minimum gate time not reached"}
	}
	w := g.cfg.SalienceWeights
	salience := e.Scores.RawSalience
	if e.Scores.Layer0Score != nil {
		salience = *e.Scores.Layer0Score
	}
	repeatFactor := math.Min(float64(e.Metadata.RepetitionCount)/10.0, 1)
	rewardFactor := 0.0
	if e.Metadata.Reward != nil {
		rewardFactor = math.Min(math.Abs(*e.Metadata.Reward), 1)
	}
	combined := w.Alpha*salience + w.Beta*repeatFactor + w.Gamma*rewardFactor
	threshold := g.cfg.PromoThreshold[0]

	reason := types.ReasonHighSalience
	switch {
	case repeatFactor > 0.5:
		reason = types.ReasonRepeatedExposure
	case rewardFactor > 0.7:
		reason = types.ReasonTaskReward
	}

	return PromotionDecision{
		ShouldPromote: combined >= threshold,
		Reason:        reason,
		Score:         combined,
		Details: fmt.Sprintf("salience: %.3f, repeat: %.3f, reward: %.3f, combined: %.3f, threshold: %.3f",
			salience, repeatFactor, rewardFactor, combined, threshold),
	}
}

// ShouldPromoteToLayer2 evaluates a Layer-1 event for promotion.
// midTerm is required so we can look up the event's graph centrality.
func (g *CascadeGates) ShouldPromoteToLayer2(e *types.Event, midTerm *storage.MidTermStore) (PromotionDecision, error) {
	ageSec := float64(g.clock.NowMillis()-e.CreatedAt) / 1000.0
	minWait := g.cfg.Replay.MinWaitTime.Seconds() * 2
	if ageSec < minWait {
		return PromotionDecision{Reason: types.ReasonStructuralSupport, Details: "Minimum consolidation time not reached"}, nil
	}
	layer1Score := e.Scores.RawSalience
	switch {
	case e.Scores.Layer1Score != nil:
		layer1Score = *e.Scores.Layer1Score
	case e.Scores.Layer0Score != nil:
		layer1Score = *e.Scores.Layer0Score
	}
	centrality, err := midTerm.Centrality(e.ID)
	if err != nil {
		return PromotionDecision{}, err
	}
	replayCount := 0
	for _, h := range e.History {
		if h.Action == types.ActionReplayed {
			replayCount++
		}
	}
	replayScore := math.Min(float64(replayCount)/5.0, 1)
	stabilityScore := math.Min(ageSec/86400.0/7.0, 1)
	combined := 0.3*layer1Score + 0.3*centrality + 0.2*replayScore + 0.2*stabilityScore
	threshold := g.cfg.PromoThreshold[1]
	reason := types.ReasonStructuralSupport
	if replayScore > 0.7 {
		reason = types.ReasonReplayConsolidation
	}
	return PromotionDecision{
		ShouldPromote: combined >= threshold,
		Reason:        reason,
		Score:         combined,
		Details: fmt.Sprintf("layer1: %.3f, structural: %.3f, replay: %.3f, stability: %.3f, combined: %.3f, threshold: %.3f",
			layer1Score, centrality, replayScore, stabilityScore, combined, threshold),
	}, nil
}

// MarkPromoted mutates the event to record promotion to the new layer.
func (g *CascadeGates) MarkPromoted(e *types.Event, to types.LayerState, reason types.PromotionReason, score float64) {
	e.LayerState = to
	switch to {
	case types.MidTerm:
		e.Scores.Layer1Score = &score
	case types.LongTerm:
		e.Scores.Layer2Score = &score
	}
	now := g.clock.NowMillis()
	layerCopy := to
	scoreCopy := score
	e.History = append(e.History, types.HistoryEntry{
		Action:  types.ActionPromoted,
		TS:      now,
		ToLayer: &layerCopy,
		Reason:  string(reason),
		Score:   &scoreCopy,
	})
	e.PromotionEligibleAt = now + g.cfg.Replay.MinWaitTime.Milliseconds()
}
