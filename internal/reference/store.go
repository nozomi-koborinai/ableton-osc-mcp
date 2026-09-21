// Package reference keeps named mix profiles of reference tracks: numbers
// only, never audio.
package reference

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

const fileVersion = 1

var namePattern = regexp.MustCompile(`^[a-z0-9_-]{1,40}$`)

// Profile is what is kept about one reference track.
type Profile struct {
	Name        string                  `json:"name"`
	SourceKind  string                  `json:"source_kind"` // "url" or "file"
	Source      string                  `json:"source"`
	SavedAt     time.Time               `json:"saved_at"`
	RangeSec    [2]float64              `json:"range_sec"`
	DurationSec float64                 `json:"duration_sec"`
	BPM         float64                 `json:"bpm"`
	Key         string                  `json:"key"`
	Scale       string                  `json:"scale"`
	Mix         audioanalyze.MixProfile `json:"mix"`
}

type fileContents struct {
	Version  int                `json:"version"`
	Profiles map[string]Profile `json:"profiles"`
}

// Store is a JSON file of profiles keyed by normalized name.
type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("reference profiles path is required")
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string {
	return s.path
}

// NormalizeName trims and lowercases a profile name and checks it against
// ^[a-z0-9_-]{1,40}$.
func NormalizeName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("invalid reference name %q: use 1-40 characters from a-z, 0-9, '-' and '_'", raw)
	}
	return name, nil
}

// Save stores p under its normalized name, replacing any profile already there.
func (s *Store) Save(p Profile) (Profile, error) {
	name, err := NormalizeName(p.Name)
	if err != nil {
		return Profile{}, err
	}
	p.Name = name
	if p.SavedAt.IsZero() {
		p.SavedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	contents, err := s.load()
	if err != nil {
		return Profile{}, err
	}
	contents.Profiles[name] = p
	if err := s.save(contents); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// List returns every profile, ordered by name.
func (s *Store) List() ([]Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	contents, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(contents.Profiles))
	for _, p := range contents.Profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) load() (fileContents, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return fileContents{Version: fileVersion, Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return fileContents{}, fmt.Errorf("read reference profiles: %w", err)
	}
	var contents fileContents
	if err := json.Unmarshal(data, &contents); err != nil {
		return fileContents{}, fmt.Errorf("parse reference profiles: %w", err)
	}
	if contents.Version != fileVersion {
		return fileContents{}, fmt.Errorf("unsupported reference profiles version: %d", contents.Version)
	}
	if contents.Profiles == nil {
		contents.Profiles = map[string]Profile{}
	}
	return contents, nil
}

func (s *Store) save(contents fileContents) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create reference profiles directory: %w", err)
	}
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return fmt.Errorf("encode reference profiles: %w", err)
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(filepath.Dir(s.path), ".reference-profiles-*.json")
	if err != nil {
		return fmt.Errorf("create reference profiles temp file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write reference profiles: %w", err)
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set reference profiles permissions: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close reference profiles: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace reference profiles: %w", err)
	}
	return nil
}
