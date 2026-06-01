package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"chronocascade/internal/types"
	"chronocascade/internal/util"
)

// ProfileStore persists UserProfile objects as Markdown + SQLite envelopes.
//
// Layout:
//   profiles/<user_id>.md   -- canonical human-readable file
//   user_profiles row       -- JSON payload + timestamps for fast lookup
type ProfileStore struct {
	baseDir string
	idx     *Index
	clock   util.Clock
}

// NewProfileStore constructs a profile store.
func NewProfileStore(baseDir string, idx *Index, clock util.Clock) *ProfileStore {
	if clock == nil {
		clock = util.SystemClock{}
	}
	return &ProfileStore{baseDir: baseDir, idx: idx, clock: clock}
}

func (p *ProfileStore) dir() string { return filepath.Join(p.baseDir, "profiles") }
func (p *ProfileStore) path(userID string) string {
	return filepath.Join(p.dir(), userID+".md")
}

// Write persists the profile to both markdown and SQLite. Empty UserID errors.
func (p *ProfileStore) Write(ctx context.Context, profile *types.UserProfile) error {
	if profile == nil {
		return errors.New("ProfileStore.Write: profile is nil")
	}
	if profile.UserID == "" {
		return errors.New("ProfileStore.Write: userID required")
	}
	now := time.UnixMilli(p.clock.NowMillis()).UTC()
	if profile.CreatedAt.IsZero() {
		existing, err := p.idx.GetUserProfileRow(ctx, profile.UserID)
		if err != nil {
			return err
		}
		if existing != nil {
			profile.CreatedAt = time.UnixMilli(existing.CreatedAt).UTC()
		} else {
			profile.CreatedAt = now
		}
	}
	profile.UpdatedAt = now

	payload, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	if err := p.writeFile(profile); err != nil {
		return err
	}
	return p.idx.UpsertUserProfileRow(ctx, profile.UserID, string(payload),
		p.path(profile.UserID), profile.CreatedAt.UnixMilli(), profile.UpdatedAt.UnixMilli())
}

// Read returns the profile (or nil if it does not exist).
func (p *ProfileStore) Read(ctx context.Context, userID string) (*types.UserProfile, error) {
	if userID == "" {
		return nil, errors.New("ProfileStore.Read: userID required")
	}
	row, err := p.idx.GetUserProfileRow(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	var profile types.UserProfile
	if err := json.Unmarshal([]byte(row.Payload), &profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &profile, nil
}

func (p *ProfileStore) writeFile(profile *types.UserProfile) error {
	if err := os.MkdirAll(p.dir(), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(struct {
		UserID             string                    `yaml:"userId"`
		DisplayName        string                    `yaml:"displayName,omitempty"`
		Tags               []string                  `yaml:"tags,omitempty"`
		CommunicationStyle string                    `yaml:"communicationStyle,omitempty"`
		Patterns           []types.ProfilePattern    `yaml:"patterns,omitempty"`
		Preferences        *types.ProfilePreferences `yaml:"preferences,omitempty"`
		ActivePlan         *types.ProfileActivePlan  `yaml:"activePlan,omitempty"`
		Facts              map[string]any            `yaml:"facts,omitempty"`
		CreatedAt          time.Time                 `yaml:"createdAt"`
		UpdatedAt          time.Time                 `yaml:"updatedAt"`
	}{
		UserID:             profile.UserID,
		DisplayName:        profile.DisplayName,
		Tags:               profile.Tags,
		CommunicationStyle: profile.CommunicationStyle,
		Patterns:           profile.Patterns,
		Preferences:        profile.Preferences,
		ActivePlan:         profile.ActivePlan,
		Facts:              profile.Facts,
		CreatedAt:          profile.CreatedAt,
		UpdatedAt:          profile.UpdatedAt,
	}); err != nil {
		return err
	}
	_ = enc.Close()
	buf.WriteString("---\n\n")
	fmt.Fprintf(&buf, "# Profile: %s\n\n", profile.UserID)
	if profile.DisplayName != "" {
		fmt.Fprintf(&buf, "- Display name: %s\n", profile.DisplayName)
	}
	if profile.CommunicationStyle != "" {
		fmt.Fprintf(&buf, "- Communication style: %s\n", profile.CommunicationStyle)
	}
	if len(profile.Tags) > 0 {
		fmt.Fprintf(&buf, "- Tags: %s\n", strings.Join(profile.Tags, ", "))
	}
	if profile.ActivePlan != nil && profile.ActivePlan.Goal != "" {
		fmt.Fprintf(&buf, "- Active plan: %s (status: %s)\n",
			profile.ActivePlan.Goal, profile.ActivePlan.Status)
	}
	tmp := p.path(profile.UserID) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path(profile.UserID))
}
