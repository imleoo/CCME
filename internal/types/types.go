// Package types defines the core data structures of the ChronoCascade Memory Engine.
// These types map biological memory concepts to software components.
package types

// LayerState identifies which layer of the cascade an event currently lives in.
type LayerState int

const (
	ShortTerm LayerState = 0 // Layer 0 - Thalamus / CAMTA1
	MidTerm   LayerState = 1 // Layer 1 - TCF4 / Structural support
	LongTerm  LayerState = 2 // Layer 2 - ASH1L / Chromatin remodeling
)

func (l LayerState) String() string {
	switch l {
	case ShortTerm:
		return "short-term"
	case MidTerm:
		return "mid-term"
	case LongTerm:
		return "long-term"
	default:
		return "unknown"
	}
}

// EventMetadata carries the descriptive context of an event.
//
// UserID/SessionID isolate memories belonging to different users or chat
// sessions; they are optional — empty strings mean "global / not attributed".
type EventMetadata struct {
	TS              int64    `yaml:"ts" json:"ts"`
	Source          string   `yaml:"source" json:"source"`
	ContextID       string   `yaml:"contextId" json:"contextId"`
	UserID          string   `yaml:"userId,omitempty" json:"userId,omitempty"`
	SessionID       string   `yaml:"sessionId,omitempty" json:"sessionId,omitempty"`
	AgentName       string   `yaml:"agentName,omitempty" json:"agentName,omitempty"`
	Reward          *float64 `yaml:"reward,omitempty" json:"reward,omitempty"`
	Tags            []string `yaml:"tags" json:"tags"`
	RepetitionCount int      `yaml:"repetitionCount" json:"repetitionCount"`
}

// HistoryAction enumerates lifecycle events recorded on each Event.
type HistoryAction string

const (
	ActionSeen     HistoryAction = "seen"
	ActionPromoted HistoryAction = "promoted"
	ActionDecayed  HistoryAction = "decayed"
	ActionReplayed HistoryAction = "replayed"
	ActionPruned   HistoryAction = "pruned"
)

// HistoryEntry is one breadcrumb on the lifecycle trail of an Event.
type HistoryEntry struct {
	Action  HistoryAction `yaml:"action" json:"action"`
	TS      int64         `yaml:"ts" json:"ts"`
	ToLayer *LayerState   `yaml:"toLayer,omitempty" json:"toLayer,omitempty"`
	Reason  string        `yaml:"reason,omitempty" json:"reason,omitempty"`
	Score   *float64      `yaml:"score,omitempty" json:"score,omitempty"`
}

// Scores aggregates the salience signals computed at each layer.
type Scores struct {
	RawSalience float64  `yaml:"rawSalience" json:"rawSalience"`
	Layer0Score *float64 `yaml:"layer0Score,omitempty" json:"layer0Score,omitempty"`
	Layer1Score *float64 `yaml:"layer1Score,omitempty" json:"layer1Score,omitempty"`
	Layer2Score *float64 `yaml:"layer2Score,omitempty" json:"layer2Score,omitempty"`
}

// Event is the basic unit of memory in the cascade.
type Event struct {
	ID                  string         `yaml:"id" json:"id"`
	Vector              []float64      `yaml:"vector" json:"vector"`
	Metadata            EventMetadata  `yaml:"metadata" json:"metadata"`
	LayerState          LayerState     `yaml:"layerState" json:"layerState"`
	Scores              Scores         `yaml:"scores" json:"scores"`
	History             []HistoryEntry `yaml:"history" json:"history"`
	CreatedAt           int64          `yaml:"createdAt" json:"createdAt"`
	LastAccessedAt      int64          `yaml:"lastAccessedAt" json:"lastAccessedAt"`
	PromotionEligibleAt int64          `yaml:"promotionEligibleAt,omitempty" json:"promotionEligibleAt,omitempty"`
	Content             any            `yaml:"-" json:"-"` // markdown body, not stored in frontmatter
	ContentRaw          string         `yaml:"-" json:"-"` // original raw content (string form) for round-trip
}

// PromotionReason describes why an event moved up the cascade.
type PromotionReason string

const (
	ReasonHighSalience        PromotionReason = "high_salience"
	ReasonRepeatedExposure    PromotionReason = "repeated_exposure"
	ReasonTaskReward          PromotionReason = "task_reward"
	ReasonStructuralSupport   PromotionReason = "structural_support"
	ReasonReplayConsolidation PromotionReason = "replay_consolidation"
	ReasonManualOverride      PromotionReason = "manual_override"
)

// ForgettingReason describes why an event was pruned from a layer.
type ForgettingReason string

const (
	ForgetLowScore      ForgettingReason = "low_score"
	ForgetExpired       ForgettingReason = "expired"
	ForgetCapacityLimit ForgettingReason = "capacity_limit"
	ForgetLowUtility    ForgettingReason = "low_utility"
	ForgetManualDelete  ForgettingReason = "manual_delete"
)

// RetrievalQuery is the search input passed to memory stores.
type RetrievalQuery struct {
	Vector    []float64
	ContextID string
	UserID    string
	SessionID string
	Tags      []string
	MinScore  *float64
	Layer     *LayerState
	TopK      int
}

// RetrievalResult is a single search hit.
type RetrievalResult struct {
	Event           *Event
	Similarity      float64
	HasSimilarity   bool
	RetrievalReason string
}

// LayerStats summarises one store's occupancy.
type LayerStats struct {
	Layer           LayerState
	Size            int
	Capacity        int
	UtilizationRate float64
	AvgScore        float64
	MaxScore        float64
	MinScore        float64
}

// CurrentScore returns the score that matters for the event's current layer.
func (e *Event) CurrentScore() float64 {
	switch e.LayerState {
	case ShortTerm:
		if e.Scores.Layer0Score != nil {
			return *e.Scores.Layer0Score
		}
	case MidTerm:
		if e.Scores.Layer1Score != nil {
			return *e.Scores.Layer1Score
		}
		if e.Scores.Layer0Score != nil {
			return *e.Scores.Layer0Score
		}
	case LongTerm:
		if e.Scores.Layer2Score != nil {
			return *e.Scores.Layer2Score
		}
		if e.Scores.Layer1Score != nil {
			return *e.Scores.Layer1Score
		}
		if e.Scores.Layer0Score != nil {
			return *e.Scores.Layer0Score
		}
	}
	return e.Scores.RawSalience
}

// Float64Ptr is a tiny helper for optional scores.
func Float64Ptr(v float64) *float64 { return &v }
