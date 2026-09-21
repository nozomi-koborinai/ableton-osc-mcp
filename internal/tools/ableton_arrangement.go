package tools

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	// arrangementSectionsTrack shows the song's sections in the Arrangement: one
	// empty clip per section, named after it. Live 11 can set a locator but not
	// name it, and a nameless locator says nothing.
	arrangementSectionsTrack = "Sections"
	arrangementBatch         = 200 // copies sent before giving Live a moment
	arrangementBatchPause    = 50 * time.Millisecond
	arrangementBeatSlack     = 1e-3
)

type WriteArrangementInput struct {
	Sections  []SongSection `json:"sections" jsonschema:"description=The song as a list of sections\\, each a scene for so many bars (1-64 sections\\, 400 bars in all). A section has to be a whole number of times as long as every clip in its scene"`
	StartBar  int           `json:"start_bar,omitempty" jsonschema:"description=Bar of the Arrangement where the song starts (default 1),minimum=1"`
	Overwrite bool          `json:"overwrite,omitempty" jsonschema:"description=Delete the Arrangement clips that are in the way\\, on the tracks that hold Session clips and on the Sections track. Only after the person has agreed: what is deleted does not come back"`
}

type WrittenSection struct {
	Name        string `json:"name"`
	SceneIndex  int    `json:"scene_index"`
	StartBar    int    `json:"start_bar"`
	Bars        int    `json:"bars"`
	ClipsPlaced int    `json:"clips_placed"`
}

type WriteArrangementOutput struct {
	StartBar           int              `json:"start_bar"`
	EndBar             int              `json:"end_bar" jsonschema:"description=Last bar of the song"`
	TotalBars          int              `json:"total_bars"`
	DurationSec        float64          `json:"duration_sec"`
	TempoBPM           float64          `json:"tempo_bpm"`
	Sections           []WrittenSection `json:"sections"`
	TracksWritten      []int            `json:"tracks_written"`
	ClipsPlaced        int              `json:"clips_placed"`
	ClipsReplaced      int              `json:"clips_replaced"`
	SectionsTrackIndex int              `json:"sections_track_index"`
	Verified           bool             `json:"verified" jsonschema:"description=The Arrangement was read back and holds what was planned"`
	Note               string           `json:"note"`
}

type GetArrangementInput struct {
	FromBar      int   `json:"from_bar,omitempty" jsonschema:"description=First bar to read (default 1),minimum=1"`
	ToBar        int   `json:"to_bar,omitempty" jsonschema:"description=Last bar to read (default: to the end),minimum=1"`
	TrackIndices []int `json:"track_indices,omitempty" jsonschema:"description=Tracks to read (default all)"`
}

// ArrangementSpan is a clip in the Arrangement, or several copies of it in a row.
type ArrangementSpan struct {
	Name     string  `json:"name"`
	StartBar float64 `json:"start_bar"`
	Bars     float64 `json:"bars"`
	Repeats  int     `json:"repeats,omitempty" jsonschema:"description=How many copies follow one another without a gap (omitted for one)"`
}

type ArrangementLocator struct {
	Name string  `json:"name"`
	Bar  float64 `json:"bar"`
}

type ArrangementTrack struct {
	TrackIndex int               `json:"track_index"`
	Name       string            `json:"name"`
	Clips      []ArrangementSpan `json:"clips"`
}

type GetArrangementOutput struct {
	TempoBPM    float64              `json:"tempo_bpm"`
	BeatsPerBar int                  `json:"beats_per_bar"`
	EndBar      float64              `json:"end_bar" jsonschema:"description=Bar in which the last clip ends"`
	Sections    []ArrangementSpan    `json:"sections" jsonschema:"description=What the Sections track shows: the song as ableton_write_arrangement wrote it"`
	Locators    []ArrangementLocator `json:"locators"`
	Tracks      []ArrangementTrack   `json:"tracks" jsonschema:"description=Tracks that have Arrangement clips in the range"`
}

func NewAbletonWriteArrangement(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_write_arrangement",
		"Ableton Live: lay a song out in the Arrangement from `sections` (each a scene for so many bars): every clip of a section's scene is copied end to end until the section is full, and a MIDI track named 'Sections', added at the end of the set if missing, gets one empty clip per section with the section's name, so the shape of the song shows on the timeline. Playback is not stopped and the playhead is not moved. Refuses when Arrangement clips are in the way, on any track that holds Session clips or on the Sections track (an earlier version of the song may have used other tracks than this one); overwrite=true deletes those first, for good, so pass it only after the person has agreed. It deletes whole clips only: a clip that lies across the first or the last bar line of the song is refused either way, because Live 11 cannot cut one. A track without Session clips (a recorded vocal) is never touched. Reads the Arrangement back and reports whether it holds what was planned. A track that is playing a Session clip keeps playing that until Back to Arrangement is pressed in Live. The bounce (ableton_bounce_session_pass) records from the scenes, not from this: edits made on the timeline by hand are not in it.",
		func(_ *ai.ToolContext, input WriteArrangementInput) (WriteArrangementOutput, error) {
			return writeArrangement(client, time.Sleep, input)
		},
	)
}

func NewAbletonGetArrangement(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_arrangement",
		"Ableton Live: read the Arrangement in bars — the sections shown on the 'Sections' track, locators, and for each track its clips, with copies that follow one another folded into one line. Use when the person asks what is on the timeline, or to pick up a song's structure again in a later session; not needed right after ableton_write_arrangement, which reports what it wrote.",
		func(_ *ai.ToolContext, input GetArrangementInput) (GetArrangementOutput, error) {
			return getArrangement(client, input)
		},
	)
}

type arrangementClip struct {
	Name       string
	Start, End float64
}

var errArrangementPatchMissing = &ActionableError{
	Code:     "arrangement_patch_missing",
	Message:  "this AbletonOSC install cannot read the Arrangement",
	NextStep: "Copy remote-script/abletonosc/browser.py into AbletonOSC again, then restart Live (or send /live/api/reload).",
}

// queryArrangementClips lists the clips of a track that overlap [from, to) beats.
func queryArrangementClips(c oscQuerier, track int, from, to float64) ([]arrangementClip, error) {
	res, err := c.Query("/live/track/get/arrangement_clips", int32(track), float32(from), float32(to))
	if err != nil {
		if strings.Contains(err.Error(), "no response received to query") {
			return nil, errArrangementPatchMissing
		}
		return nil, err
	}
	if len(res) < 2 {
		return nil, fmt.Errorf("unexpected arrangement reply: %v", res)
	}
	count, err := abletonosc.AsInt(res[1])
	if err != nil || len(res) < 2+3*count {
		return nil, fmt.Errorf("unexpected arrangement reply: %v", res)
	}
	clips := make([]arrangementClip, 0, count)
	for i := 0; i < count; i++ {
		start, err1 := abletonosc.AsFloat64(res[3+3*i])
		end, err2 := abletonosc.AsFloat64(res[4+3*i])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("unexpected arrangement reply: %v", res)
		}
		clips = append(clips, arrangementClip{Name: fmt.Sprint(res[2+3*i]), Start: start, End: end})
	}
	sort.Slice(clips, func(i, j int) bool { return clips[i].Start < clips[j].Start })
	return clips, nil
}

// sessionClipLengths reads the length of every Session clip in one reply:
// [track][scene], 0 where a slot is empty.
func sessionClipLengths(c oscQuerier, numTracks, numScenes int) ([][]float64, error) {
	res, err := c.Query("/live/song/get/track_data", int32(0), int32(numTracks), "clip.length")
	if err != nil {
		return nil, fmt.Errorf("read clip lengths: %w", err)
	}
	if len(res) != numTracks*numScenes {
		return nil, fmt.Errorf("unexpected clip length listing: %d values for %d tracks of %d scenes", len(res), numTracks, numScenes)
	}
	out := make([][]float64, numTracks)
	for t := range out {
		out[t] = make([]float64, numScenes)
		for s := range out[t] {
			if v := res[t*numScenes+s]; v != nil {
				if out[t][s], err = abletonosc.AsFloat64(v); err != nil {
					return nil, fmt.Errorf("clip length of track %d, scene %d: %w", t, s, err)
				}
			}
		}
	}
	return out, nil
}

type arrangementPlacement struct {
	track, scene int
	at           float64
}

func writeArrangement(c auditionClient, sleep auditionSleeper, input WriteArrangementInput) (WriteArrangementOutput, error) {
	if err := validateSongSections(input.Sections); err != nil {
		return WriteArrangementOutput{}, err
	}
	startBar := input.StartBar
	if startBar == 0 {
		startBar = 1
	}
	if startBar < 1 {
		return WriteArrangementOutput{}, invalidSongPlan("start_bar must be 1 or more, got %d", input.StartBar)
	}
	sections, err := resolveSongSections(c, input.Sections)
	if err != nil {
		return WriteArrangementOutput{}, err
	}
	tempo, err := queryAuditionTempo(c)
	if err != nil {
		return WriteArrangementOutput{}, err
	}
	beatsPerBar, err := queryAuditionBeatsPerBar(c)
	if err != nil {
		return WriteArrangementOutput{}, err
	}
	names, err := c.Query("/live/song/get/track_names")
	if err != nil {
		return WriteArrangementOutput{}, fmt.Errorf("get track names: %w", err)
	}
	trackNames := toStringSlice(names)
	numScenes, err := queryNumScenes(c)
	if err != nil {
		return WriteArrangementOutput{}, err
	}
	lengths, err := sessionClipLengths(c, len(trackNames), numScenes)
	if err != nil {
		return WriteArrangementOutput{}, err
	}

	// Work the whole song out before anything is touched.
	bar := float64(beatsPerBar)
	from := float64(startBar-1) * bar
	out := WriteArrangementOutput{StartBar: startBar, TempoBPM: tempo, SectionsTrackIndex: -1}
	var placements []arrangementPlacement
	expected := map[int][]arrangementClip{}
	at := from
	for _, section := range sections {
		beats := float64(section.Bars) * bar
		written := WrittenSection{Name: section.Name, SceneIndex: section.SceneIndex, StartBar: startBar + out.TotalBars, Bars: section.Bars}
		for track := range trackNames {
			length := lengths[track][section.SceneIndex]
			if length <= 0 || trackNames[track] == arrangementSectionsTrack {
				continue
			}
			copies := math.Round(beats / length)
			if copies < 1 || math.Abs(copies*length-beats) > arrangementBeatSlack {
				return WriteArrangementOutput{}, actionable("section_not_multiple_of_clip",
					fmt.Sprintf("section %q is %d bars, but the clip of track %d (%s) in scene %d is %s bars long and does not fill it a whole number of times",
						section.Name, section.Bars, track, clipLabel(c, track, section.SceneIndex), section.SceneIndex, trimFloat(length/bar)),
					"Change the section's bars to a multiple of that clip, or put a shorter version of the clip into a scene of its own. Nothing in Live was touched.")
			}
			for i := 0; i < int(copies); i++ {
				start := at + float64(i)*length
				placements = append(placements, arrangementPlacement{track, section.SceneIndex, start})
				expected[track] = append(expected[track], arrangementClip{Start: start, End: start + length})
			}
			written.ClipsPlaced += int(copies)
		}
		out.Sections = append(out.Sections, written)
		out.ClipsPlaced += written.ClipsPlaced
		out.TotalBars += section.Bars
		at += beats
	}
	to := at
	if len(placements) == 0 {
		return WriteArrangementOutput{}, actionable("song_target_missing",
			"none of the sections' scenes has a clip, so there is nothing to lay out",
			"Call ableton_get_sounding_snapshot to see which scenes hold clips. Nothing in Live was touched.")
	}
	for track := range expected {
		out.TracksWritten = append(out.TracksWritten, track)
	}
	sort.Ints(out.TracksWritten)
	out.EndBar = startBar + out.TotalBars - 1
	out.DurationSec = round2((to - from) * 60 / tempo)

	// What is in the way? The song owns its range on every track that holds
	// Session clips, not only on those this version uses: an earlier version may
	// have used others, and its clips must not play on under the new one. A track
	// without Session clips (a recorded vocal) is never the song's.
	sectionsTrack := -1
	var owned []int
	for track, name := range trackNames {
		if name == arrangementSectionsTrack {
			sectionsTrack = track
			owned = append(owned, track)
			continue
		}
		for _, length := range lengths[track] {
			if length > 0 {
				owned = append(owned, track)
				break
			}
		}
	}
	// A version of the song written here before may have been longer. The
	// Sections track tells how far it went: its markers follow one another from
	// the start bar. The song owns that stretch too, or the old ending would
	// play on behind a shorter rewrite (seen on a real Live).
	clearTo := to
	if sectionsTrack >= 0 {
		markers, err := queryArrangementClips(c, sectionsTrack, from, 1e9)
		if err != nil {
			return WriteArrangementOutput{}, err
		}
		// Followed from the marker that covers the start bar, which need not begin
		// on it: the song may be written again from a later bar than before.
		reach := from
		for _, marker := range markers {
			if marker.Start > reach+arrangementBeatSlack {
				break // a gap: what comes after belongs to something else
			}
			reach = math.Max(reach, marker.End)
		}
		clearTo = math.Max(to, reach)
	}

	var inTheWay, crossing []string
	blocked := 0
	for _, track := range owned {
		clips, err := queryArrangementClips(c, track, from, clearTo)
		if err != nil {
			return WriteArrangementOutput{}, err
		}
		blocked += len(clips)
		for _, clip := range clips {
			if clip.Start < from-arrangementBeatSlack || clip.End > clearTo+arrangementBeatSlack {
				crossing = append(crossing, fmt.Sprintf("%q on track %d (%s), bars %s-%s", clip.Name, track, trackNames[track], trimFloat(clip.Start/bar+1), trimFloat(clip.End/bar)))
			}
		}
		for _, span := range foldArrangementClips(clips, bar) { // copies in a row read as one entry
			copies := max(span.Repeats, 1)
			entry := fmt.Sprintf("%q on track %d (%s), bars %s-%s", span.Name, track, trackNames[track],
				trimFloat(span.StartBar), trimFloat(span.StartBar+span.Bars*float64(copies)-1))
			if copies > 1 {
				entry += fmt.Sprintf(" (%d clips)", copies)
			}
			inTheWay = append(inTheWay, entry)
		}
	}
	if len(crossing) > 0 {
		// Live 11 cannot cut an Arrangement clip, and deleting takes the whole clip.
		// One that lies across the song's first or last bar line can be neither kept
		// nor removed: whatever the new copies left uncovered would play on.
		return WriteArrangementOutput{}, actionable("arrangement_clip_crosses_range",
			fmt.Sprintf("the song would run from bar %s to bar %s, and these clips lie across one of those bar lines: %s",
				trimFloat(from/bar+1), trimFloat(clearTo/bar), strings.Join(crossing, "; ")),
			"Start the song where such a clip starts (start_bar) or after it ends, or have the person remove or split that clip in Live. overwrite cannot help: it deletes whole clips only. Nothing in Live was touched.")
	}
	if len(inTheWay) > 0 && !input.Overwrite {
		shown := inTheWay
		if len(shown) > 8 {
			shown = append(shown[:8:8], fmt.Sprintf("and %d more entries", len(inTheWay)-8))
		}
		return WriteArrangementOutput{}, actionable("arrangement_occupied",
			fmt.Sprintf("the Arrangement already has %d clips where the song would go: %s", blocked, strings.Join(shown, "; ")),
			"Ask the person whether those may be replaced, then pass overwrite=true; or write the song further along with start_bar. Nothing in Live was touched.")
	}

	// From here on Live changes. If the range was empty, everything in it afterwards is ours to take back.
	wasEmpty := len(inTheWay) == 0
	takeBack := func(cause error) (WriteArrangementOutput, error) {
		if wasEmpty {
			for _, track := range owned {
				_, _ = c.Query("/live/track/delete_arrangement_clips", int32(track), float32(from), float32(clearTo))
			}
			return WriteArrangementOutput{}, cause
		}
		return WriteArrangementOutput{}, fmt.Errorf("%w (the clips that overwrite deleted are gone; what was placed so far stays)", cause)
	}
	for _, track := range owned {
		if !input.Overwrite {
			break
		}
		res, err := c.Query("/live/track/delete_arrangement_clips", int32(track), float32(from), float32(clearTo))
		if err != nil {
			return takeBack(fmt.Errorf("clear track %d: %w", track, err))
		}
		if len(res) >= 3 {
			if deleted, err := abletonosc.AsInt(res[2]); err == nil {
				out.ClipsReplaced += deleted
			}
		}
	}

	for i, p := range placements {
		if err := c.Send("/live/clip_slot/duplicate_clip_to_arrangement", int32(p.track), int32(p.scene), float32(p.at)); err != nil {
			return takeBack(fmt.Errorf("copy the clip of track %d, scene %d to beat %v: %w", p.track, p.scene, p.at, err))
		}
		if (i+1)%arrangementBatch == 0 {
			sleep(arrangementBatchPause)
		}
	}

	if sectionsTrack < 0 {
		if sectionsTrack, err = createSectionsTrack(c, sleep, len(trackNames)); err != nil {
			return takeBack(err)
		}
		owned = append(owned, sectionsTrack)
	}
	out.SectionsTrackIndex = sectionsTrack
	if err := writeSectionMarkers(c, sectionsTrack, numScenes, sections, from, bar); err != nil {
		return takeBack(err)
	}
	at = from
	for _, section := range sections {
		beats := float64(section.Bars) * bar
		expected[sectionsTrack] = append(expected[sectionsTrack], arrangementClip{Name: section.Name, Start: at, End: at + beats})
		at += beats
	}

	// Believe the Arrangement, not the sends.
	for _, track := range owned {
		got, err := queryArrangementClips(c, track, from, clearTo)
		if err != nil {
			return takeBack(err)
		}
		if problem := compareArrangement(got, expected[track]); problem != "" {
			return takeBack(actionable("arrangement_not_as_planned",
				fmt.Sprintf("track %d does not hold what was planned: %s", track, problem),
				"Call ableton_get_arrangement to see what is there, then try again."))
		}
	}
	out.Verified = true
	out.Note = "A track that is playing a Session clip keeps playing it: the Arrangement sounds once Back to Arrangement is pressed in Live. The bounce records from the scenes, not from the Arrangement."
	return out, nil
}

// compareArrangement checks what was read back from the song's stretch against
// the plan. It has to be the plan and nothing else: no clip of an older version
// may be left in there.
func compareArrangement(got, want []arrangementClip) string {
	if len(got) != len(want) {
		return fmt.Sprintf("%d clips in the song's stretch, %d planned", len(got), len(want))
	}
	sort.Slice(want, func(i, j int) bool { return want[i].Start < want[j].Start })
	for i := range want {
		if math.Abs(got[i].Start-want[i].Start) > arrangementBeatSlack || math.Abs(got[i].End-want[i].End) > arrangementBeatSlack {
			return fmt.Sprintf("clip %d runs from beat %v to %v, planned %v to %v", i, got[i].Start, got[i].End, want[i].Start, want[i].End)
		}
		if want[i].Name != "" && got[i].Name != want[i].Name {
			return fmt.Sprintf("clip %d is named %q, planned %q", i, got[i].Name, want[i].Name)
		}
	}
	return ""
}

// createSectionsTrack appends the marker track. Live selects a track it has
// just created; the selection goes back to where the person had it.
func createSectionsTrack(c auditionClient, sleep auditionSleeper, tracksBefore int) (int, error) {
	selected, selectedErr := c.Query("/live/view/get/selected_track")
	if err := c.Send("/live/song/create_midi_track", int32(-1)); err != nil {
		return 0, fmt.Errorf("create the Sections track: %w", err)
	}
	count := tracksBefore
	for i := 0; i < 50 && count <= tracksBefore; i++ {
		var err error
		if count, err = queryNumTracks(c); err != nil {
			return 0, fmt.Errorf("create the Sections track: %w", err)
		}
		if count <= tracksBefore {
			sleep(auditionPollInterval)
		}
	}
	if count <= tracksBefore {
		return 0, fmt.Errorf("create the Sections track: Live did not add a track")
	}
	track := count - 1
	if err := c.Send("/live/track/set/name", int32(track), arrangementSectionsTrack); err != nil {
		return 0, fmt.Errorf("name the Sections track: %w", err)
	}
	if selectedErr == nil && len(selected) > 0 {
		if previous, err := abletonosc.AsInt(selected[0]); err == nil {
			_ = c.Send("/live/view/set/selected_track", int32(previous))
		}
	}
	return track, nil
}

// writeSectionMarkers puts one empty, named clip per section on the Sections
// track. The Arrangement only takes copies of Session clips, so each marker is
// made in a free slot, copied over, and removed from the slot again.
func writeSectionMarkers(c auditionClient, track, numScenes int, sections []SongSection, from, bar float64) error {
	slot := -1
	for s := 0; s < numScenes && slot < 0; s++ {
		has, err := queryBool(c, "/live/clip_slot/get/has_clip", int32(track), int32(s))
		if err != nil {
			return fmt.Errorf("look for a free slot on the Sections track: %w", err)
		}
		if !has {
			slot = s
		}
	}
	if slot < 0 {
		return fmt.Errorf("the Sections track has no free clip slot to build its markers in")
	}
	at := from
	for _, section := range sections {
		beats := float64(section.Bars) * bar
		for _, step := range []struct {
			address string
			args    []interface{}
		}{
			{"/live/clip_slot/create_clip", []interface{}{int32(track), int32(slot), float32(beats)}},
			{"/live/clip/set/name", []interface{}{int32(track), int32(slot), section.Name}},
			{"/live/clip_slot/duplicate_clip_to_arrangement", []interface{}{int32(track), int32(slot), float32(at)}},
			{"/live/clip_slot/delete_clip", []interface{}{int32(track), int32(slot)}},
		} {
			if err := c.Send(step.address, step.args...); err != nil {
				return fmt.Errorf("mark section %q: %w", section.Name, err)
			}
		}
		// Live works through its messages in order: once this answers, the slot is free again.
		if _, err := queryBool(c, "/live/clip_slot/get/has_clip", int32(track), int32(slot)); err != nil {
			return fmt.Errorf("mark section %q: %w", section.Name, err)
		}
		at += beats
	}
	return nil
}

func clipLabel(c oscQuerier, track, scene int) string {
	res, err := c.Query("/live/clip/get/name", int32(track), int32(scene))
	if err != nil || len(res) < 3 || strings.TrimSpace(fmt.Sprint(res[2])) == "" {
		return "unnamed clip"
	}
	return fmt.Sprintf("%q", fmt.Sprint(res[2]))
}

func trimFloat(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

func getArrangement(c auditionClient, input GetArrangementInput) (GetArrangementOutput, error) {
	if input.FromBar < 0 || input.ToBar < 0 || (input.ToBar > 0 && input.ToBar < max(input.FromBar, 1)) {
		return GetArrangementOutput{}, fmt.Errorf("from_bar and to_bar must be 1 or more, and to_bar not before from_bar")
	}
	tempo, err := queryAuditionTempo(c)
	if err != nil {
		return GetArrangementOutput{}, err
	}
	beatsPerBar, err := queryAuditionBeatsPerBar(c)
	if err != nil {
		return GetArrangementOutput{}, err
	}
	names, err := c.Query("/live/song/get/track_names")
	if err != nil {
		return GetArrangementOutput{}, fmt.Errorf("get track names: %w", err)
	}
	trackNames := toStringSlice(names)
	bar := float64(beatsPerBar)
	from, to := float64(max(input.FromBar, 1)-1)*bar, 1e9
	if input.ToBar > 0 {
		to = float64(input.ToBar) * bar
	}
	wanted := map[int]bool{}
	for _, track := range input.TrackIndices {
		if track < 0 || track >= len(trackNames) {
			return GetArrangementOutput{}, fmt.Errorf("track_indices: there is no track %d (the set has %d)", track, len(trackNames))
		}
		wanted[track] = true
	}

	out := GetArrangementOutput{TempoBPM: tempo, BeatsPerBar: beatsPerBar, Sections: []ArrangementSpan{}, Locators: []ArrangementLocator{}, Tracks: []ArrangementTrack{}}
	for track, name := range trackNames {
		if len(wanted) > 0 && !wanted[track] && name != arrangementSectionsTrack {
			continue
		}
		clips, err := queryArrangementClips(c, track, from, to)
		if err != nil {
			return GetArrangementOutput{}, err
		}
		if len(clips) == 0 {
			continue
		}
		spans := foldArrangementClips(clips, bar)
		out.EndBar = math.Max(out.EndBar, round3(clips[len(clips)-1].End/bar))
		if name == arrangementSectionsTrack {
			out.Sections = spans
			continue
		}
		out.Tracks = append(out.Tracks, ArrangementTrack{TrackIndex: track, Name: name, Clips: spans})
	}

	if res, err := c.Query("/live/song/get/cue_points"); err == nil {
		for i := 0; i+1 < len(res); i += 2 {
			beat, err := abletonosc.AsFloat64(res[i+1])
			if err != nil || beat < from || beat >= to {
				continue
			}
			out.Locators = append(out.Locators, ArrangementLocator{Name: fmt.Sprint(res[i]), Bar: round3(beat/bar + 1)})
		}
		sort.Slice(out.Locators, func(i, j int) bool { return out.Locators[i].Bar < out.Locators[j].Bar }) // Live lists them in the order they were made
	}
	return out, nil
}

// foldArrangementClips turns clips into spans in bars, with copies of the same
// clip that follow one another without a gap folded into one line.
func foldArrangementClips(clips []arrangementClip, bar float64) []ArrangementSpan {
	var spans []ArrangementSpan
	var lastEnd, lastLength float64
	for _, clip := range clips {
		length := clip.End - clip.Start
		if n := len(spans); n > 0 && spans[n-1].Name == clip.Name && math.Abs(clip.Start-lastEnd) < arrangementBeatSlack && math.Abs(length-lastLength) < arrangementBeatSlack {
			if spans[n-1].Repeats == 0 {
				spans[n-1].Repeats = 1
			}
			spans[n-1].Repeats++
		} else {
			spans = append(spans, ArrangementSpan{Name: clip.Name, StartBar: round3(clip.Start/bar + 1), Bars: round3(length / bar)})
		}
		lastEnd, lastLength = clip.End, length
	}
	return spans
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
