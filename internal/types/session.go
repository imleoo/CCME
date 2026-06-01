package types

import "time"

// Message is one turn of a multi-turn dialogue.
type Message struct {
	Role    string `json:"role" yaml:"role"`
	Content string `json:"content" yaml:"content"`
}

// SessionContext is the short-term running state of a chat session.
// It is keyed by (userID, sessionID) and held in a single document.
type SessionContext struct {
	UserID        string                   `json:"userId" yaml:"userId"`
	SessionID     string                   `json:"sessionId" yaml:"sessionId"`
	History       []Message                `json:"history" yaml:"history"`
	LastAgent     string                   `json:"lastAgent,omitempty" yaml:"lastAgent,omitempty"`
	TurnCount     int                      `json:"turnCount" yaml:"turnCount"`
	SessionStates map[string]*SessionState `json:"sessionStates,omitempty" yaml:"sessionStates,omitempty"`
	UpdatedAt     time.Time                `json:"updatedAt" yaml:"updatedAt"`
}

// SessionState carries per-session task/goal/intervention state.
type SessionState struct {
	TurnIndex          int    `json:"turnIndex,omitempty" yaml:"turnIndex,omitempty"`
	LastAgent          string `json:"lastAgent,omitempty" yaml:"lastAgent,omitempty"`
	MainAgent          string `json:"mainAgent,omitempty" yaml:"mainAgent,omitempty"`
	SessionGoal        string `json:"sessionGoal,omitempty" yaml:"sessionGoal,omitempty"`
	CurrentProblem     string `json:"currentProblem,omitempty" yaml:"currentProblem,omitempty"`
	ChosenIntervention string `json:"chosenIntervention,omitempty" yaml:"chosenIntervention,omitempty"`
	MinimalAction      string `json:"minimalAction,omitempty" yaml:"minimalAction,omitempty"`
	Status             string `json:"status,omitempty" yaml:"status,omitempty"` // active | completed | stalled
	CurrentPhase       string `json:"currentPhase,omitempty" yaml:"currentPhase,omitempty"`
}

// SessionSummary is a rolling summary of a slice of conversation history.
// Produced when SessionContext.History grows past a threshold so that the
// running buffer can be truncated while still preserving the gist.
type SessionSummary struct {
	ID             string    `json:"id" yaml:"id"`
	UserID         string    `json:"userId" yaml:"userId"`
	SessionID      string    `json:"sessionId" yaml:"sessionId"`
	TurnRangeStart int       `json:"turnRangeStart" yaml:"turnRangeStart"`
	TurnRangeEnd   int       `json:"turnRangeEnd" yaml:"turnRangeEnd"`
	Summary        string    `json:"summary" yaml:"summary"`
	CreatedAt      time.Time `json:"createdAt" yaml:"createdAt"`
}
