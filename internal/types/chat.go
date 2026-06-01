package types

import "time"

// ChatMessage is an append-only audit record of a single turn. It never feeds
// retrieval or promotion — it exists for debugging, replay, and analytics.
type ChatMessage struct {
	ID              string         `json:"id"`
	UserID          string         `json:"userId"`
	SessionID       string         `json:"sessionId"`
	TurnIndex       int            `json:"turnIndex"`
	Role            string         `json:"role"` // user | assistant | system | tool
	AgentName       string         `json:"agentName,omitempty"`
	Content         string         `json:"content"`
	IntentSnapshot  map[string]any `json:"intentSnapshot,omitempty"`
	ProfileSnapshot map[string]any `json:"profileSnapshot,omitempty"`
	ReviewPassed    *bool          `json:"reviewPassed,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}
