package taste

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRecordAndLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "taste-profile.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	recordedAt := time.Date(2026, time.July, 18, 7, 0, 0, 0, time.UTC)
	profile, err := store.Record(Preference{
		Instrument: "drum",
		Variation:  "groove",
		Preferred:  "variation",
		Note:       "More relaxed",
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if profile.Version != profileVersion || len(profile.Preferences) != 1 {
		t.Fatalf("Record() profile = %#v", profile)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Preferences) != 1 {
		t.Fatalf("Load() preferences = %v, want one", loaded.Preferences)
	}
	if got := loaded.Preferences[0]; got.Instrument != "drum" || got.Variation != "groove" || got.Preferred != "variation" || !got.RecordedAt.Equal(recordedAt) {
		t.Errorf("loaded preference = %#v", got)
	}
}

func TestStoreLoadMissingReturnsEmptyProfile(t *testing.T) {
	t.Parallel()

	store, err := NewStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	profile, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if profile.Version != profileVersion || len(profile.Preferences) != 0 {
		t.Errorf("Load() = %#v, want empty current profile", profile)
	}
}

func TestNewStoreRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(""); err == nil {
		t.Fatal("NewStore(\"\") error = nil, want error")
	}
}
