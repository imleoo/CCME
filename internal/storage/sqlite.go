package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// Index is the SQLite-backed index over markdown event files.
// Schema layout:
//   events(id, layer, context_id, user_id, session_id, agent_name, source,
//          ts, created_at, last_accessed_at, promotion_eligible_at,
//          score, reward, repetition_count, vector BLOB, file_path)
//   tags(event_id, tag)
//   associations(a_id, b_id)              -- mid-term layer graph
//   schemas(id, summary, vector, importance, created_at, last_updated_at, file_path)
//   schema_sources(schema_id, event_id)
//   user_profiles(user_id, payload, file_path, created_at, updated_at)
//   session_contexts(user_id, session_id, payload, file_path, updated_at)
//   session_summaries(id, user_id, session_id, turn_range_start, turn_range_end,
//                     summary, file_path, created_at)
//   chat_messages(id, user_id, session_id, turn_index, role, agent_name,
//                 content, intent_snapshot, profile_snapshot, review_passed, created_at)
type Index struct {
	db *sql.DB
}

// OpenIndex opens (or creates) the SQLite index at the given path.
func OpenIndex(ctx context.Context, path string) (*Index, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	idx := &Index{db: db}
	if err := idx.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

// Close releases the underlying database handle.
func (i *Index) Close() error { return i.db.Close() }

// DB exposes the raw *sql.DB — useful for tests.
func (i *Index) DB() *sql.DB { return i.db }

func (i *Index) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			layer INTEGER NOT NULL,
			context_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL,
			ts INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			last_accessed_at INTEGER NOT NULL,
			promotion_eligible_at INTEGER NOT NULL DEFAULT 0,
			score REAL NOT NULL,
			reward REAL,
			repetition_count INTEGER NOT NULL DEFAULT 0,
			vector BLOB NOT NULL,
			file_path TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_layer ON events(layer)`,
		`CREATE INDEX IF NOT EXISTS idx_events_context ON events(context_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_user ON events(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session ON events(user_id, session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_score ON events(score)`,
		`CREATE TABLE IF NOT EXISTS tags (
			event_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (event_id, tag),
			FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag)`,
		`CREATE TABLE IF NOT EXISTS associations (
			a_id TEXT NOT NULL,
			b_id TEXT NOT NULL,
			PRIMARY KEY (a_id, b_id),
			FOREIGN KEY (a_id) REFERENCES events(id) ON DELETE CASCADE,
			FOREIGN KEY (b_id) REFERENCES events(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_assoc_b ON associations(b_id)`,
		`CREATE TABLE IF NOT EXISTS schemas (
			id TEXT PRIMARY KEY,
			summary TEXT NOT NULL,
			vector BLOB NOT NULL,
			importance REAL NOT NULL,
			created_at INTEGER NOT NULL,
			last_updated_at INTEGER NOT NULL,
			file_path TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS schema_sources (
			schema_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			PRIMARY KEY (schema_id, event_id),
			FOREIGN KEY (schema_id) REFERENCES schemas(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS user_profiles (
			user_id TEXT PRIMARY KEY,
			payload TEXT NOT NULL,
			file_path TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS session_contexts (
			user_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			payload TEXT NOT NULL,
			file_path TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, session_id)
		)`,
		`CREATE TABLE IF NOT EXISTS session_summaries (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			turn_range_start INTEGER NOT NULL,
			turn_range_end INTEGER NOT NULL,
			summary TEXT NOT NULL,
			file_path TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_summary_session ON session_summaries(user_id, session_id, turn_range_start)`,
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			turn_index INTEGER NOT NULL,
			role TEXT NOT NULL,
			agent_name TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			intent_snapshot TEXT,
			profile_snapshot TEXT,
			review_passed INTEGER,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_session ON chat_messages(user_id, session_id, turn_index)`,
	}
	for _, s := range stmts {
		if _, err := i.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate: %w (%s)", err, s)
		}
	}
	return nil
}

// UpsertEvent writes (or overwrites) the index row for an event.
func (i *Index) UpsertEvent(ctx context.Context, e *types.Event, filePath string) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var reward sql.NullFloat64
	if e.Metadata.Reward != nil {
		reward = sql.NullFloat64{Float64: *e.Metadata.Reward, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events
		(id, layer, context_id, user_id, session_id, agent_name, source, ts,
		 created_at, last_accessed_at, promotion_eligible_at, score, reward,
		 repetition_count, vector, file_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			layer=excluded.layer,
			context_id=excluded.context_id,
			user_id=excluded.user_id,
			session_id=excluded.session_id,
			agent_name=excluded.agent_name,
			source=excluded.source,
			ts=excluded.ts,
			last_accessed_at=excluded.last_accessed_at,
			promotion_eligible_at=excluded.promotion_eligible_at,
			score=excluded.score,
			reward=excluded.reward,
			repetition_count=excluded.repetition_count,
			vector=excluded.vector,
			file_path=excluded.file_path
	`,
		e.ID, int(e.LayerState), e.Metadata.ContextID,
		e.Metadata.UserID, e.Metadata.SessionID, e.Metadata.AgentName,
		e.Metadata.Source, e.Metadata.TS, e.CreatedAt, e.LastAccessedAt,
		e.PromotionEligibleAt, e.CurrentScore(), reward, e.Metadata.RepetitionCount,
		util.EncodeVector(e.Vector), filePath,
	)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE event_id = ?`, e.ID); err != nil {
		return err
	}
	for _, tag := range e.Metadata.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(event_id, tag) VALUES (?, ?)`, e.ID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteEvent removes the row (and tag/association rows via cascade).
func (i *Index) DeleteEvent(ctx context.Context, id string) (bool, error) {
	res, err := i.db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// EventRow is the SQLite-side row plus the decoded vector and tags.
type EventRow struct {
	ID                  string
	Layer               types.LayerState
	ContextID           string
	UserID              string
	SessionID           string
	AgentName           string
	Source              string
	TS                  int64
	CreatedAt           int64
	LastAccessedAt      int64
	PromotionEligibleAt int64
	Score               float64
	Reward              *float64
	RepetitionCount     int
	Vector              []float64
	FilePath            string
	Tags                []string
}

func scanEvent(row interface {
	Scan(dest ...any) error
}) (*EventRow, error) {
	var (
		r        EventRow
		reward   sql.NullFloat64
		layerInt int
		blob     []byte
	)
	if err := row.Scan(
		&r.ID, &layerInt, &r.ContextID, &r.UserID, &r.SessionID, &r.AgentName,
		&r.Source, &r.TS, &r.CreatedAt, &r.LastAccessedAt, &r.PromotionEligibleAt,
		&r.Score, &reward, &r.RepetitionCount, &blob, &r.FilePath,
	); err != nil {
		return nil, err
	}
	r.Layer = types.LayerState(layerInt)
	if reward.Valid {
		v := reward.Float64
		r.Reward = &v
	}
	vec, err := util.DecodeVector(blob)
	if err != nil {
		return nil, err
	}
	r.Vector = vec
	return &r, nil
}

const eventColumns = `id, layer, context_id, user_id, session_id, agent_name,
		source, ts, created_at, last_accessed_at, promotion_eligible_at,
		score, reward, repetition_count, vector, file_path`

// ListByLayer streams all rows belonging to a layer.
func (i *Index) ListByLayer(ctx context.Context, layer types.LayerState) ([]*EventRow, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT `+eventColumns+` FROM events WHERE layer = ?`, int(layer))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return i.collectAndAttachTags(ctx, rows)
}

// GetByID returns the row for an event id, or nil if not found.
func (i *Index) GetByID(ctx context.Context, id string) (*EventRow, error) {
	row := i.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = ?`, id)
	r, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tags, err := i.tagsFor(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	r.Tags = tags
	return r, nil
}

// CountByLayer returns the number of events stored in a layer.
func (i *Index) CountByLayer(ctx context.Context, layer types.LayerState) (int, error) {
	var n int
	err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE layer = ?`, int(layer)).Scan(&n)
	return n, err
}

// SearchFilters lets the Search call mix layer/context/user/session/tag filters.
type SearchFilters struct {
	Layer     *types.LayerState
	ContextID string
	UserID    string
	SessionID string
	Tags      []string
}

// Search filters rows and returns them. Vector ranking is done by the caller.
func (i *Index) Search(ctx context.Context, f SearchFilters) ([]*EventRow, error) {
	var (
		conds []string
		args  []any
	)
	q := `SELECT ` + prefixCols("e.") + ` FROM events e`
	// Tags JOIN must bind before WHERE clauses so placeholders line up.
	if len(f.Tags) > 0 {
		placeholders := strings.Repeat("?,", len(f.Tags))
		placeholders = strings.TrimRight(placeholders, ",")
		q += ` JOIN tags t ON t.event_id = e.id AND t.tag IN (` + placeholders + `)`
		for _, t := range f.Tags {
			args = append(args, t)
		}
	}
	if f.Layer != nil {
		conds = append(conds, "e.layer = ?")
		args = append(args, int(*f.Layer))
	}
	if f.ContextID != "" {
		conds = append(conds, "e.context_id = ?")
		args = append(args, f.ContextID)
	}
	if f.UserID != "" {
		conds = append(conds, "e.user_id = ?")
		args = append(args, f.UserID)
	}
	if f.SessionID != "" {
		conds = append(conds, "e.session_id = ?")
		args = append(args, f.SessionID)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, " AND ")
	}
	q += ` GROUP BY e.id`
	rows, err := i.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return i.collectAndAttachTags(ctx, rows)
}

func prefixCols(prefix string) string {
	parts := strings.Split(eventColumns, ",")
	for idx, p := range parts {
		parts[idx] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func (i *Index) collectAndAttachTags(ctx context.Context, rows *sql.Rows) ([]*EventRow, error) {
	var out []*EventRow
	for rows.Next() {
		r, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, r := range out {
		tags, err := i.tagsFor(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		r.Tags = tags
	}
	return out, nil
}

func (i *Index) tagsFor(ctx context.Context, id string) ([]string, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT tag FROM tags WHERE event_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// --- associations (mid-term layer graph) ---

// AddAssociation records a bidirectional edge between two events.
func (i *Index) AddAssociation(ctx context.Context, a, b string) error {
	if a == b {
		return nil
	}
	if a > b {
		a, b = b, a
	}
	_, err := i.db.ExecContext(ctx, `INSERT OR IGNORE INTO associations(a_id, b_id) VALUES (?, ?)`, a, b)
	return err
}

// NeighborCount returns the number of events directly associated with id.
func (i *Index) NeighborCount(ctx context.Context, id string) (int, error) {
	var n int
	err := i.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM associations WHERE a_id = ? OR b_id = ?
	`, id, id).Scan(&n)
	return n, err
}

// Neighbors returns the IDs of all events associated with id.
func (i *Index) Neighbors(ctx context.Context, id string) ([]string, error) {
	rows, err := i.db.QueryContext(ctx, `
		SELECT CASE WHEN a_id = ? THEN b_id ELSE a_id END
		FROM associations WHERE a_id = ? OR b_id = ?
	`, id, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TotalAssociations returns the count of bidirectional edges (each stored once).
func (i *Index) TotalAssociations(ctx context.Context) (int, error) {
	var n int
	err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM associations`).Scan(&n)
	return n, err
}

// RemoveAssociations removes every edge touching id.
func (i *Index) RemoveAssociations(ctx context.Context, id string) error {
	_, err := i.db.ExecContext(ctx, `DELETE FROM associations WHERE a_id = ? OR b_id = ?`, id, id)
	return err
}

// --- schemas (long-term consolidation) ---

// SchemaRow mirrors the schemas row.
type SchemaRow struct {
	ID               string
	Summary          string
	Vector           []float64
	Importance       float64
	CreatedAt        int64
	LastUpdatedAt    int64
	FilePath         string
	ConsolidatedFrom []string
}

// UpsertSchema persists a schema record.
func (i *Index) UpsertSchema(ctx context.Context, s *SchemaRow) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO schemas (id, summary, vector, importance, created_at, last_updated_at, file_path)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			summary=excluded.summary,
			vector=excluded.vector,
			importance=excluded.importance,
			last_updated_at=excluded.last_updated_at,
			file_path=excluded.file_path
	`, s.ID, s.Summary, util.EncodeVector(s.Vector), s.Importance,
		s.CreatedAt, s.LastUpdatedAt, s.FilePath)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_sources WHERE schema_id = ?`, s.ID); err != nil {
		return err
	}
	for _, src := range s.ConsolidatedFrom {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_sources(schema_id, event_id) VALUES (?, ?)`, s.ID, src); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CountSchemas returns the number of stored schemas.
func (i *Index) CountSchemas(ctx context.Context) (int, error) {
	var n int
	err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schemas`).Scan(&n)
	return n, err
}

// ClearAll wipes every table - used by CascadeMemorySystem.Clear().
func (i *Index) ClearAll(ctx context.Context) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	tables := []string{
		"schema_sources", "schemas", "associations", "tags", "events",
		"user_profiles", "session_contexts", "session_summaries", "chat_messages",
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- user profiles ---

// UpsertUserProfileRow persists a user profile row.
func (i *Index) UpsertUserProfileRow(ctx context.Context, userID, payload, filePath string, createdAt, updatedAt int64) error {
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO user_profiles (user_id, payload, file_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			payload=excluded.payload,
			file_path=excluded.file_path,
			updated_at=excluded.updated_at
	`, userID, payload, filePath, createdAt, updatedAt)
	return err
}

// UserProfileRow is the stored profile envelope.
type UserProfileRow struct {
	UserID    string
	Payload   string
	FilePath  string
	CreatedAt int64
	UpdatedAt int64
}

// GetUserProfileRow returns the profile row (nil if absent).
func (i *Index) GetUserProfileRow(ctx context.Context, userID string) (*UserProfileRow, error) {
	row := i.db.QueryRowContext(ctx, `
		SELECT user_id, payload, file_path, created_at, updated_at
		FROM user_profiles WHERE user_id = ?
	`, userID)
	var r UserProfileRow
	err := row.Scan(&r.UserID, &r.Payload, &r.FilePath, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// --- session contexts ---

// UpsertSessionContextRow persists a session context row.
func (i *Index) UpsertSessionContextRow(ctx context.Context, userID, sessionID, payload, filePath string, updatedAt int64) error {
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO session_contexts (user_id, session_id, payload, file_path, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, session_id) DO UPDATE SET
			payload=excluded.payload,
			file_path=excluded.file_path,
			updated_at=excluded.updated_at
	`, userID, sessionID, payload, filePath, updatedAt)
	return err
}

// SessionContextRow is the stored session envelope.
type SessionContextRow struct {
	UserID    string
	SessionID string
	Payload   string
	FilePath  string
	UpdatedAt int64
}

// GetSessionContextRow returns the session row (nil if absent).
func (i *Index) GetSessionContextRow(ctx context.Context, userID, sessionID string) (*SessionContextRow, error) {
	row := i.db.QueryRowContext(ctx, `
		SELECT user_id, session_id, payload, file_path, updated_at
		FROM session_contexts WHERE user_id = ? AND session_id = ?
	`, userID, sessionID)
	var r SessionContextRow
	err := row.Scan(&r.UserID, &r.SessionID, &r.Payload, &r.FilePath, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// --- session summaries ---

// SessionSummaryRow is the stored summary envelope.
type SessionSummaryRow struct {
	ID             string
	UserID         string
	SessionID      string
	TurnRangeStart int
	TurnRangeEnd   int
	Summary        string
	FilePath       string
	CreatedAt      int64
}

// UpsertSessionSummaryRow replaces all summaries for (userID, sessionID) with a
// single rolling row. Delete + insert in one tx.
func (i *Index) UpsertSessionSummaryRow(ctx context.Context, r *SessionSummaryRow) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_summaries WHERE user_id = ? AND session_id = ?
	`, r.UserID, r.SessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_summaries
		(id, user_id, session_id, turn_range_start, turn_range_end, summary, file_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.UserID, r.SessionID, r.TurnRangeStart, r.TurnRangeEnd, r.Summary, r.FilePath, r.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSessionSummaryRows returns summaries ordered by turn_range_start ASC.
// If sessionID == "" returns every session for the user.
func (i *Index) ListSessionSummaryRows(ctx context.Context, userID, sessionID string) ([]*SessionSummaryRow, error) {
	var (
		q    = `SELECT id, user_id, session_id, turn_range_start, turn_range_end, summary, file_path, created_at FROM session_summaries WHERE user_id = ?`
		args = []any{userID}
	)
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY turn_range_start ASC`
	rows, err := i.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SessionSummaryRow
	for rows.Next() {
		var r SessionSummaryRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.SessionID,
			&r.TurnRangeStart, &r.TurnRangeEnd, &r.Summary, &r.FilePath, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// --- chat messages (append-only audit) ---

// ChatMessageRow is one stored chat message.
type ChatMessageRow struct {
	ID              string
	UserID          string
	SessionID       string
	TurnIndex       int
	Role            string
	AgentName       string
	Content         string
	IntentSnapshot  sql.NullString
	ProfileSnapshot sql.NullString
	ReviewPassed    sql.NullInt64
	CreatedAt       int64
}

// InsertChatMessage appends a single chat message row.
func (i *Index) InsertChatMessage(ctx context.Context, r *ChatMessageRow) error {
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO chat_messages
		(id, user_id, session_id, turn_index, role, agent_name, content,
		 intent_snapshot, profile_snapshot, review_passed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.UserID, r.SessionID, r.TurnIndex, r.Role, r.AgentName,
		r.Content, r.IntentSnapshot, r.ProfileSnapshot, r.ReviewPassed, r.CreatedAt)
	return err
}

// RecentChatMessages returns up to `limit` messages, most recent first
// (ORDER BY turn_index DESC). Callers may reverse for chronological order.
func (i *Index) RecentChatMessages(ctx context.Context, userID, sessionID string, limit int) ([]*ChatMessageRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := i.db.QueryContext(ctx, `
		SELECT id, user_id, session_id, turn_index, role, agent_name, content,
		       intent_snapshot, profile_snapshot, review_passed, created_at
		FROM chat_messages
		WHERE user_id = ? AND session_id = ?
		ORDER BY turn_index DESC
		LIMIT ?
	`, userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ChatMessageRow
	for rows.Next() {
		var r ChatMessageRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.SessionID, &r.TurnIndex, &r.Role,
			&r.AgentName, &r.Content, &r.IntentSnapshot, &r.ProfileSnapshot,
			&r.ReviewPassed, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
