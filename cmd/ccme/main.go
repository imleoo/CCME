// ccme is a small demo runner for the ChronoCascade Memory Engine.
//
// It exercises the public surface of the library: ingestion (single + batch +
// async), repetition reinforcement, maintenance cycle (decay/promotion/replay/
// forget), retrieval by tag/context, per-event audit logs, plus the
// session-side API (UserProfile, SessionContext, SessionSummary, ChatLog).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sys, err := core.New(ctx, cfg)
	if err != nil {
		log.Fatalf("init system: %v", err)
	}
	defer func() { _ = sys.Close() }()

	runDemo(ctx, sys)
}

func runDemo(ctx context.Context, sys *core.CascadeMemorySystem) {
	fmt.Println("=== ChronoCascade Memory Engine (Go) ===")

	userID, sessionID := "user_alice", "session_001"

	// 1. Single ingest (now with user/session attribution)
	e, err := sys.Ingest(ctx, types.RawEvent{
		Content:   map[string]any{"message": "User prefers dark mode"},
		Source:    "user_interaction",
		ContextID: "user_123",
		UserID:    userID,
		SessionID: sessionID,
		AgentName: "ui_tracker",
		Tags:      []string{"preference", "ui"},
		Reward:    ptr(0.8),
	})
	must(err)
	fmt.Printf("[ingest] id=%s layer=%s salience=%.3f user=%s session=%s\n",
		e.ID, e.LayerState, e.Scores.RawSalience, e.Metadata.UserID, e.Metadata.SessionID)

	// 2. Async ingest
	resultCh := sys.IngestAsync(ctx, types.RawEvent{
		Content:   "Async background-written event",
		Source:    "background",
		ContextID: "user_123",
		UserID:    userID,
		SessionID: sessionID,
		Tags:      []string{"async"},
	})
	if r := <-resultCh; r.Err != nil {
		log.Fatalf("async ingest: %v", r.Err)
	} else {
		fmt.Printf("[ingest-async] id=%s\n", r.Event.ID)
	}

	// 3. Repeated event reinforcement
	for range 5 {
		_, err := sys.Ingest(ctx, types.RawEvent{
			Content:   map[string]any{"user": "alice", "preference": "vegetarian"},
			Source:    "dialogue",
			ContextID: "alice_preferences",
			UserID:    userID,
			SessionID: sessionID,
			Tags:      []string{"food", "preference"},
			Reward:    ptr(0.9),
		})
		must(err)
	}

	// 4. Batch ingest for retrieval coverage
	_, err = sys.IngestBatch(ctx, []types.RawEvent{
		{Content: "Machine learning basics", Source: "learning", ContextID: "study",
			UserID: userID, SessionID: sessionID, Tags: []string{"AI", "ML"}},
		{Content: "Deep learning intro", Source: "learning", ContextID: "study",
			UserID: userID, SessionID: sessionID, Tags: []string{"AI", "DL"}},
		{Content: "How to make pasta", Source: "learning", ContextID: "hobby",
			UserID: userID, SessionID: sessionID, Tags: []string{"cooking"}},
	})
	must(err)

	// 5. SessionContext + ChatMessage audit
	must(sys.WriteSessionContext(ctx, &types.SessionContext{
		UserID:    userID,
		SessionID: sessionID,
		LastAgent: "ui_tracker",
		TurnCount: 6,
		History: []types.Message{
			{Role: "user", Content: "Switch to dark mode"},
			{Role: "assistant", Content: "Done — dark mode enabled."},
		},
	}))
	must(sys.WriteChatMessage(ctx, &types.ChatMessage{
		ID: uuid.NewString(), UserID: userID, SessionID: sessionID,
		TurnIndex: 1, Role: "user", Content: "Switch to dark mode",
	}))
	must(sys.WriteChatMessage(ctx, &types.ChatMessage{
		ID: uuid.NewString(), UserID: userID, SessionID: sessionID,
		TurnIndex: 1, Role: "assistant", AgentName: "ui_tracker",
		Content: "Done — dark mode enabled.",
	}))

	// 6. UserProfile structured layer
	must(sys.WriteUserProfile(ctx, &types.UserProfile{
		UserID:             userID,
		DisplayName:        "Alice",
		CommunicationStyle: "concise",
		Tags:               []string{"vegetarian", "ui_dark"},
		Preferences:        &types.ProfilePreferences{Tone: "warm"},
		Patterns: []types.ProfilePattern{
			{ID: "p1", Name: "vegetarian preference", Confidence: 0.9, LastSeen: time.Now()},
		},
		ActivePlan: &types.ProfileActivePlan{
			Goal: "Maintain consistent UI preferences", Status: "in_progress",
		},
	}))

	// 7. Rolling session summary
	must(sys.UpsertSessionSummary(ctx, &types.SessionSummary{
		UserID: userID, SessionID: sessionID,
		TurnRangeStart: 1, TurnRangeEnd: 6,
		Summary: "User reaffirmed dark mode + vegetarian preferences; on a learning streak (AI/ML).",
	}))

	// 8. Maintenance cycle
	replay, forget, err := sys.RunMaintenanceCycle(ctx)
	must(err)
	fmt.Printf("[maintenance] L0->L1=%d L1->L2=%d replays=%d pruned=%d\n",
		replay.Layer0Promotions, replay.Layer1Promotions, replay.TotalReplays, forget.TotalPruned)

	// 9. Retrieval (user-scoped)
	aiHits, err := sys.Retrieve(ctx, types.RetrievalQuery{
		UserID: userID, Tags: []string{"AI"}, TopK: 5,
	})
	must(err)
	fmt.Printf("[retrieve user=%s tag=AI] hits=%d\n", userID, len(aiHits))

	studyHits, err := sys.Retrieve(ctx, types.RetrievalQuery{
		UserID: userID, ContextID: "study", TopK: 5,
	})
	must(err)
	fmt.Printf("[retrieve user=%s ctx=study] hits=%d\n", userID, len(studyHits))

	// 10. Profile + summary + chat readback
	profile, err := sys.ReadUserProfile(ctx, userID)
	must(err)
	fmt.Printf("[profile] %s style=%s patterns=%d\n",
		profile.DisplayName, profile.CommunicationStyle, len(profile.Patterns))

	summaries, err := sys.ListSessionSummaries(ctx, userID, sessionID)
	must(err)
	fmt.Printf("[summaries] count=%d first=%q\n", len(summaries), summaries[0].Summary)

	msgs, err := sys.ReadRecentChatMessages(ctx, userID, sessionID, 10)
	must(err)
	fmt.Printf("[chat] recent=%d\n", len(msgs))

	// 11. Stats
	stats, err := sys.GetStats(ctx)
	must(err)
	dumpStats(stats)
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
