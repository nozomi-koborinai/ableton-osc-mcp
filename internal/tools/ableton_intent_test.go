package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveParamIndex(t *testing.T) {
	names := []string{"Device On", "Frequency", "Filter Type", "Dry/Wet"}
	cases := []struct {
		q      string
		want   int
		wantOK bool
	}{
		{"Frequency", 1, true},    // exact
		{"frequency", 1, true},    // case-insensitive
		{"freq", 1, true},         // substring
		{"filter", 2, true},       // substring
		{"2", 2, true},            // numeric index
		{"dry/wet", 3, true},      // ci with slash
		{"nonexistent", 0, false}, // no match
		{"9", 0, false},           // out of range index
		{"", 0, false},            // empty
	}
	for _, c := range cases {
		got, ok := resolveParamIndex(names, c.q)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("resolveParamIndex(%q) = (%d,%v), want (%d,%v)", c.q, got, ok, c.want, c.wantOK)
		}
	}
}

func TestIntentPathSanitize(t *testing.T) {
	path, err := intentPath("HP 180Hz / bright!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := filepath.Base(path)
	if base != "HP 180Hz _ bright_.json" {
		t.Errorf("sanitized base = %q", base)
	}
	if _, err := intentPath("   "); err == nil {
		t.Error("expected error for blank name")
	}
}

func TestIntentRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.json")
	in := Intent{
		Version: intentVersion,
		Name:    "hp-clean",
		Settings: []IntentSetting{
			{Param: "Filter Type", Value: "HP"},
			{Param: "Frequency", Value: "180 Hz"},
		},
	}
	if err := writeIntent(path, in); err != nil {
		t.Fatalf("writeIntent: %v", err)
	}
	got, err := readIntent(path)
	if err != nil {
		t.Fatalf("readIntent: %v", err)
	}
	if got.Name != "hp-clean" || len(got.Settings) != 2 {
		t.Fatalf("round trip = %+v", got)
	}
	if got.Settings[1].Param != "Frequency" || got.Settings[1].Value != "180 Hz" {
		t.Errorf("settings not preserved: %+v", got.Settings)
	}
}

func TestReadIntentMissing(t *testing.T) {
	if _, err := readIntent(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing intent")
	}
}

func TestReadIntentBadVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"name":"x","settings":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readIntent(path); err == nil {
		t.Fatal("expected error for unsupported version")
	}
}
