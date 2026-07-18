package splice

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfiguredPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lib, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want, _ := filepath.Abs(dir)
	if lib.Source != "env" || lib.Path != want {
		t.Errorf("library = %#v, want path %q", lib, want)
	}
}

func TestResolveMissing(t *testing.T) {
	t.Parallel()

	_, err := Resolve(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestSearchSamples(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sounds", "kick_punch.wav"), "x")
	mustWrite(t, filepath.Join(root, "sounds", "snare.aiff"), "x")
	mustWrite(t, filepath.Join(root, "readme.txt"), "nope")

	got, err := Search(root, "kick", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "kick_punch.wav" {
		t.Fatalf("got = %#v", got)
	}
	if got[0].RelativePath != "sounds/kick_punch.wav" {
		t.Errorf("relative_path = %q", got[0].RelativePath)
	}
}

func TestSearchRequiresAudioExtension(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "kick.txt"), "x")
	got, err := Search(root, "kick", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %#v, want none", got)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
