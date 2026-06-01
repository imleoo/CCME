package core_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"chronocascade/internal/config"
	"chronocascade/internal/core"
	"chronocascade/internal/types"
)

func newSystem(t *testing.T) *core.CascadeMemorySystem {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.BaseDir = dir
	cfg.Storage.SQLitePath = filepath.Join(dir, "index.db")
	cfg.Capacity.ShortTerm = 100
	cfg.Capacity.MidTerm = 50
	cfg.Capacity.LongTerm = 25
	sys, err := core.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close() })
	return sys
}

func TestIngestSingleEvent(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	e, err := sys.Ingest(ctx, types.RawEvent{
		Content:   map[string]any{"message": "Hello"},
		Source:    "test",
		ContextID: "ctx1",
		UserID:    "u1",
		SessionID: "s1",
		Tags:      []string{"greeting"},
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if e.ID == "" {
		t.Error("expected non-empty id")
	}
	if e.LayerState != types.ShortTerm {
		t.Errorf("expected short-term, got %v", e.LayerState)
	}
	if e.Metadata.UserID != "u1" || e.Metadata.SessionID != "s1" {
		t.Errorf("expected user/session attribution, got user=%q session=%q",
			e.Metadata.UserID, e.Metadata.SessionID)
	}
	if len(e.Vector) != 384 {
		t.Errorf("expected vector len 384, got %d", len(e.Vector))
	}
}

func TestIngestBatch(t *testing.T) {
	sys := newSystem(t)
	events, err := sys.IngestBatch(context.Background(), []types.RawEvent{
		{Content: "Event 1", Source: "test", ContextID: "ctx1"},
		{Content: "Event 2", Source: "test", ContextID: "ctx1"},
	})
	if err != nil {
		t.Fatalf("ingest batch: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID == events[1].ID {
		t.Errorf("expected unique ids")
	}
}

func TestIngestAsync(t *testing.T) {
	sys := newSystem(t)
	ch := sys.IngestAsync(context.Background(), types.RawEvent{
		Content: "async event", Source: "test", ContextID: "ctx1", UserID: "u1",
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			t.Fatalf("async err: %v", r.Err)
		}
		if r.Event == nil || r.Event.ID == "" {
			t.Fatalf("async: expected event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("async ingest timeout")
	}
}

func TestRetrieveByContext(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	if _, err := sys.Ingest(ctx, types.RawEvent{Content: "Test 1", Source: "test", ContextID: "ctx1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.Ingest(ctx, types.RawEvent{Content: "Test 2", Source: "test", ContextID: "ctx2"}); err != nil {
		t.Fatal(err)
	}
	results, err := sys.Retrieve(ctx, types.RetrievalQuery{ContextID: "ctx1", TopK: 10})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	for _, r := range results {
		if r.Event.Metadata.ContextID != "ctx1" {
			t.Errorf("expected ctx1, got %s", r.Event.Metadata.ContextID)
		}
	}
}

func TestRetrieveByUserIsolation(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	if _, err := sys.Ingest(ctx, types.RawEvent{Content: "alice secret", Source: "t",
		ContextID: "ctx", UserID: "alice", Tags: []string{"shared"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.Ingest(ctx, types.RawEvent{Content: "bob secret", Source: "t",
		ContextID: "ctx", UserID: "bob", Tags: []string{"shared"}}); err != nil {
		t.Fatal(err)
	}
	aliceHits, err := sys.Retrieve(ctx, types.RetrievalQuery{
		UserID: "alice", Tags: []string{"shared"}, TopK: 10,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(aliceHits) != 1 {
		t.Fatalf("expected exactly 1 alice hit, got %d", len(aliceHits))
	}
	if aliceHits[0].Event.Metadata.UserID != "alice" {
		t.Errorf("expected alice's event, got %q", aliceHits[0].Event.Metadata.UserID)
	}
}

func TestRetrieveByTags(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	if _, err := sys.Ingest(ctx, types.RawEvent{Content: "tagged", Source: "test",
		ContextID: "ctx1", Tags: []string{"important"}}); err != nil {
		t.Fatal(err)
	}
	results, err := sys.Retrieve(ctx, types.RetrievalQuery{Tags: []string{"important"}, TopK: 10})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected tag hits")
	}
}

func TestSystemStats(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	if _, err := sys.Ingest(ctx, types.RawEvent{Content: "Test", Source: "test", ContextID: "ctx1"}); err != nil {
		t.Fatal(err)
	}
	stats, err := sys.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Layer0.Size != 1 {
		t.Errorf("layer0 size expected 1, got %d", stats.Layer0.Size)
	}
	if stats.TotalEvents != 1 {
		t.Errorf("totalEvents expected 1, got %d", stats.TotalEvents)
	}
}

func TestMaintenanceCycle(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	reward := 0.9
	for range 5 {
		_, err := sys.Ingest(ctx, types.RawEvent{
			Content: "High-value event", Source: "test", ContextID: "ctx1",
			Reward: &reward, Tags: []string{"important"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := sys.RunMaintenanceCycle(ctx); err != nil {
		t.Fatalf("maintenance: %v", err)
	}
}

func TestDeleteEvent(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	e, err := sys.Ingest(ctx, types.RawEvent{Content: "to delete", Source: "test", ContextID: "ctx1"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := sys.DeleteEvent(ctx, e.ID)
	if err != nil || !ok {
		t.Fatalf("delete: ok=%v err=%v", ok, err)
	}
	got, err := sys.GetEvent(ctx, e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestUserProfileRoundTrip(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	want := &types.UserProfile{
		UserID:             "u1",
		DisplayName:        "Alice",
		CommunicationStyle: "concise",
		Tags:               []string{"vegetarian"},
		Patterns: []types.ProfilePattern{
			{ID: "p1", Name: "vegan-curious", Confidence: 0.8, LastSeen: time.Now()},
		},
	}
	if err := sys.WriteUserProfile(ctx, want); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	got, err := sys.ReadUserProfile(ctx, "u1")
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if got == nil || got.DisplayName != want.DisplayName {
		t.Fatalf("expected profile round-trip, got %+v", got)
	}
	if len(got.Patterns) != 1 || got.Patterns[0].Name != "vegan-curious" {
		t.Errorf("expected pattern preserved, got %+v", got.Patterns)
	}
}

func TestSessionContextRoundTrip(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	want := &types.SessionContext{
		UserID:    "u1",
		SessionID: "s1",
		LastAgent: "agent_a",
		TurnCount: 3,
		History: []types.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	}
	if err := sys.WriteSessionContext(ctx, want); err != nil {
		t.Fatalf("write session: %v", err)
	}
	got, err := sys.ReadSessionContext(ctx, "u1", "s1")
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if got == nil || got.TurnCount != 3 || len(got.History) != 2 {
		t.Fatalf("expected session round-trip, got %+v", got)
	}
}

func TestSessionSummaryRolling(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	first := &types.SessionSummary{
		UserID: "u1", SessionID: "s1", TurnRangeStart: 1, TurnRangeEnd: 5,
		Summary: "first window",
	}
	if err := sys.UpsertSessionSummary(ctx, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	second := &types.SessionSummary{
		UserID: "u1", SessionID: "s1", TurnRangeStart: 1, TurnRangeEnd: 10,
		Summary: "wider window",
	}
	if err := sys.UpsertSessionSummary(ctx, second); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	got, err := sys.ListSessionSummaries(ctx, "u1", "s1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one rolling summary, got %d", len(got))
	}
	if got[0].Summary != "wider window" {
		t.Errorf("expected latest summary, got %q", got[0].Summary)
	}
}

func TestChatLogAppend(t *testing.T) {
	sys := newSystem(t)
	ctx := context.Background()
	for i := range 3 {
		if err := sys.WriteChatMessage(ctx, &types.ChatMessage{
			ID:        uuid.NewString(),
			UserID:    "u1",
			SessionID: "s1",
			TurnIndex: i + 1,
			Role:      "user",
			Content:   "msg",
		}); err != nil {
			t.Fatalf("write msg: %v", err)
		}
	}
	msgs, err := sys.ReadRecentChatMessages(ctx, "u1", "s1", 10)
	if err != nil {
		t.Fatalf("read chat: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Recent → DESC: first row should have highest turn_index.
	if msgs[0].TurnIndex != 3 {
		t.Errorf("expected DESC ordering, got %+v", msgs)
	}
}
