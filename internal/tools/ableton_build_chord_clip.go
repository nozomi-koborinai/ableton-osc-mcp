package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	defaultBeatsPerChord = 4.0
	defaultChordOctave   = 4 // C4 = MIDI 60 (standard); C3 in Ableton's display
	defaultChordVelocity = 90
	maxChordSlots        = 64
)

// chordClipClient is the subset of the OSC client used to build a chord clip.
type chordClipClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

type BuildChordClipInput struct {
	TrackIndex    int      `json:"track_index" jsonschema:"description=Existing MIDI track index to write into,minimum=0"`
	ClipIndex     int      `json:"clip_index" jsonschema:"description=Empty clip slot index to fill,minimum=0"`
	Progression   string   `json:"progression" jsonschema:"description=Chord names separated by space/comma/pipe (e.g. 'C G Am F' or 'C | G | Am | F'). Supports m, dim, aug, 7, maj7, m7, sus2, sus4, 5. Use N.C. or - for a rest."`
	BeatsPerChord *float64 `json:"beats_per_chord,omitempty" jsonschema:"description=Beats each chord lasts (default 4 = one bar in 4/4),minimum=0.25,maximum=16"`
	RootOctave    *int     `json:"root_octave,omitempty" jsonschema:"description=Octave of the chord roots (default 4; C4=MIDI 60),minimum=0,maximum=8"`
	Velocity      *int     `json:"velocity,omitempty" jsonschema:"description=Note velocity (default 90),minimum=1,maximum=127"`
	Tempo         *float64 `json:"tempo,omitempty" jsonschema:"description=Optional project tempo (BPM) to set before building,minimum=20,maximum=999"`
	Fire          bool     `json:"fire,omitempty" jsonschema:"description=Fire the clip after building"`
}

type BuildChordClipOutput struct {
	TrackIndex  int      `json:"track_index"`
	ClipIndex   int      `json:"clip_index"`
	Chords      []string `json:"chords"`
	LengthBeats float64  `json:"length_beats"`
	NotesAdded  int      `json:"notes_added"`
	TempoSet    float64  `json:"tempo_set,omitempty"`
	Fired       bool     `json:"fired"`
	Note        string   `json:"note"`
}

func NewAbletonBuildChordClip(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_build_chord_clip",
		"Ableton Live: write a MIDI chord-progression clip into an existing MIDI track from a chord string (e.g. from ableton_analyze_audio_url's chord_summary). Creates the clip, adds block chords, and optionally fires it. A starting-point sketch, not a finished arrangement.",
		func(_ *ai.ToolContext, input BuildChordClipInput) (BuildChordClipOutput, error) {
			return buildChordClip(client, input)
		},
	)
}

func buildChordClip(client chordClipClient, input BuildChordClipInput) (BuildChordClipOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
		return BuildChordClipOutput{}, err
	}

	chords, err := parseProgression(input.Progression)
	if err != nil {
		return BuildChordClipOutput{}, err
	}

	beatsPerChord := defaultBeatsPerChord
	if input.BeatsPerChord != nil {
		beatsPerChord = *input.BeatsPerChord
	}
	if beatsPerChord < 0.25 || beatsPerChord > 16 {
		return BuildChordClipOutput{}, errors.New("beats_per_chord must be between 0.25 and 16")
	}

	octave := defaultChordOctave
	if input.RootOctave != nil {
		octave = *input.RootOctave
	}
	if octave < 0 || octave > 8 {
		return BuildChordClipOutput{}, errors.New("root_octave must be between 0 and 8")
	}

	velocity := defaultChordVelocity
	if input.Velocity != nil {
		velocity = *input.Velocity
	}
	if velocity < 1 || velocity > 127 {
		return BuildChordClipOutput{}, errors.New("velocity must be between 1 and 127")
	}

	notes, names := chordProgressionNotes(chords, beatsPerChord, octave, velocity)
	if len(notes) == 0 {
		return BuildChordClipOutput{}, errors.New("progression produced no playable notes")
	}
	lengthBeats := beatsPerChord * float64(len(chords))

	tempoSet := 0.0
	if input.Tempo != nil {
		if *input.Tempo < 20 || *input.Tempo > 999 {
			return BuildChordClipOutput{}, errors.New("tempo must be between 20 and 999")
		}
		if err := client.Send("/live/song/set/tempo", float32(*input.Tempo)); err != nil {
			return BuildChordClipOutput{}, fmt.Errorf("set tempo: %w", err)
		}
		tempoSet = *input.Tempo
	}

	if err := client.Send("/live/clip_slot/create_clip",
		int32(input.TrackIndex), int32(input.ClipIndex), float32(lengthBeats),
	); err != nil {
		return BuildChordClipOutput{}, fmt.Errorf("create clip: %w", err)
	}
	hasClipRes, err := client.Query("/live/clip_slot/get/has_clip", int32(input.TrackIndex), int32(input.ClipIndex))
	if err != nil {
		return BuildChordClipOutput{}, fmt.Errorf("verify clip: %w", err)
	}
	if err := ensureResponseLen(hasClipRes, 3); err != nil {
		return BuildChordClipOutput{}, fmt.Errorf("verify clip: %w", err)
	}
	hasClip, err := abletonosc.AsBool(hasClipRes[2])
	if err != nil {
		return BuildChordClipOutput{}, fmt.Errorf("verify clip: %w", err)
	}
	if !hasClip {
		return BuildChordClipOutput{}, errors.New("clip was not created (is the slot empty and the track a MIDI track?)")
	}

	if err := client.Send("/live/clip/set/name", int32(input.TrackIndex), int32(input.ClipIndex), input.Progression); err != nil {
		return BuildChordClipOutput{}, fmt.Errorf("set clip name: %w", err)
	}

	noteArgs := []interface{}{int32(input.TrackIndex), int32(input.ClipIndex)}
	for _, n := range notes {
		noteArgs = append(noteArgs,
			int32(n.Pitch), float32(n.StartTime), float32(n.Duration), int32(n.Velocity), false,
		)
	}
	if err := client.Send("/live/clip/add/notes", noteArgs...); err != nil {
		return BuildChordClipOutput{}, fmt.Errorf("add notes: %w", err)
	}

	fired := false
	if input.Fire {
		if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
			return BuildChordClipOutput{}, fmt.Errorf("fire clip: %w", err)
		}
		fired = true
	}

	return BuildChordClipOutput{
		TrackIndex:  input.TrackIndex,
		ClipIndex:   input.ClipIndex,
		Chords:      names,
		LengthBeats: lengthBeats,
		NotesAdded:  len(notes),
		TempoSet:    tempoSet,
		Fired:       fired,
		Note:        "Block-chord sketch (root-position triads/sevenths). Use it as a harmonic starting point, then voice-lead and arrange to taste.",
	}, nil
}

// parsedChord is a single chord in a progression.
type parsedChord struct {
	name      string
	rootPC    int
	intervals []int
	rest      bool
}

// chordProgressionNotes lays chords out end to end and returns the MIDI notes
// plus the resolved chord names (in order).
func chordProgressionNotes(chords []parsedChord, beatsPerChord float64, octave, velocity int) ([]MidiNote, []string) {
	var notes []MidiNote
	names := make([]string, 0, len(chords))
	for i, ch := range chords {
		names = append(names, ch.name)
		if ch.rest {
			continue
		}
		start := float64(i) * beatsPerChord
		rootMidi := ch.rootPC + 12*(octave+1)
		for _, iv := range ch.intervals {
			pitch := rootMidi + iv
			if pitch < 0 || pitch > 127 {
				continue
			}
			notes = append(notes, MidiNote{
				Pitch:     pitch,
				StartTime: start,
				Duration:  beatsPerChord,
				Velocity:  velocity,
			})
		}
	}
	return notes, names
}

func parseProgression(s string) ([]parsedChord, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '|' || r == '\t' || r == '\n'
	})
	if len(fields) == 0 {
		return nil, errors.New("progression is empty")
	}
	if len(fields) > maxChordSlots {
		return nil, fmt.Errorf("too many chords (%d); max is %d", len(fields), maxChordSlots)
	}
	chords := make([]parsedChord, 0, len(fields))
	for _, f := range fields {
		ch, err := parseChordSymbol(f)
		if err != nil {
			return nil, err
		}
		chords = append(chords, ch)
	}
	return chords, nil
}

var chordQualityIntervals = map[string][]int{
	"":     {0, 4, 7},
	"maj":  {0, 4, 7},
	"M":    {0, 4, 7},
	"m":    {0, 3, 7},
	"min":  {0, 3, 7},
	"-":    {0, 3, 7},
	"dim":  {0, 3, 6},
	"aug":  {0, 4, 8},
	"7":    {0, 4, 7, 10},
	"maj7": {0, 4, 7, 11},
	"M7":   {0, 4, 7, 11},
	"m7":   {0, 3, 7, 10},
	"min7": {0, 3, 7, 10},
	"sus2": {0, 2, 7},
	"sus4": {0, 5, 7},
	"5":    {0, 7},
}

var noteLetterPC = map[byte]int{
	'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11,
}

func parseChordSymbol(sym string) (parsedChord, error) {
	trimmed := strings.TrimSpace(sym)
	if trimmed == "" {
		return parsedChord{}, errors.New("empty chord symbol")
	}
	switch strings.ToUpper(trimmed) {
	case "N.C.", "NC", "N", "R", "REST", "-", "_":
		return parsedChord{name: "N.C.", rest: true}, nil
	}

	letter := trimmed[0]
	pc, ok := noteLetterPC[letter&^0x20] // uppercase the letter
	if !ok {
		return parsedChord{}, fmt.Errorf("invalid chord root %q", sym)
	}
	idx := 1
	name := strings.ToUpper(string(letter))
	if idx < len(trimmed) {
		switch trimmed[idx] {
		case '#':
			pc = (pc + 1) % 12
			name += "#"
			idx++
		case 'b':
			pc = (pc + 11) % 12
			name += "b"
			idx++
		}
	}

	quality := trimmed[idx:]
	intervals, ok := chordQualityIntervals[quality]
	if !ok {
		return parsedChord{}, fmt.Errorf("unsupported chord quality %q in %q (try m, dim, aug, 7, maj7, m7, sus2, sus4, 5)", quality, sym)
	}
	return parsedChord{
		name:      name + quality,
		rootPC:    pc,
		intervals: intervals,
	}, nil
}
