// Package storage persists Events as Markdown files indexed by SQLite.
//
// Each Event lives in a single markdown file with YAML frontmatter holding
// structured fields and a body containing the original content. SQLite is the
// queryable index plus the vector store.
package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"chronocascade/internal/types"
)

const frontmatterFence = "---"

// frontmatter is the YAML-serialisable view of an Event.
type frontmatter struct {
	ID                  string               `yaml:"id"`
	LayerState          types.LayerState     `yaml:"layerState"`
	CreatedAt           int64                `yaml:"createdAt"`
	LastAccessedAt      int64                `yaml:"lastAccessedAt"`
	PromotionEligibleAt int64                `yaml:"promotionEligibleAt,omitempty"`
	Metadata            types.EventMetadata  `yaml:"metadata"`
	Scores              types.Scores         `yaml:"scores"`
	History             []types.HistoryEntry `yaml:"history"`
	Vector              []float64            `yaml:"vector,flow"`
}

// MarshalEvent renders an Event to a markdown document with YAML frontmatter.
func MarshalEvent(e *types.Event) ([]byte, error) {
	fm := frontmatter{
		ID:                  e.ID,
		LayerState:          e.LayerState,
		CreatedAt:           e.CreatedAt,
		LastAccessedAt:      e.LastAccessedAt,
		PromotionEligibleAt: e.PromotionEligibleAt,
		Metadata:            e.Metadata,
		Scores:              e.Scores,
		History:             e.History,
		Vector:              e.Vector,
	}
	var buf bytes.Buffer
	buf.WriteString(frontmatterFence)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString(frontmatterFence)
	buf.WriteByte('\n')
	buf.WriteByte('\n')
	buf.WriteString(renderBody(e))
	return buf.Bytes(), nil
}

func renderBody(e *types.Event) string {
	if e.ContentRaw != "" {
		return e.ContentRaw
	}
	if e.Content == nil {
		return ""
	}
	if s, ok := e.Content.(string); ok {
		return s
	}
	raw, err := json.MarshalIndent(e.Content, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", e.Content)
	}
	return "```json\n" + string(raw) + "\n```\n"
}

// UnmarshalEvent parses a markdown document produced by MarshalEvent.
func UnmarshalEvent(data []byte) (*types.Event, error) {
	text := string(data)
	if !strings.HasPrefix(text, frontmatterFence) {
		return nil, errors.New("missing frontmatter fence")
	}
	rest := strings.TrimPrefix(text, frontmatterFence)
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n"+frontmatterFence)
	if end < 0 {
		return nil, errors.New("unterminated frontmatter")
	}
	fmYAML := rest[:end]
	body := rest[end+len("\n"+frontmatterFence):]
	body = strings.TrimLeft(body, "\r\n")

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(fmYAML), &fm); err != nil {
		return nil, fmt.Errorf("decode frontmatter: %w", err)
	}
	e := &types.Event{
		ID:                  fm.ID,
		Vector:              fm.Vector,
		Metadata:            fm.Metadata,
		LayerState:          fm.LayerState,
		Scores:              fm.Scores,
		History:             fm.History,
		CreatedAt:           fm.CreatedAt,
		LastAccessedAt:      fm.LastAccessedAt,
		PromotionEligibleAt: fm.PromotionEligibleAt,
		ContentRaw:          body,
		Content:             body,
	}
	return e, nil
}

// LayerDir returns the per-layer subdirectory inside baseDir.
func LayerDir(baseDir string, layer types.LayerState) string {
	return filepath.Join(baseDir, fmt.Sprintf("l%d", int(layer)))
}

// EventPath is the canonical markdown file path for an event id in a layer.
func EventPath(baseDir string, layer types.LayerState, id string) string {
	return filepath.Join(LayerDir(baseDir, layer), id+".md")
}

// WriteEventFile writes the markdown representation atomically.
func WriteEventFile(baseDir string, e *types.Event) error {
	dir := LayerDir(baseDir, e.LayerState)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := MarshalEvent(e)
	if err != nil {
		return err
	}
	path := EventPath(baseDir, e.LayerState, e.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadEventFile loads an event from its markdown file.
func ReadEventFile(baseDir string, layer types.LayerState, id string) (*types.Event, error) {
	data, err := os.ReadFile(EventPath(baseDir, layer, id))
	if err != nil {
		return nil, err
	}
	return UnmarshalEvent(data)
}

// RemoveEventFile deletes the markdown file for an event. Missing files are not errors.
func RemoveEventFile(baseDir string, layer types.LayerState, id string) error {
	err := os.Remove(EventPath(baseDir, layer, id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
