package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"chronocascade/internal/config"
	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// LongTermStore is Layer 2: sparse, schema-aware, slow decay.
type LongTermStore struct {
	baseDir    string
	idx        *Index
	capacity   int
	tauSeconds float64
	decayRate  float64
	clock      util.Clock
}

// NewLongTermStore constructs a Layer-2 store.
func NewLongTermStore(cfg config.Config, idx *Index, clock util.Clock) *LongTermStore {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &LongTermStore{
		baseDir:    cfg.Storage.BaseDir,
		idx:        idx,
		capacity:   cfg.Capacity.LongTerm,
		tauSeconds: cfg.Tau[2].Seconds(),
		decayRate:  cfg.DecayRates[2],
		clock:      clock,
	}
}

func (l *LongTermStore) Layer() types.LayerState { return types.LongTerm }

func (l *LongTermStore) Add(ctx context.Context, e *types.Event) error {
	size, err := l.idx.CountByLayer(ctx, types.LongTerm)
	if err != nil {
		return err
	}
	existing, err := l.idx.GetByID(ctx, e.ID)
	if err != nil {
		return err
	}
	if existing == nil && size >= l.capacity {
		return fmt.Errorf("%w: long-term", ErrCapacityExceeded)
	}
	e.LayerState = types.LongTerm
	e.LastAccessedAt = l.clock.NowMillis()
	if err := WriteEventFile(l.baseDir, e); err != nil {
		return err
	}
	return l.idx.UpsertEvent(ctx, e, EventPath(l.baseDir, types.LongTerm, e.ID))
}

func (l *LongTermStore) Get(ctx context.Context, id string) (*types.Event, error) {
	r, err := l.idx.GetByID(ctx, id)
	if err != nil || r == nil || r.Layer != types.LongTerm {
		return nil, err
	}
	return hydrateEvent(l.baseDir, r)
}

func (l *LongTermStore) GetAll(ctx context.Context) ([]*types.Event, error) {
	rows, err := l.idx.ListByLayer(ctx, types.LongTerm)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Event, 0, len(rows))
	for _, r := range rows {
		e, err := hydrateEvent(l.baseDir, r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (l *LongTermStore) Remove(ctx context.Context, id string) (bool, error) {
	r, err := l.idx.GetByID(ctx, id)
	if err != nil || r == nil || r.Layer != types.LongTerm {
		return false, err
	}
	if err := RemoveEventFile(l.baseDir, types.LongTerm, id); err != nil {
		return false, err
	}
	return l.idx.DeleteEvent(ctx, id)
}

func (l *LongTermStore) Size(ctx context.Context) (int, error) {
	return l.idx.CountByLayer(ctx, types.LongTerm)
}

func (l *LongTermStore) Clear(ctx context.Context) error {
	events, err := l.GetAll(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		_ = RemoveEventFile(l.baseDir, types.LongTerm, e.ID)
		if _, err := l.idx.DeleteEvent(ctx, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (l *LongTermStore) Search(ctx context.Context, q types.RetrievalQuery) ([]types.RetrievalResult, error) {
	layer := types.LongTerm
	rows, err := l.idx.Search(ctx, SearchFilters{
		Layer:     &layer,
		ContextID: q.ContextID,
		UserID:    q.UserID,
		SessionID: q.SessionID,
		Tags:      q.Tags,
	})
	if err != nil {
		return nil, err
	}
	events := make([]*types.Event, 0, len(rows))
	for _, r := range rows {
		e, err := hydrateEvent(l.baseDir, r)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	events = applyMinScoreFilter(events, q)
	return rankAndTopK(events, q), nil
}

func (l *LongTermStore) ApplyDecay(ctx context.Context, nowMillis int64) error {
	events, err := l.GetAll(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		age := float64(nowMillis-e.LastAccessedAt) / 1000.0
		factor := math.Exp(-l.decayRate * age)
		current := e.Scores.RawSalience
		if e.Scores.Layer2Score != nil {
			current = *e.Scores.Layer2Score
		} else if e.Scores.Layer1Score != nil {
			current = *e.Scores.Layer1Score
		}
		next := current * factor
		e.Scores.Layer2Score = &next
		e.History = append(e.History, types.HistoryEntry{
			Action: types.ActionDecayed,
			TS:     nowMillis,
			Score:  &next,
		})
		if err := WriteEventFile(l.baseDir, e); err != nil {
			return err
		}
		if err := l.idx.UpsertEvent(ctx, e, EventPath(l.baseDir, types.LongTerm, e.ID)); err != nil {
			return err
		}
	}
	return nil
}

func (l *LongTermStore) GetExpiredEvents(ctx context.Context) ([]*types.Event, error) {
	events, err := l.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	now := l.clock.NowMillis()
	var out []*types.Event
	for _, e := range events {
		if float64(now-e.CreatedAt)/1000.0 > l.tauSeconds {
			out = append(out, e)
		}
	}
	return out, nil
}

func (l *LongTermStore) GetStats(ctx context.Context) (types.LayerStats, error) {
	events, err := l.GetAll(ctx)
	if err != nil {
		return types.LayerStats{}, err
	}
	return computeLayerStats(types.LongTerm, l.capacity, events), nil
}

// --- schema consolidation ---

// SchemaEntry is the public type returned by consolidation.
type SchemaEntry struct {
	ID               string
	Summary          string
	ConsolidatedFrom []string
	Vector           []float64
	Importance       float64
	CreatedAt        int64
	LastUpdatedAt    int64
}

// AutoConsolidate clusters similar events and merges each cluster into a Schema.
func (l *LongTermStore) AutoConsolidate(ctx context.Context, minGroupSize int, similarityThreshold float64) ([]*SchemaEntry, error) {
	events, err := l.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(events) < minGroupSize {
		return nil, nil
	}
	groups := clusterSimilar(events, similarityThreshold)
	var out []*SchemaEntry
	for _, g := range groups {
		if len(g) < minGroupSize {
			continue
		}
		schema, err := l.consolidate(ctx, g)
		if err != nil {
			return nil, err
		}
		out = append(out, schema)
	}
	return out, nil
}

func clusterSimilar(events []*types.Event, threshold float64) [][]*types.Event {
	assigned := make(map[string]bool)
	var groups [][]*types.Event
	for _, seed := range events {
		if assigned[seed.ID] {
			continue
		}
		group := []*types.Event{seed}
		assigned[seed.ID] = true
		for _, other := range events {
			if assigned[other.ID] {
				continue
			}
			if util.DotProduct(seed.Vector, other.Vector) >= threshold {
				group = append(group, other)
				assigned[other.ID] = true
			}
		}
		groups = append(groups, group)
	}
	return groups
}

func (l *LongTermStore) consolidate(ctx context.Context, events []*types.Event) (*SchemaEntry, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("cannot consolidate empty list")
	}
	dim := len(events[0].Vector)
	avg := make([]float64, dim)
	for _, e := range events {
		for i := range dim {
			avg[i] += e.Vector[i]
		}
	}
	for i := range avg {
		avg[i] /= float64(len(events))
	}
	avg = util.Normalize(avg)

	importance := schemaImportance(events)
	now := l.clock.NowMillis()
	schema := &SchemaEntry{
		ID:               newSchemaID(now),
		Summary:          summarise(events),
		ConsolidatedFrom: collectIDs(events),
		Vector:           avg,
		Importance:       importance,
		CreatedAt:        now,
		LastUpdatedAt:    now,
	}
	if err := l.writeSchemaFile(schema); err != nil {
		return nil, err
	}
	row := &SchemaRow{
		ID:               schema.ID,
		Summary:          schema.Summary,
		Vector:           schema.Vector,
		Importance:       schema.Importance,
		CreatedAt:        schema.CreatedAt,
		LastUpdatedAt:    schema.LastUpdatedAt,
		FilePath:         l.schemaPath(schema.ID),
		ConsolidatedFrom: schema.ConsolidatedFrom,
	}
	if err := l.idx.UpsertSchema(ctx, row); err != nil {
		return nil, err
	}
	for _, e := range events {
		if _, err := l.Remove(ctx, e.ID); err != nil {
			return nil, err
		}
	}
	return schema, nil
}

func (l *LongTermStore) schemaDir() string { return filepath.Join(l.baseDir, "schemas") }
func (l *LongTermStore) schemaPath(id string) string {
	return filepath.Join(l.schemaDir(), id+".md")
}

func (l *LongTermStore) writeSchemaFile(s *SchemaEntry) error {
	if err := os.MkdirAll(l.schemaDir(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", s.ID)
	fmt.Fprintf(&b, "importance: %g\n", s.Importance)
	fmt.Fprintf(&b, "createdAt: %d\n", s.CreatedAt)
	fmt.Fprintf(&b, "lastUpdatedAt: %d\n", s.LastUpdatedAt)
	b.WriteString("consolidatedFrom:\n")
	for _, src := range s.ConsolidatedFrom {
		fmt.Fprintf(&b, "  - %s\n", src)
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# Schema: %s\n\n%s\n", s.ID, s.Summary)
	return os.WriteFile(l.schemaPath(s.ID), []byte(b.String()), 0o644)
}

func summarise(events []*types.Event) string {
	sources := uniqueOf(events, func(e *types.Event) string { return e.Metadata.Source })
	contexts := uniqueOf(events, func(e *types.Event) string { return e.Metadata.ContextID })
	tagCount := map[string]int{}
	for _, e := range events {
		for _, t := range e.Metadata.Tags {
			tagCount[t]++
		}
	}
	half := len(events) / 2
	var common []string
	for tag, n := range tagCount {
		if n >= half {
			common = append(common, tag)
		}
	}
	sort.Strings(common)
	return fmt.Sprintf("Consolidated %d events from %d source(s), %d context(s), common tags: [%s]",
		len(events), len(sources), len(contexts), strings.Join(common, ", "))
}

func uniqueOf(events []*types.Event, get func(*types.Event) string) []string {
	set := map[string]struct{}{}
	for _, e := range events {
		set[get(e)] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func schemaImportance(events []*types.Event) float64 {
	if len(events) == 0 {
		return 0
	}
	maxS := events[0].CurrentScore()
	sum := 0.0
	for _, e := range events {
		s := e.CurrentScore()
		sum += s
		if s > maxS {
			maxS = s
		}
	}
	avg := sum / float64(len(events))
	return 0.6*maxS + 0.4*avg
}

func collectIDs(events []*types.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

func newSchemaID(nowMillis int64) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("schema_%d_%s", nowMillis, hex.EncodeToString(b))
}

// SchemaCount exposes the persisted schema count for system stats.
func (l *LongTermStore) SchemaCount(ctx context.Context) (int, error) {
	return l.idx.CountSchemas(ctx)
}
