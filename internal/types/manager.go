package types

import "context"

// Manager is the public contract of the memory engine. CascadeMemorySystem
// is the canonical implementation; tests can swap in fakes.
//
// All methods accept a context.Context so callers can apply cancellation
// and deadlines. The cascade (Ingest/Retrieve/...) and the session-side
// (Profile/Summary/Chat) APIs share this single facade by design — same
// guarantees, same failure modes.
type Manager interface {
	// Cascade-side: event ingestion, retrieval, maintenance.
	Ingest(ctx context.Context, raw RawEvent) (*Event, error)
	IngestBatch(ctx context.Context, raws []RawEvent) ([]*Event, error)
	IngestAsync(ctx context.Context, raw RawEvent) <-chan IngestResult
	Retrieve(ctx context.Context, q RetrievalQuery) ([]RetrievalResult, error)
	GetEvent(ctx context.Context, id string) (*Event, error)
	DeleteEvent(ctx context.Context, id string) (bool, error)

	// Session context (Layer-0 of conversation state).
	ReadSessionContext(ctx context.Context, userID, sessionID string) (*SessionContext, error)
	WriteSessionContext(ctx context.Context, sc *SessionContext) error

	// User profile (Layer-1 structured long-term portrait).
	ReadUserProfile(ctx context.Context, userID string) (*UserProfile, error)
	WriteUserProfile(ctx context.Context, p *UserProfile) error

	// Session summary (rolling compression of history).
	UpsertSessionSummary(ctx context.Context, s *SessionSummary) error
	ListSessionSummaries(ctx context.Context, userID, sessionID string) ([]*SessionSummary, error)

	// Chat audit log (append-only).
	WriteChatMessage(ctx context.Context, msg *ChatMessage) error
	ReadRecentChatMessages(ctx context.Context, userID, sessionID string, limit int) ([]*ChatMessage, error)

	// Close releases the underlying storage handles.
	Close() error
}

// RawEvent is the input to Ingest. Declared here (not core) so callers can
// depend only on the types package.
type RawEvent struct {
	Content   any
	Source    string
	ContextID string
	UserID    string
	SessionID string
	AgentName string
	Reward    *float64
	Tags      []string
	Vector    []float64 // optional pre-computed embedding
}

// IngestResult is what IngestAsync delivers when the background write finishes.
type IngestResult struct {
	Event *Event
	Err   error
}
