package tools

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type arrClip struct {
	name       string
	start, end float64
}

// fakeArrangementLive is a Live set with Session clips of known lengths and an
// Arrangement, behaving the way Live 11 was seen to (2026-09-21):
//   - a copy into the Arrangement is as long as the clip's loop and carries its
//     name; it replaces only the part of older clips that it covers
//   - the patch lists the clips that overlap a range and deletes only those
//     wholly inside one
//   - a new track is appended and becomes the selected track
type fakeArrangementLive struct {
	trackNames    []string
	sceneNames    []string
	lengths       map[[2]int]float64 // {track, scene} -> beats; absent: empty slot
	clipNames     map[[2]int]string
	arrangement   map[int][]arrClip
	cues          []arrClip // name and start only
	selectedTrack int
	patchMissing  bool
	dropCopies    int // copies Live silently does not make (to test the read-back)
	failSend      map[string]error
	sent          []string
}

func newFakeArrangementLive() *fakeArrangementLive {
	return &fakeArrangementLive{
		trackNames: []string{"Drums", "808", "Vox"},
		sceneNames: []string{"Hook", "Intro", ""},
		lengths: map[[2]int]float64{
			{0, 0}: 8, {1, 0}: 16, // Hook: two-bar drums, four-bar 808
			{0, 1}: 4, // Intro: one-bar drums only
		},
		clipNames:   map[[2]int]string{{0, 0}: "Drums A", {1, 0}: "808 A", {0, 1}: "Drums intro"},
		arrangement: map[int][]arrClip{},
	}
}

func (f *fakeArrangementLive) touching(track int, from, to float64) []arrClip {
	var out []arrClip
	for _, c := range f.arrangement[track] {
		if c.start < to-1e-4 && c.end > from+1e-4 {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

func (f *fakeArrangementLive) Query(address string, args ...interface{}) ([]interface{}, error) {
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	floatArg := func(i int) float64 { return float64(args[i].(float32)) }
	switch address {
	case "/live/song/get/tempo":
		return []interface{}{float32(120)}, nil
	case "/live/song/get/signature_numerator":
		return []interface{}{int32(4)}, nil
	case "/live/song/get/scenes/name":
		out := make([]interface{}, len(f.sceneNames))
		for i, n := range f.sceneNames {
			out[i] = n
		}
		return out, nil
	case "/live/song/get/num_tracks":
		return []interface{}{int32(len(f.trackNames))}, nil
	case "/live/song/get/num_scenes":
		return []interface{}{int32(len(f.sceneNames))}, nil
	case "/live/song/get/track_names":
		out := make([]interface{}, len(f.trackNames))
		for i, n := range f.trackNames {
			out[i] = n
		}
		return out, nil
	case "/live/song/get/track_data":
		var out []interface{}
		for t := intArg(0); t < intArg(1); t++ {
			for s := range f.sceneNames {
				if length, ok := f.lengths[[2]int{t, s}]; ok {
					out = append(out, float32(length))
				} else {
					out = append(out, nil)
				}
			}
		}
		return out, nil
	case "/live/view/get/selected_track":
		return []interface{}{int32(f.selectedTrack)}, nil
	case "/live/clip_slot/get/has_clip":
		_, has := f.lengths[[2]int{intArg(0), intArg(1)}]
		return []interface{}{int32(intArg(0)), int32(intArg(1)), has}, nil
	case "/live/clip/get/name":
		return []interface{}{int32(intArg(0)), int32(intArg(1)), f.clipNames[[2]int{intArg(0), intArg(1)}]}, nil
	case "/live/song/get/cue_points":
		var out []interface{}
		for _, cue := range f.cues {
			out = append(out, cue.name, float32(cue.start))
		}
		return out, nil
	case "/live/track/get/arrangement_clips":
		if f.patchMissing {
			return nil, errors.New("no response received to query: " + address)
		}
		from, to := 0.0, 1e12
		if len(args) >= 3 {
			from, to = floatArg(1), floatArg(2)
		}
		clips := f.touching(intArg(0), from, to)
		out := []interface{}{int32(intArg(0)), int32(len(clips))}
		for _, c := range clips {
			out = append(out, c.name, float32(c.start), float32(c.end))
		}
		return out, nil
	case "/live/track/delete_arrangement_clips":
		if f.patchMissing {
			return nil, errors.New("no response received to query: " + address)
		}
		f.sent = append(f.sent, fmt.Sprintf("delete %d %v-%v", intArg(0), floatArg(1), floatArg(2)))
		track, from, to := intArg(0), floatArg(1), floatArg(2)
		var kept []arrClip
		deleted := 0
		for _, c := range f.arrangement[track] {
			if c.start >= from-1e-4 && c.end <= to+1e-4 {
				deleted++
			} else {
				kept = append(kept, c)
			}
		}
		f.arrangement[track] = kept
		return []interface{}{int32(track), "ok", int32(deleted), int32(len(f.touching(track, from, to)))}, nil
	}
	return nil, errors.New("unexpected query: " + address)
}

func (f *fakeArrangementLive) Send(address string, args ...interface{}) error {
	if err := f.failSend[address]; err != nil {
		return err
	}
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	f.sent = append(f.sent, address)
	switch address {
	case "/live/clip_slot/duplicate_clip_to_arrangement":
		key := [2]int{intArg(0), intArg(1)}
		if f.dropCopies > 0 {
			f.dropCopies--
			return nil
		}
		start := float64(args[2].(float32))
		end := start + f.lengths[key]
		var kept []arrClip
		for _, c := range f.arrangement[key[0]] { // a copy replaces what it covers, and only that
			if c.start < start {
				kept = append(kept, arrClip{c.name, c.start, min(c.end, start)})
			}
			if c.end > end {
				kept = append(kept, arrClip{c.name, max(c.start, end), c.end})
			}
		}
		f.arrangement[key[0]] = append(kept, arrClip{f.clipNames[key], start, end})
	case "/live/song/create_midi_track":
		f.trackNames = append(f.trackNames, fmt.Sprintf("%d-MIDI", len(f.trackNames)+1))
		f.selectedTrack = len(f.trackNames) - 1
	case "/live/track/set/name":
		f.trackNames[intArg(0)] = args[1].(string)
	case "/live/view/set/selected_track":
		f.selectedTrack = intArg(0)
	case "/live/clip_slot/create_clip":
		f.lengths[[2]int{intArg(0), intArg(1)}] = float64(args[2].(float32))
	case "/live/clip/set/name":
		f.clipNames[[2]int{intArg(0), intArg(1)}] = args[2].(string)
	case "/live/clip_slot/delete_clip":
		delete(f.lengths, [2]int{intArg(0), intArg(1)})
		delete(f.clipNames, [2]int{intArg(0), intArg(1)})
	default:
		return errors.New("unexpected send: " + address)
	}
	return nil
}

func (f *fakeArrangementLive) clips(track int) []arrClip { return f.touching(track, 0, 1e12) }

func noSleep(time.Duration) {}

// Intro for two bars, then the Hook for eight.
func testSong() []SongSection {
	return []SongSection{{SceneIndex: 1, Bars: 2}, {SceneIndex: 0, Bars: 8, Name: "Hook 1"}}
}

func TestWriteArrangementFillsEverySectionWithItsClips(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	live.selectedTrack = 1
	got, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()})
	if err != nil {
		t.Fatalf("writeArrangement() error = %v", err)
	}
	// Drums: two one-bar intro clips, then four two-bar hook clips. 808: two four-bar clips from bar 3.
	wantDrums := []arrClip{{"Drums intro", 0, 4}, {"Drums intro", 4, 8}, {"Drums A", 8, 16}, {"Drums A", 16, 24}, {"Drums A", 24, 32}, {"Drums A", 32, 40}}
	if !reflect.DeepEqual(live.clips(0), wantDrums) {
		t.Errorf("drums = %v, want %v", live.clips(0), wantDrums)
	}
	if want := []arrClip{{"808 A", 8, 24}, {"808 A", 24, 40}}; !reflect.DeepEqual(live.clips(1), want) {
		t.Errorf("808 = %v, want %v", live.clips(1), want)
	}
	if len(live.clips(2)) != 0 {
		t.Errorf("vox = %v; the song has nothing for that track", live.clips(2))
	}
	// The sections show on a track of their own, named after the scene unless named.
	if len(live.trackNames) != 4 || live.trackNames[3] != "Sections" || live.selectedTrack != 1 {
		t.Fatalf("tracks = %v, selected = %d; want a Sections track at the end and the selection back on 1", live.trackNames, live.selectedTrack)
	}
	if want := []arrClip{{"Intro", 0, 8}, {"Hook 1", 8, 40}}; !reflect.DeepEqual(live.clips(3), want) {
		t.Errorf("sections = %v, want %v", live.clips(3), want)
	}
	if len(live.lengths) != 3 {
		t.Errorf("session clips = %v; the scratch clips of the Sections track should be gone", live.lengths)
	}

	if got.StartBar != 1 || got.EndBar != 10 || got.TotalBars != 10 || got.DurationSec != 20 || got.ClipsPlaced != 8 || !got.Verified {
		t.Errorf("got = %+v, want bars 1-10, 20 s, 8 clips placed, verified", got)
	}
	wantSections := []WrittenSection{{Name: "Intro", SceneIndex: 1, StartBar: 1, Bars: 2, ClipsPlaced: 2}, {Name: "Hook 1", SceneIndex: 0, StartBar: 3, Bars: 8, ClipsPlaced: 6}}
	if !reflect.DeepEqual(got.Sections, wantSections) || !reflect.DeepEqual(got.TracksWritten, []int{0, 1}) {
		t.Errorf("sections = %+v, tracks = %v", got.Sections, got.TracksWritten)
	}
}

func TestWriteArrangementRefusesASectionItsClipsDoNotFill(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	_, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: []SongSection{{SceneIndex: 0, Bars: 6}}})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "section_not_multiple_of_clip" {
		t.Fatalf("error = %v, want section_not_multiple_of_clip", err)
	}
	// Six bars hold three two-bar drum clips, but not a whole number of four-bar 808 clips.
	if !strings.Contains(actionableErr.Message, "808 A") || strings.Contains(actionableErr.Message, "Drums A") {
		t.Errorf("message = %q; it should name the 808 clip and only that", actionableErr.Message)
	}
	if len(live.sent) != 0 {
		t.Errorf("Live was touched: %v", live.sent)
	}
}

func TestWriteArrangementNeverWritesOverWhatIsThereUnlessTold(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	live.arrangement[0] = []arrClip{{"old drums", 4, 8}}
	live.arrangement[2] = []arrClip{{"Vocal take", 0, 64}} // a track the song does not use
	_, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_occupied" || !strings.Contains(actionableErr.Message, "old drums") {
		t.Fatalf("error = %v, want arrangement_occupied naming the clip in the way", err)
	}
	if len(live.sent) != 0 {
		t.Errorf("Live was touched: %v", live.sent)
	}

	got, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong(), Overwrite: true})
	if err != nil {
		t.Fatalf("with overwrite: error = %v", err)
	}
	if clips := live.clips(0); len(clips) != 6 || clips[1].name != "Drums intro" {
		t.Errorf("drums = %v; the old clip should have made way", clips)
	}
	if !reflect.DeepEqual(live.clips(2), []arrClip{{"Vocal take", 0, 64}}) {
		t.Errorf("vox = %v; the song does not use that track, so nothing on it may go", live.clips(2))
	}
	if got.ClipsReplaced != 1 {
		t.Errorf("clips_replaced = %d, want 1", got.ClipsReplaced)
	}
}

func TestWriteArrangementRewritesASongWithoutLeavingTheOldOneBehind(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	if _, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()}); err != nil {
		t.Fatal(err)
	}
	// The same ten bars, now Intro all the way: the 808 has nothing to play any more.
	if _, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: []SongSection{{SceneIndex: 1, Bars: 10}}, Overwrite: true}); err != nil {
		t.Fatalf("rewrite: error = %v", err)
	}
	if len(live.clips(1)) != 0 {
		t.Errorf("808 = %v; the first version's clips should be gone", live.clips(1))
	}
	if want := []arrClip{{"Intro", 0, 40}}; !reflect.DeepEqual(live.clips(3), want) || len(live.trackNames) != 4 {
		t.Errorf("sections = %v on tracks %v; want one Intro on the one Sections track", live.clips(3), live.trackNames)
	}
}

func TestWriteArrangementStartsWhereItIsTold(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	got, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: []SongSection{{SceneIndex: 1, Bars: 1}}, StartBar: 9})
	if err != nil {
		t.Fatal(err)
	}
	if want := []arrClip{{"Drums intro", 32, 36}}; !reflect.DeepEqual(live.clips(0), want) || got.StartBar != 9 || got.EndBar != 9 {
		t.Errorf("drums = %v, bars %d-%d; want one clip on bar 9", live.clips(0), got.StartBar, got.EndBar)
	}
}

func TestWriteArrangementTakesBackWhatItPlacedWhenItFails(t *testing.T) {
	t.Parallel()

	// Live drops two copies without a word. Only reading back can tell.
	live := newFakeArrangementLive()
	live.dropCopies = 2
	_, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_not_as_planned" {
		t.Fatalf("error = %v, want arrangement_not_as_planned", err)
	}
	for track := range live.trackNames {
		if clips := live.clips(track); len(clips) != 0 {
			t.Errorf("track %d still has %v; the range was empty before and should be again", track, clips)
		}
	}

	live = newFakeArrangementLive()
	live.failSend = map[string]error{"/live/clip_slot/create_clip": errors.New("boom")}
	if _, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()}); err == nil {
		t.Fatal("expected the failed send to surface")
	}
	if len(live.clips(0)) != 0 || len(live.clips(1)) != 0 {
		t.Errorf("drums = %v, 808 = %v; want them taken back", live.clips(0), live.clips(1))
	}
}

func TestArrangementToolsNeedThePatch(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	live.patchMissing = true
	_, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_patch_missing" || len(live.sent) != 0 {
		t.Errorf("write: error = %v, sent = %v; want arrangement_patch_missing and nothing sent", err, live.sent)
	}
	if _, err := getArrangement(live, GetArrangementInput{}); !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_patch_missing" {
		t.Errorf("read: error = %v, want arrangement_patch_missing", err)
	}
}

func TestGetArrangementReadsTheSongBack(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	if _, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()}); err != nil {
		t.Fatal(err)
	}
	live.arrangement[2] = []arrClip{{"Vocal take", 10, 30}}
	live.cues = []arrClip{{name: "2", start: 32}, {name: "1", start: 8}} // in the order they were made

	got, err := getArrangement(live, GetArrangementInput{})
	if err != nil {
		t.Fatalf("getArrangement() error = %v", err)
	}
	if got.TempoBPM != 120 || got.BeatsPerBar != 4 || got.EndBar != 10 {
		t.Errorf("tempo %v, %d beats per bar, end bar %v; want 120, 4, 10", got.TempoBPM, got.BeatsPerBar, got.EndBar)
	}
	wantSections := []ArrangementSpan{{Name: "Intro", StartBar: 1, Bars: 2}, {Name: "Hook 1", StartBar: 3, Bars: 8}}
	if !reflect.DeepEqual(got.Sections, wantSections) {
		t.Errorf("sections = %+v, want %+v", got.Sections, wantSections)
	}
	if want := []ArrangementLocator{{Name: "1", Bar: 3}, {Name: "2", Bar: 9}}; !reflect.DeepEqual(got.Locators, want) {
		t.Errorf("locators = %+v, want them in time order: %+v", got.Locators, want)
	}
	// Four hook clips in a row read as one line. The Sections track is not listed among the tracks.
	wantTracks := []ArrangementTrack{
		{TrackIndex: 0, Name: "Drums", Clips: []ArrangementSpan{{Name: "Drums intro", StartBar: 1, Bars: 1, Repeats: 2}, {Name: "Drums A", StartBar: 3, Bars: 2, Repeats: 4}}},
		{TrackIndex: 1, Name: "808", Clips: []ArrangementSpan{{Name: "808 A", StartBar: 3, Bars: 4, Repeats: 2}}},
		{TrackIndex: 2, Name: "Vox", Clips: []ArrangementSpan{{Name: "Vocal take", StartBar: 3.5, Bars: 5}}},
	}
	if !reflect.DeepEqual(got.Tracks, wantTracks) {
		t.Errorf("tracks = %+v\nwant     %+v", got.Tracks, wantTracks)
	}

	narrowed, err := getArrangement(live, GetArrangementInput{FromBar: 3, ToBar: 4, TrackIndices: []int{0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed.Tracks) != 1 || len(narrowed.Tracks[0].Clips) != 1 || narrowed.Tracks[0].Clips[0].Name != "Drums A" || narrowed.Tracks[0].Clips[0].Repeats != 0 {
		t.Errorf("bars 3-4 of the drums = %+v, want the one hook clip that sits there", narrowed.Tracks)
	}
}

// Found on a real Live: a song rewritten shorter left the end of the longer
// version playing on behind it. The Sections track says how far that version went.
func TestWriteArrangementRewrittenShorterLeavesNoOldEnding(t *testing.T) {
	t.Parallel()

	live := newFakeArrangementLive()
	if _, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()}); err != nil {
		t.Fatal(err)
	}
	live.arrangement[0] = append(live.arrangement[0], arrClip{"Another sketch", 400, 408}) // far away, not part of the song

	// Without overwrite, the refusal already counts the old ending among what is in the way.
	_, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()[:1]})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_occupied" || !strings.Contains(actionableErr.Message, "808 A") {
		t.Fatalf("error = %v; want arrangement_occupied that lists the old version's 808 too", err)
	}

	got, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: testSong()[:1], Overwrite: true})
	if err != nil {
		t.Fatalf("rewrite: error = %v", err)
	}
	if want := []arrClip{{"Drums intro", 0, 4}, {"Drums intro", 4, 8}, {"Another sketch", 400, 408}}; !reflect.DeepEqual(live.clips(0), want) {
		t.Errorf("drums = %v, want %v", live.clips(0), want)
	}
	if len(live.clips(1)) != 0 || !reflect.DeepEqual(live.clips(3), []arrClip{{"Intro", 0, 8}}) {
		t.Errorf("808 = %v, sections = %v; want nothing left of the longer version", live.clips(1), live.clips(3))
	}
	if got.ClipsReplaced != 10 || !got.Verified {
		t.Errorf("clips_replaced = %d, verified = %v; want the 8 clips and 2 markers of the first version", got.ClipsReplaced, got.Verified)
	}
}

// Live 11 cannot cut an Arrangement clip, and deleting takes the whole clip. A
// clip that lies across the first or the last bar line of the song can therefore
// be neither kept nor removed: what the new copies do not cover would play on.
// That is refused before anything is touched, overwrite or not.
func TestWriteArrangementRefusesAClipThatLiesAcrossItsEdges(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		before   func(*fakeArrangementLive)
		input    WriteArrangementInput
		mentions string
	}{
		// Reported on the pull request: an eight-bar hook written from bar 1, the song rewritten from bar 2.
		"an old long clip across the new start": {
			before: func(live *fakeArrangementLive) {
				live.lengths[[2]int{1, 0}] = 32
				if _, err := writeArrangement(live, noSleep, WriteArrangementInput{Sections: []SongSection{{SceneIndex: 0, Bars: 8}}}); err != nil {
					t.Fatal(err)
				}
			},
			input:    WriteArrangementInput{Sections: []SongSection{{SceneIndex: 1, Bars: 1}}, StartBar: 2, Overwrite: true},
			mentions: "808 A",
		},
		"a clip that runs out over the end": {
			before:   func(live *fakeArrangementLive) { live.arrangement[0] = []arrClip{{"long take", 36, 48}} },
			input:    WriteArrangementInput{Sections: testSong(), Overwrite: true},
			mentions: "long take",
		},
	} {
		live := newFakeArrangementLive()
		tc.before(live)
		snapshot := fmt.Sprint(live.arrangement)
		live.sent = nil

		_, err := writeArrangement(live, noSleep, tc.input)
		var actionableErr *ActionableError
		if !errors.As(err, &actionableErr) || actionableErr.Code != "arrangement_clip_crosses_range" || !strings.Contains(actionableErr.Message, tc.mentions) {
			t.Errorf("%s: error = %v, want arrangement_clip_crosses_range naming %q", name, err, tc.mentions)
		}
		if len(live.sent) != 0 || fmt.Sprint(live.arrangement) != snapshot {
			t.Errorf("%s: Live was touched: %v", name, live.sent)
		}
	}
}

// What is read back has to be the plan and nothing else: a leftover piece of an
// older clip inside the song's stretch must not pass as verified.
func TestWriteArrangementVerifiesThatNothingElseIsLeftInItsStretch(t *testing.T) {
	t.Parallel()

	got := compareArrangement(
		[]arrangementClip{{Name: "Drums A", Start: 0, End: 8}, {Name: "left over", Start: 8, End: 12}},
		[]arrangementClip{{Start: 0, End: 8}})
	if got == "" {
		t.Error("a clip that is not in the plan went unnoticed")
	}
	if got := compareArrangement([]arrangementClip{{Name: "Drums A", Start: 0, End: 8}}, []arrangementClip{{Start: 0, End: 8}}); got != "" {
		t.Errorf("the plan itself is reported as a problem: %s", got)
	}
}
