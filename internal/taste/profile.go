package taste

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const profileVersion = 1

type Preference struct {
	Instrument string    `json:"instrument"`
	Variation  string    `json:"variation"`
	Preferred  string    `json:"preferred"`
	Note       string    `json:"note,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// AuditionOption is one variant the listener heard in an audition.
type AuditionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AuditionChoice is what the listener picked out of an N-way audition. It
// keeps every option they heard: a choice only means something next to what
// it was chosen over.
type AuditionChoice struct {
	Topic      string           `json:"topic"`
	Options    []AuditionOption `json:"options"`
	Chosen     string           `json:"chosen"`
	Note       string           `json:"note,omitempty"`
	RecordedAt time.Time        `json:"recorded_at"`
}

// Profile is the file on disk. AuditionChoices came later and stays out of
// files that have none, so the version is still 1 and older files read as is.
type Profile struct {
	Version         int              `json:"version"`
	Preferences     []Preference     `json:"preferences"`
	AuditionChoices []AuditionChoice `json:"audition_choices,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("taste profile path is required")
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Record(preference Preference) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile, err := s.load()
	if err != nil {
		return Profile{}, err
	}
	if preference.RecordedAt.IsZero() {
		preference.RecordedAt = time.Now().UTC()
	}
	profile.Preferences = append(profile.Preferences, preference)
	if err := s.save(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (s *Store) RecordAuditionChoice(choice AuditionChoice) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile, err := s.load()
	if err != nil {
		return Profile{}, err
	}
	if choice.RecordedAt.IsZero() {
		choice.RecordedAt = time.Now().UTC()
	}
	profile.AuditionChoices = append(profile.AuditionChoices, choice)
	if err := s.save(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (s *Store) Load() (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (Profile, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{Version: profileVersion, Preferences: []Preference{}}, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read taste profile: %w", err)
	}

	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, fmt.Errorf("parse taste profile: %w", err)
	}
	if profile.Version != profileVersion {
		return Profile{}, fmt.Errorf("unsupported taste profile version: %d", profile.Version)
	}
	if profile.Preferences == nil {
		profile.Preferences = []Preference{}
	}
	return profile, nil
}

func (s *Store) save(profile Profile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create taste profile directory: %w", err)
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode taste profile: %w", err)
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(filepath.Dir(s.path), ".taste-profile-*.json")
	if err != nil {
		return fmt.Errorf("create taste profile temp file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write taste profile: %w", err)
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set taste profile permissions: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close taste profile: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace taste profile: %w", err)
	}
	return nil
}
