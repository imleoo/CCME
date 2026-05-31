package core_test

import (
	"path/filepath"
	"testing"

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
	sys, err := core.New(cfg)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close() })
	return sys
}

func TestIngestSingleEvent(t *testing.T) {
	sys := newSystem(t)
	e, err := sys.Ingest(core.RawEvent{
		Content:   map[string]any{"message": "Hello"},
		Source:    "test",
		ContextID: "ctx1",
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
	if len(e.Vector) != 384 {
		t.Errorf("expected vector len 384, got %d", len(e.Vector))
	}
	if e.Scores.RawSalience <= 0 {
		t.Errorf("expected positive salience, got %f", e.Scores.RawSalience)
	}
}

func TestIngestBatch(t *testing.T) {
	sys := newSystem(t)
	events, err := sys.IngestBatch([]core.RawEvent{
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

func TestRetrieveByContext(t *testing.T) {
	sys := newSystem(t)
	_, err := sys.Ingest(core.RawEvent{Content: "Test 1", Source: "test", ContextID: "ctx1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sys.Ingest(core.RawEvent{Content: "Test 2", Source: "test", ContextID: "ctx2"})
	if err != nil {
		t.Fatal(err)
	}
	results, err := sys.Retrieve(types.RetrievalQuery{ContextID: "ctx1", TopK: 10})
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

func TestRetrieveByTags(t *testing.T) {
	sys := newSystem(t)
	_, err := sys.Ingest(core.RawEvent{
		Content:   "tagged",
		Source:    "test",
		ContextID: "ctx1",
		Tags:      []string{"important"},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := sys.Retrieve(types.RetrievalQuery{Tags: []string{"important"}, TopK: 10})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected tag hits")
	}
}

func TestSystemStats(t *testing.T) {
	sys := newSystem(t)
	_, err := sys.Ingest(core.RawEvent{Content: "Test", Source: "test", ContextID: "ctx1"})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := sys.GetStats()
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
	reward := 0.9
	for i := 0; i < 5; i++ {
		_, err := sys.Ingest(core.RawEvent{
			Content:   "High-value event",
			Source:    "test",
			ContextID: "ctx1",
			Reward:    &reward,
			Tags:      []string{"important"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := sys.RunMaintenanceCycle()
	if err != nil {
		t.Fatalf("maintenance: %v", err)
	}
}

func TestDeleteEvent(t *testing.T) {
	sys := newSystem(t)
	e, err := sys.Ingest(core.RawEvent{Content: "to delete", Source: "test", ContextID: "ctx1"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := sys.DeleteEvent(e.ID)
	if err != nil || !ok {
		t.Fatalf("delete: ok=%v err=%v", ok, err)
	}
	got, err := sys.GetEvent(e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}
