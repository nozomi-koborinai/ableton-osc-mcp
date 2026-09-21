package reference

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "nested", "reference-profiles.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func TestNormalizeName(t *testing.T) {
	t.Parallel()

	got, err := NormalizeName("  My-Ref_1 ")
	if err != nil || got != "my-ref_1" {
		t.Errorf("NormalizeName() = %q, %v; want my-ref_1", got, err)
	}
	for _, bad := range []string{"", "   ", "has space", "日本語", strings.Repeat("a", 41), "semi;colon"} {
		if _, err := NormalizeName(bad); err == nil {
			t.Errorf("NormalizeName(%q) should fail", bad)
		}
	}
	if _, err := NormalizeName(strings.Repeat("a", 40)); err != nil {
		t.Errorf("40 characters should be accepted: %v", err)
	}
}

func TestStoreListsSavedProfilesByName(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	for _, name := range []string{"zeta", " Alpha "} {
		if _, err := store.Save(Profile{Name: name, SourceKind: "file", Source: "/tmp/" + name + ".wav"}); err != nil {
			t.Fatalf("Save(%q) error = %v", name, err)
		}
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("List() names = %+v, want [alpha zeta]", list)
	}
	if list[0].SavedAt.IsZero() {
		t.Error("SavedAt was not stamped")
	}
}

func TestStoreSaveOverwritesTheSameName(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	_, _ = store.Save(Profile{Name: "envy", BPM: 140})
	_, _ = store.Save(Profile{Name: "ENVY", BPM: 143.6})

	list, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].BPM != 143.6 {
		t.Errorf("List() = %+v, want one profile with bpm 143.6", list)
	}
}

func TestStoreSurvivesAReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "refs.json")
	first, _ := NewStore(path)
	mix := audioanalyze.MixProfile{LUFSIntegrated: -9.5, Bands: []audioanalyze.BandLevel{{Label: "<60", DB: -3}}}
	if _, err := first.Save(Profile{Name: "kept", Mix: mix}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	second, _ := NewStore(path)
	list, err := second.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List() after reopen = %+v, %v", list, err)
	}
	if list[0].Mix.LUFSIntegrated != -9.5 || list[0].Mix.Bands[0].DB != -3 {
		t.Errorf("mix did not round-trip: %+v", list[0].Mix)
	}
}

func TestStoreRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	if _, err := newTestStore(t).Save(Profile{Name: "not valid"}); err == nil {
		t.Error("Save() with an invalid name should fail")
	}
}
