// ccme is a small demo runner for the ChronoCascade Memory Engine.
//
// It exercises the public surface of the library: ingestion (single + batch),
// repetition reinforcement, maintenance cycle (decay/promotion/replay/forget),
// retrieval by tag/context, and per-event audit logs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"chronocascade/internal/config"
	"chronocascade/internal/core"
	"chronocascade/internal/types"
)

func main() {
	storeDir := flag.String("dir", "./memory", "base directory for markdown + sqlite storage")
	reset := flag.Bool("reset", false, "wipe the store before running the demo")
	flag.Parse()

	cfg := config.Default()
	cfg.Storage.BaseDir = *storeDir
	cfg.Storage.SQLitePath = filepath.Join(*storeDir, "index.db")

	if *reset {
		if err := os.RemoveAll(*storeDir); err != nil {
			log.Fatalf("reset store: %v", err)
		}
	}

	sys, err := core.New(cfg)
	if err != nil {
		log.Fatalf("init system: %v", err)
	}
	defer func() { _ = sys.Close() }()

	runDemo(sys)
}

func runDemo(sys *core.CascadeMemorySystem) {
	fmt.Println("=== ChronoCascade Memory Engine (Go) ===")

	// 1. Single ingest
	e, err := sys.Ingest(core.RawEvent{
		Content:   map[string]any{"message": "User prefers dark mode"},
		Source:    "user_interaction",
		ContextID: "user_123",
		Tags:      []string{"preference", "ui"},
		Reward:    ptr(0.8),
	})
	must(err)
	fmt.Printf("[ingest] id=%s layer=%s salience=%.3f\n",
		e.ID, e.LayerState, e.Scores.RawSalience)

	// 2. Repeated event (should be reinforced via repetition detection)
	for i := 0; i < 5; i++ {
		_, err := sys.Ingest(core.RawEvent{
			Content:   map[string]any{"user": "alice", "preference": "vegetarian"},
			Source:    "dialogue",
			ContextID: "alice_preferences",
			Tags:      []string{"food", "preference"},
			Reward:    ptr(0.9),
		})
		must(err)
	}

	// 3. Batch ingest for retrieval coverage
	_, err = sys.IngestBatch([]core.RawEvent{
		{Content: "Machine learning basics", Source: "learning", ContextID: "study", Tags: []string{"AI", "ML"}},
		{Content: "Deep learning intro", Source: "learning", ContextID: "study", Tags: []string{"AI", "DL"}},
		{Content: "How to make pasta", Source: "learning", ContextID: "hobby", Tags: []string{"cooking"}},
	})
	must(err)

	// 4. Maintenance cycle
	replay, forget, err := sys.RunMaintenanceCycle()
	must(err)
	fmt.Printf("[maintenance] L0->L1=%d L1->L2=%d replays=%d pruned=%d\n",
		replay.Layer0Promotions, replay.Layer1Promotions, replay.TotalReplays, forget.TotalPruned)

	// 5. Retrieval
	aiResults, err := sys.Retrieve(types.RetrievalQuery{Tags: []string{"AI"}, TopK: 5})
	must(err)
	fmt.Printf("[retrieve tag=AI] hits=%d\n", len(aiResults))

	studyResults, err := sys.Retrieve(types.RetrievalQuery{ContextID: "study", TopK: 5})
	must(err)
	fmt.Printf("[retrieve ctx=study] hits=%d\n", len(studyResults))

	// 6. Stats
	stats, err := sys.GetStats()
	must(err)
	dumpStats(stats)

	// 7. Event history
	history := sys.GetEventHistory(e.ID)
	fmt.Printf("[history e=%s] entries=%d\n", e.ID, len(history))
}

func dumpStats(s core.SystemStats) {
	out, _ := json.MarshalIndent(map[string]any{
		"layer0": map[string]any{
			"size": s.Layer0.Size, "capacity": s.Layer0.Capacity,
			"utilization": s.Layer0.UtilizationRate,
		},
		"layer1": map[string]any{
			"size": s.Layer1.Size, "capacity": s.Layer1.Capacity,
			"associations": s.Layer1Edges,
		},
		"layer2": map[string]any{
			"size": s.Layer2.Size, "capacity": s.Layer2.Capacity,
			"schemas": s.Layer2Schemas,
		},
		"totalEvents": s.TotalEvents,
	}, "", "  ")
	fmt.Println("[stats]")
	fmt.Println(string(out))
}

func ptr[T any](v T) *T { return &v }

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
