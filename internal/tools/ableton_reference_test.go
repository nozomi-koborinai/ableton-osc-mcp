package tools

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/reference"
)

// writeToneWAV writes a mono 16-bit sine, loud enough to measure.
func writeToneWAV(t *testing.T, path string, freq float64, seconds int) {
	t.Helper()
	const sampleRate = 44100
	n := sampleRate * seconds
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	write := func(v interface{}) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = f.Write([]byte("RIFF"))
	write(uint32(36 + n*2))
	_, _ = f.Write([]byte("WAVE"))
	_, _ = f.Write([]byte("fmt "))
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(sampleRate))
	write(uint32(sampleRate * 2))
	write(uint16(2))
	write(uint16(16))
	_, _ = f.Write([]byte("data"))
	write(uint32(n * 2))
	for i := 0; i < n; i++ {
		write(int16(math.Round(16000 * math.Sin(2*math.Pi*freq*float64(i)/sampleRate))))
	}
}

func newReferenceTestStore(t *testing.T) *reference.Store {
	t.Helper()
	store, err := reference.NewStore(filepath.Join(t.TempDir(), "refs.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAnalyzeLocalAudioSavesAReferenceProfile(t *testing.T) {
	t.Parallel()

	store := newReferenceTestStore(t)
	path := filepath.Join(t.TempDir(), "ref.wav")
	writeToneWAV(t, path, 100, 3)

	got, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, SaveReferenceAs: "  Low-Tone "}, store)
	if err != nil {
		t.Fatalf("analyzeLocalAudio() error = %v", err)
	}
	if got.SavedReference != "low-tone" {
		t.Errorf("saved_reference = %q, want low-tone", got.SavedReference)
	}
	if got.MixProfile == nil || len(got.MixProfile.Bands) != 9 {
		t.Fatalf("mix_profile = %+v, want 9 bands", got.MixProfile)
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("store.List() = %+v, %v; want one profile", list, err)
	}
	if list[0].SourceKind != "file" || list[0].Source != path || len(list[0].Mix.Bands) != 9 {
		t.Errorf("stored profile = %+v", list[0])
	}
}

func TestAnalyzeLocalAudioLeavesTheStoreAloneUnlessAsked(t *testing.T) {
	t.Parallel()

	store := newReferenceTestStore(t)
	path := filepath.Join(t.TempDir(), "mine.wav")
	writeToneWAV(t, path, 100, 2)

	if _, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path}, store); err != nil {
		t.Fatalf("analyzeLocalAudio() error = %v", err)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Errorf("store has %d profiles after a plain analysis, want 0", len(list))
	}
}

func TestAnalyzeLocalAudioComparesAgainstSavedReferences(t *testing.T) {
	t.Parallel()

	store := newReferenceTestStore(t)
	dir := t.TempDir()
	refPath, minePath := filepath.Join(dir, "ref.wav"), filepath.Join(dir, "mine.wav")
	writeToneWAV(t, refPath, 100, 3)   // all energy in 60-120
	writeToneWAV(t, minePath, 3000, 3) // all energy in 2-4k
	if _, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: refPath, SaveReferenceAs: "low"}, store); err != nil {
		t.Fatal(err)
	}

	got, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: minePath, References: []reference.Weight{{Name: "low"}}}, store)
	if err != nil {
		t.Fatalf("analyzeLocalAudio() error = %v", err)
	}
	if got.Reference == nil {
		t.Fatal("reference comparison missing")
	}
	deltas := map[string]float64{}
	for _, d := range got.Reference.BandDeltasDB {
		deltas[d.Label] = d.DeltaDB
	}
	if deltas["60-120"] > -20 || deltas["2-4k"] < 20 {
		t.Errorf("deltas = %v, want 60-120 far below and 2-4k far above the reference", deltas)
	}
	flagged := strings.Join(got.Reference.OutOfRange, ",")
	if !strings.Contains(flagged, "60-120") || !strings.Contains(flagged, "2-4k") {
		t.Errorf("out_of_range = %v, want both 60-120 and 2-4k", got.Reference.OutOfRange)
	}
	if got.Reference.Caveat == "" || len(got.Reference.Blend) != 1 || got.Reference.Blend[0].Weight != 1 {
		t.Errorf("blend/caveat = %+v", got.Reference)
	}
}

func TestAnalyzeLocalAudioUnknownReferenceListsWhatIsSaved(t *testing.T) {
	t.Parallel()

	store := newReferenceTestStore(t)
	path := filepath.Join(t.TempDir(), "mine.wav")
	writeToneWAV(t, path, 100, 2)
	if _, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, SaveReferenceAs: "envy"}, store); err != nil {
		t.Fatal(err)
	}

	_, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, References: []reference.Weight{{Name: "rainy"}}}, store)
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "unknown_reference" {
		t.Fatalf("error = %v, want ActionableError unknown_reference", err)
	}
	if !strings.Contains(actionableErr.NextStep, "envy") {
		t.Errorf("next step should list the saved names: %q", actionableErr.NextStep)
	}
}

func TestAnalyzeLocalAudioRejectsABadReferenceNameBeforeAnalyzing(t *testing.T) {
	t.Parallel()

	// The path does not exist: reaching the analysis would fail differently.
	_, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: "/nonexistent/x.wav", SaveReferenceAs: "not valid"}, newReferenceTestStore(t))
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "invalid_reference_name" {
		t.Fatalf("error = %v, want ActionableError invalid_reference_name", err)
	}
}

func TestAnalyzeLocalAudioPassesTheWindowThrough(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mine.wav")
	writeToneWAV(t, path, 100, 4)
	start, end := 1.0, 3.0

	got, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, StartSec: &start, EndSec: &end}, nil)
	if err != nil {
		t.Fatalf("analyzeLocalAudio() error = %v", err)
	}
	if math.Abs(got.DurationSec-2) > 0.01 || got.RangeStartSec != 1 || got.RangeEndSec != 3 {
		t.Errorf("duration/range = %v [%v, %v], want 2 [1, 3]", got.DurationSec, got.RangeStartSec, got.RangeEndSec)
	}
}

func TestAnalyzeLocalAudioWithoutAStoreExplainsItself(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mine.wav")
	writeToneWAV(t, path, 100, 2)

	_, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, SaveReferenceAs: "x"}, nil)
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "reference_store_unavailable" {
		t.Fatalf("error = %v, want ActionableError reference_store_unavailable", err)
	}
}

func TestListReferenceProfilesSummarizesTheStore(t *testing.T) {
	t.Parallel()

	store := newReferenceTestStore(t)
	path := filepath.Join(t.TempDir(), "ref.wav")
	writeToneWAV(t, path, 100, 3)
	if _, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, SaveReferenceAs: "low"}, store); err != nil {
		t.Fatal(err)
	}

	got, err := listReferenceProfiles(store)
	if err != nil {
		t.Fatalf("listReferenceProfiles() error = %v", err)
	}
	if got.StorePath != store.Path() || len(got.Profiles) != 1 {
		t.Fatalf("got = %+v", got)
	}
	p := got.Profiles[0]
	if p.Name != "low" || p.SourceKind != "file" || p.Source != path || len(p.Bands) != 9 || p.SavedAt == "" {
		t.Errorf("summary = %+v", p)
	}
}

func TestAnalysisOptionsCarryTheDeepRequest(t *testing.T) {
	t.Parallel()

	zero := 0.0
	opts := analysisOptions(nil, nil, nil, true, &zero)
	if !opts.Deep || !opts.DownbeatSet || opts.DownbeatSec != 0 {
		t.Errorf("opts = %+v; a downbeat of zero is a downbeat that was given", opts)
	}
	if opts := analysisOptions(nil, nil, nil, false, nil); opts.Deep || opts.DownbeatSet {
		t.Errorf("opts = %+v, want nothing deep about a plain analysis", opts)
	}
}

func TestCapHarmonyKeepsURLRepliesSmall(t *testing.T) {
	t.Parallel()

	long := &audioanalyze.Harmony{Chords: make([]audioanalyze.HarmonyChord, 200), Summary: "Am | F", Note: "n."}
	got := capHarmony(long, maxURLHarmonyChords)
	if len(got.Chords) != 64 || got.Summary != "Am | F" || !strings.Contains(got.Note, "first 64") || len(long.Chords) != 200 {
		t.Errorf("capped to %d chords, note %q; the original has %d", len(got.Chords), got.Note, len(long.Chords))
	}
	if short := (&audioanalyze.Harmony{Chords: make([]audioanalyze.HarmonyChord, 3)}); capHarmony(short, 64) != short || capHarmony(nil, 64) != nil {
		t.Error("a short list and no list pass through untouched")
	}
}
