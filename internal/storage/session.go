package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// SessionStore persists SessionContext + SessionSummary documents.
//
// Layout:
//
//	sessions/<user_id>/<session_id>.md      -- session context
//	summaries/<user_id>/<session_id>.md     -- rolling summary
type SessionStore struct {
	baseDir string
	idx     *Index
	clock   util.Clock
}

// NewSessionStore constructs a session store.
func NewSessionStore(baseDir string, idx *Index, clock util.Clock) *SessionStore {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &SessionStore{baseDir: baseDir, idx: idx, clock: clock}
}

func (s *SessionStore) sessionDir(userID string) string {
	return filepath.Join(s.baseDir, "sessions", userID)
}
func (s *SessionStore) sessionPath(userID, sessionID string) string {
	return filepath.Join(s.sessionDir(userID), sessionID+".md")
}
func (s *SessionStore) summaryDir(userID string) string {
	return filepath.Join(s.baseDir, "summaries", userID)
}
func (s *SessionStore) summaryPath(userID, sessionID string) string {
	return filepath.Join(s.summaryDir(userID), sessionID+".md")
}

// --- session context ---

// WriteContext persists the session context to MD + SQLite.
func (s *SessionStore) WriteContext(ctx context.Context, sc *types.SessionContext) error {
	if sc == nil {
		return errors.New("SessionStore.WriteContext: nil context")
	}
	if sc.UserID == "" || sc.SessionID == "" {
		return errors.New("SessionStore.WriteContext: userID and sessionID required")
	}
	sc.UpdatedAt = time.UnixMilli(s.clock.NowMillis()).UTC()
	payload, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := s.writeContextFile(sc); err != nil {
		return err
	}
	return s.idx.UpsertSessionContextRow(ctx, sc.UserID, sc.SessionID,
		string(payload), s.sessionPath(sc.UserID, sc.SessionID), sc.UpdatedAt.UnixMilli())
}

// ReadContext returns the session context (nil if not found).
func (s *SessionStore) ReadContext(ctx context.Context, userID, sessionID string) (*types.SessionContext, error) {
	if userID == "" || sessionID == "" {
		return nil, errors.New("SessionStore.ReadContext: userID and sessionID required")
	}
	row, err := s.idx.GetSessionContextRow(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	var sc types.SessionContext
	if err := json.Unmarshal([]byte(row.Payload), &sc); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &sc, nil
}

func (s *SessionStore) writeContextFile(sc *types.SessionContext) error {
	if err := os.MkdirAll(s.sessionDir(sc.UserID), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(struct {
		UserID    string    `yaml:"userId"`
		SessionID string    `yaml:"sessionId"`
		LastAgent string    `yaml:"lastAgent,omitempty"`
		TurnCount int       `yaml:"turnCount"`
		UpdatedAt time.Time `yaml:"updatedAt"`
	}{
		UserID:    sc.UserID,
		SessionID: sc.SessionID,
		LastAgent: sc.LastAgent,
		TurnCount: sc.TurnCount,
		UpdatedAt: sc.UpdatedAt,
	}); err != nil {
		return err
	}
	_ = enc.Close()
	buf.WriteString("---\n\n# Conversation\n\n")
	for i, msg := range sc.History {
		fmt.Fprintf(&buf, "## Turn %d — %s\n\n%s\n\n", i+1, msg.Role, msg.Content)
	}
	tmp := s.sessionPath(sc.UserID, sc.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.sessionPath(sc.UserID, sc.SessionID))
}

// --- session summaries ---

// UpsertSummary replaces the rolling summary for (user, session) with the given one.
func (s *SessionStore) UpsertSummary(ctx context.Context, summary *types.SessionSummary) error {
	if summary == nil {
		return errors.New("SessionStore.UpsertSummary: nil summary")
	}
	if summary.UserID == "" || summary.SessionID == "" {
		return errors.New("SessionStore.UpsertSummary: userID and sessionID required")
	}
	if summary.ID == "" {
		summary.ID = uuid.NewString()
	}
	summary.CreatedAt = time.UnixMilli(s.clock.NowMillis()).UTC()
	if err := s.writeSummaryFile(summary); err != nil {
		return err
	}
	return s.idx.UpsertSessionSummaryRow(ctx, &SessionSummaryRow{
		ID:             summary.ID,
		UserID:         summary.UserID,
		SessionID:      summary.SessionID,
		TurnRangeStart: summary.TurnRangeStart,
		TurnRangeEnd:   summary.TurnRangeEnd,
		Summary:        summary.Summary,
		FilePath:       s.summaryPath(summary.UserID, summary.SessionID),
		CreatedAt:      summary.CreatedAt.UnixMilli(),
	})
}

// ListSummaries returns rolling summaries ordered by turn_range_start ASC.
// If sessionID is empty, returns every session's summary for the user.
func (s *SessionStore) ListSummaries(ctx context.Context, userID, sessionID string) ([]*types.SessionSummary, error) {
	rows, err := s.idx.ListSessionSummaryRows(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]*types.SessionSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, &types.SessionSummary{
			ID:             r.ID,
			UserID:         r.UserID,
			SessionID:      r.SessionID,
			TurnRangeStart: r.TurnRangeStart,
			TurnRangeEnd:   r.TurnRangeEnd,
			Summary:        r.Summary,
			CreatedAt:      time.UnixMilli(r.CreatedAt).UTC(),
		})
	}
	return out, nil
}

func (s *SessionStore) writeSummaryFile(summary *types.SessionSummary) error {
	if err := os.MkdirAll(s.summaryDir(summary.UserID), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	fmt.Fprintf(&buf, "id: %s\n", summary.ID)
	fmt.Fprintf(&buf, "userId: %s\n", summary.UserID)
	fmt.Fprintf(&buf, "sessionId: %s\n", summary.SessionID)
	fmt.Fprintf(&buf, "turnRangeStart: %d\n", summary.TurnRangeStart)
	fmt.Fprintf(&buf, "turnRangeEnd: %d\n", summary.TurnRangeEnd)
	fmt.Fprintf(&buf, "createdAt: %s\n", summary.CreatedAt.Format(time.RFC3339))
	buf.WriteString("---\n\n")
	fmt.Fprintf(&buf, "# Session summary (turns %d–%d)\n\n%s\n",
		summary.TurnRangeStart, summary.TurnRangeEnd, summary.Summary)
	tmp := s.summaryPath(summary.UserID, summary.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.summaryPath(summary.UserID, summary.SessionID))
}
