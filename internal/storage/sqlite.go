package storage

import (
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
//   events(id, layer, context_id, ts, created_at, last_accessed_at,
//          promotion_eligible_at, score, reward, repetition_count, vector BLOB)
//   tags(event_id, tag)
//   associations(a_id, b_id)                  -- mid-term layer graph
//   schemas(id, summary, vector, importance, created_at, last_updated_at)
//   schema_sources(schema_id, event_id)
type Index struct {
	db *sql.DB
}

// OpenIndex opens (or creates) the SQLite index at the given path.
func OpenIndex(path string) (*Index, error) {
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
	if err := idx.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

// Close releases the underlying database handle.
func (i *Index) Close() error { return i.db.Close() }

// DB exposes the raw *sql.DB - useful for tests.
func (i *Index) DB() *sql.DB { return i.db }

func (i *Index) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			layer INTEGER NOT NULL,
			context_id TEXT NOT NULL,
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
	}
	for _, s := range stmts {
		if _, err := i.db.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w (%s)", err, s)
		}
	}
	return nil
}

// UpsertEvent writes (or overwrites) the index row for an event.
func (i *Index) UpsertEvent(e *types.Event, filePath string) error {
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var reward sql.NullFloat64
	if e.Metadata.Reward != nil {
		reward = sql.NullFloat64{Float64: *e.Metadata.Reward, Valid: true}
	}
	_, err = tx.Exec(`
		INSERT INTO events
		(id, layer, context_id, source, ts, created_at, last_accessed_at,
		 promotion_eligible_at, score, reward, repetition_count, vector, file_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			layer=excluded.layer,
			context_id=excluded.context_id,
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
		e.ID, int(e.LayerState), e.Metadata.ContextID, e.Metadata.Source,
		e.Metadata.TS, e.CreatedAt, e.LastAccessedAt, e.PromotionEligibleAt,
		e.CurrentScore(), reward, e.Metadata.RepetitionCount,
		util.EncodeVector(e.Vector), filePath,
	)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tags WHERE event_id = ?`, e.ID); err != nil {
		return err
	}
	for _, tag := range e.Metadata.Tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags(event_id, tag) VALUES (?, ?)`, e.ID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteEvent removes the row (and tag/association rows via cascade).
func (i *Index) DeleteEvent(id string) (bool, error) {
	res, err := i.db.Exec(`DELETE FROM events WHERE id = ?`, id)
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
		&r.ID, &layerInt, &r.ContextID, &r.Source, &r.TS,
		&r.CreatedAt, &r.LastAccessedAt, &r.PromotionEligibleAt,
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

const eventColumns = `id, layer, context_id, source, ts, created_at, last_accessed_at,
		promotion_eligible_at, score, reward, repetition_count, vector, file_path`

// ListByLayer streams all rows belonging to a layer.
func (i *Index) ListByLayer(layer types.LayerState) ([]*EventRow, error) {
	rows, err := i.db.Query(`SELECT `+eventColumns+` FROM events WHERE layer = ?`, int(layer))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return i.collectAndAttachTags(rows)
}

// GetByID returns the row for an event id, or nil if not found.
func (i *Index) GetByID(id string) (*EventRow, error) {
	row := i.db.QueryRow(`SELECT `+eventColumns+` FROM events WHERE id = ?`, id)
	r, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tags, err := i.tagsFor(r.ID)
	if err != nil {
		return nil, err
	}
	r.Tags = tags
	return r, nil
}

// CountByLayer returns the number of events stored in a layer.
func (i *Index) CountByLayer(layer types.LayerState) (int, error) {
	var n int
	err := i.db.QueryRow(`SELECT COUNT(*) FROM events WHERE layer = ?`, int(layer)).Scan(&n)
	return n, err
}

// Search filters rows by layer/context/tags and returns the rows.
// Vector ranking is done by the caller (brute-force cosine in-memory).
func (i *Index) Search(layer *types.LayerState, contextID string, tags []string) ([]*EventRow, error) {
	var (
		conds []string
		args  []any
	)
	q := `SELECT ` + prefixCols("e.") + ` FROM events e`
	// Tags are bound first because the JOIN clause precedes WHERE in the SQL.
	if len(tags) > 0 {
		placeholders := strings.Repeat("?,", len(tags))
		placeholders = strings.TrimRight(placeholders, ",")
		q += ` JOIN tags t ON t.event_id = e.id AND t.tag IN (` + placeholders + `)`
		for _, t := range tags {
			args = append(args, t)
		}
	}
	if layer != nil {
		conds = append(conds, "e.layer = ?")
		args = append(args, int(*layer))
	}
	if contextID != "" {
		conds = append(conds, "e.context_id = ?")
		args = append(args, contextID)
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, " AND ")
	}
	q += ` GROUP BY e.id`
	rows, err := i.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return i.collectAndAttachTags(rows)
}

func prefixCols(prefix string) string {
	parts := strings.Split(eventColumns, ",")
	for idx, p := range parts {
		parts[idx] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func (i *Index) collectAndAttachTags(rows *sql.Rows) ([]*EventRow, error) {
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
		tags, err := i.tagsFor(r.ID)
		if err != nil {
			return nil, err
		}
		r.Tags = tags
	}
	return out, nil
}

func (i *Index) tagsFor(id string) ([]string, error) {
	rows, err := i.db.Query(`SELECT tag FROM tags WHERE event_id = ?`, id)
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
func (i *Index) AddAssociation(a, b string) error {
	if a == b {
		return nil
	}
	if a > b {
		a, b = b, a
	}
	_, err := i.db.Exec(`INSERT OR IGNORE INTO associations(a_id, b_id) VALUES (?, ?)`, a, b)
	return err
}

// NeighborCount returns the number of events directly associated with id.
func (i *Index) NeighborCount(id string) (int, error) {
	var n int
	err := i.db.QueryRow(`
		SELECT COUNT(*) FROM associations WHERE a_id = ? OR b_id = ?
	`, id, id).Scan(&n)
	return n, err
}

// Neighbors returns the IDs of all events associated with id.
func (i *Index) Neighbors(id string) ([]string, error) {
	rows, err := i.db.Query(`
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
func (i *Index) TotalAssociations() (int, error) {
	var n int
	err := i.db.QueryRow(`SELECT COUNT(*) FROM associations`).Scan(&n)
	return n, err
}

// RemoveAssociations removes every edge touching id.
func (i *Index) RemoveAssociations(id string) error {
	_, err := i.db.Exec(`DELETE FROM associations WHERE a_id = ? OR b_id = ?`, id, id)
	return err
}

// --- schemas (long-term consolidation) ---

// SchemaRow mirrors the schemas row.
type SchemaRow struct {
	ID                string
	Summary           string
	Vector            []float64
	Importance        float64
	CreatedAt         int64
	LastUpdatedAt     int64
	FilePath          string
	ConsolidatedFrom  []string
}

// UpsertSchema persists a schema record.
func (i *Index) UpsertSchema(s *SchemaRow) error {
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`
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
	if _, err := tx.Exec(`DELETE FROM schema_sources WHERE schema_id = ?`, s.ID); err != nil {
		return err
	}
	for _, src := range s.ConsolidatedFrom {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_sources(schema_id, event_id) VALUES (?, ?)`, s.ID, src); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CountSchemas returns the number of stored schemas.
func (i *Index) CountSchemas() (int, error) {
	var n int
	err := i.db.QueryRow(`SELECT COUNT(*) FROM schemas`).Scan(&n)
	return n, err
}

// ClearAll wipes every table - used by CascadeMemorySystem.Clear().
func (i *Index) ClearAll() error {
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range []string{"schema_sources", "schemas", "associations", "tags", "events"} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return err
		}
	}
	return tx.Commit()
}
