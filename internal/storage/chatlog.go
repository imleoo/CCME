package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// ChatLog is an append-only audit store for chat messages. It is intentionally
// SQLite-only — message volume is high and these rows do not feed retrieval.
type ChatLog struct {
	idx   *Index
	clock util.Clock
}

// NewChatLog constructs a chat log writer.
func NewChatLog(idx *Index, clock util.Clock) *ChatLog {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &ChatLog{idx: idx, clock: clock}
}

// Write appends one chat message. ID, UserID, SessionID, Role are required.
func (c *ChatLog) Write(ctx context.Context, msg *types.ChatMessage) error {
	if msg == nil {
		return errors.New("ChatLog.Write: nil message")
	}
	if msg.ID == "" || msg.UserID == "" || msg.SessionID == "" || msg.Role == "" {
		return errors.New("ChatLog.Write: id, userID, sessionID, role required")
	}
	created := msg.CreatedAt
	if created.IsZero() {
		created = time.UnixMilli(c.clock.NowMillis()).UTC()
		msg.CreatedAt = created
	}
	row := &ChatMessageRow{
		ID:        msg.ID,
		UserID:    msg.UserID,
		SessionID: msg.SessionID,
		TurnIndex: msg.TurnIndex,
		Role:      msg.Role,
		AgentName: msg.AgentName,
		Content:   msg.Content,
		CreatedAt: created.UnixMilli(),
	}
	if msg.IntentSnapshot != nil {
		if raw, err := json.Marshal(msg.IntentSnapshot); err == nil {
			row.IntentSnapshot = sql.NullString{String: string(raw), Valid: true}
		}
	}
	if msg.ProfileSnapshot != nil {
		if raw, err := json.Marshal(msg.ProfileSnapshot); err == nil {
			row.ProfileSnapshot = sql.NullString{String: string(raw), Valid: true}
		}
	}
	if msg.ReviewPassed != nil {
		v := int64(0)
		if *msg.ReviewPassed {
			v = 1
		}
		row.ReviewPassed = sql.NullInt64{Int64: v, Valid: true}
	}
	return c.idx.InsertChatMessage(ctx, row)
}

// Recent returns up to `limit` messages, most recent first.
func (c *ChatLog) Recent(ctx context.Context, userID, sessionID string, limit int) ([]*types.ChatMessage, error) {
	rows, err := c.idx.RecentChatMessages(ctx, userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*types.ChatMessage, 0, len(rows))
	for _, r := range rows {
		msg := &types.ChatMessage{
			ID:        r.ID,
			UserID:    r.UserID,
			SessionID: r.SessionID,
			TurnIndex: r.TurnIndex,
			Role:      r.Role,
			AgentName: r.AgentName,
			Content:   r.Content,
			CreatedAt: time.UnixMilli(r.CreatedAt).UTC(),
		}
		if r.IntentSnapshot.Valid {
			var m map[string]any
			if err := json.Unmarshal([]byte(r.IntentSnapshot.String), &m); err == nil {
				msg.IntentSnapshot = m
			}
		}
		if r.ProfileSnapshot.Valid {
			var m map[string]any
			if err := json.Unmarshal([]byte(r.ProfileSnapshot.String), &m); err == nil {
				msg.ProfileSnapshot = m
			}
		}
		if r.ReviewPassed.Valid {
			v := r.ReviewPassed.Int64 == 1
			msg.ReviewPassed = &v
		}
		out = append(out, msg)
	}
	return out, nil
}
