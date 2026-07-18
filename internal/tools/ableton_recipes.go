package tools

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	drumPitchKick  = 36 // C1
	drumPitchSnare = 38 // D1
	drumPitchHat   = 42 // F#1

	defaultDrumLengthBeats = 16.0
	defaultDrumPattern     = "basic_backbeat"
)

type SetupDrumTrackInput struct {
	KitName     string   `json:"kit_name,omitempty" jsonschema:"description=Browser kit name to load by search (e.g. Street Kit). Use this or root_name+item_name"`
	RootName    string   `json:"root_name,omitempty" jsonschema:"description=Browser root for path load (e.g. Drums)"`
	PathParts   []string `json:"path_parts,omitempty" jsonschema:"description=Optional folder path under root for path load"`
	ItemName    string   `json:"item_name,omitempty" jsonschema:"description=Loadable item name for path load"`
	TrackName   string   `json:"track_name,omitempty" jsonschema:"description=Optional track name (defaults to loaded kit name)"`
	ClipIndex   *int     `json:"clip_index,omitempty" jsonschema:"description=Clip slot index (default 0),minimum=0"`
	LengthBeats float64  `json:"length_beats,omitempty" jsonschema:"description=Clip length in beats (default 16 = 4 bars),minimum=1,maximum=128"`
	Pattern     string   `json:"pattern,omitempty" jsonschema:"description=Preset pattern: basic_backbeat, four_on_floor, or kick_only (default basic_backbeat)"`
	Fire        bool     `json:"fire,omitempty" jsonschema:"description=Fire the clip after setup"`
}

type SetupDrumTrackOutput struct {
	TrackIndex  int     `json:"track_index"`
	TrackName   string  `json:"track_name"`
	ClipIndex   int     `json:"clip_index"`
	Loaded      string  `json:"loaded"`
	Pattern     string  `json:"pattern"`
	LengthBeats float64 `json:"length_beats"`
	NotesAdded  int     `json:"notes_added"`
	Fired       bool    `json:"fired"`
}

type drumSetupClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
	QueryWithTimeout(timeout time.Duration, address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonSetupDrumTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_setup_drum_track",
		"Ableton Live: create a MIDI drum track, load a kit, and fill a clip with a preset pattern (requires browser patch)",
		func(_ *ai.ToolContext, input SetupDrumTrackInput) (SetupDrumTrackOutput, error) {
			return setupDrumTrack(client, input)
		},
	)
}

func setupDrumTrack(client drumSetupClient, input SetupDrumTrackInput) (SetupDrumTrackOutput, error) {
	kitName := strings.TrimSpace(input.KitName)
	rootName := strings.TrimSpace(input.RootName)
	itemName := strings.TrimSpace(input.ItemName)
	pathParts := make([]string, 0, len(input.PathParts))
	for _, part := range input.PathParts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return SetupDrumTrackOutput{}, errors.New("path_parts must not contain empty strings")
		}
		pathParts = append(pathParts, trimmed)
	}

	usePath := rootName != "" || itemName != "" || len(pathParts) > 0
	useName := kitName != ""
	if usePath == useName {
		return SetupDrumTrackOutput{}, errors.New("provide either kit_name or root_name+item_name (not both)")
	}
	if usePath && (rootName == "" || itemName == "") {
		return SetupDrumTrackOutput{}, errors.New("root_name and item_name are required for path load")
	}

	clipIndex := 0
	if input.ClipIndex != nil {
		clipIndex = *input.ClipIndex
		if clipIndex < 0 {
			return SetupDrumTrackOutput{}, errors.New("clip_index must be >= 0")
		}
	}

	lengthBeats := input.LengthBeats
	if lengthBeats == 0 {
		lengthBeats = defaultDrumLengthBeats
	}
	if lengthBeats < 1 || lengthBeats > 128 {
		return SetupDrumTrackOutput{}, errors.New("length_beats must be between 1 and 128")
	}

	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		pattern = defaultDrumPattern
	}
	notes, err := buildDrumPattern(pattern, lengthBeats)
	if err != nil {
		return SetupDrumTrackOutput{}, err
	}

	if err := client.Send("/live/song/create_midi_track", int32(-1)); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("create midi track: %w", err)
	}
	numTracksRes, err := client.Query("/live/song/get/num_tracks")
	if err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("get num tracks: %w", err)
	}
	if err := ensureResponseLen(numTracksRes, 1); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("get num tracks: %w", err)
	}
	numTracks, err := abletonosc.AsInt(numTracksRes[0])
	if err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("get num tracks: %w", err)
	}
	if numTracks < 1 {
		return SetupDrumTrackOutput{}, errors.New("no tracks available after create")
	}
	trackIndex := numTracks - 1

	var loaded string
	if useName {
		res, err := client.Query("/live/track/load/browser_item", int32(trackIndex), kitName)
		if err != nil {
			return SetupDrumTrackOutput{}, fmt.Errorf("load kit by name (abletonosc browser patch required): %w", err)
		}
		parsed, err := parseLoadBrowserItemResponse(res)
		if err != nil {
			return SetupDrumTrackOutput{}, err
		}
		loaded = parsed.ItemName
	} else {
		res, err := client.QueryWithTimeout(
			10*time.Second,
			"/live/browser/load_at_path",
			loadAtPathArgs(trackIndex, rootName, pathParts, itemName)...,
		)
		if err != nil {
			return SetupDrumTrackOutput{}, fmt.Errorf("load kit by path (abletonosc browser patch required): %w", err)
		}
		parsed, err := parseLoadAtPathResponse(res)
		if err != nil {
			return SetupDrumTrackOutput{}, err
		}
		loaded = parsed.Loaded
	}

	trackName := strings.TrimSpace(input.TrackName)
	if trackName == "" {
		trackName = loaded
	}
	if trackName == "" {
		trackName = "Drums"
	}
	if err := client.Send("/live/track/set/name", int32(trackIndex), trackName); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("set track name: %w", err)
	}

	if err := client.Send("/live/clip_slot/create_clip",
		int32(trackIndex),
		int32(clipIndex),
		float32(lengthBeats),
	); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("create clip: %w", err)
	}
	hasClipRes, err := client.Query("/live/clip_slot/get/has_clip", int32(trackIndex), int32(clipIndex))
	if err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("verify clip: %w", err)
	}
	if err := ensureResponseLen(hasClipRes, 3); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("verify clip: %w", err)
	}
	hasClip, err := abletonosc.AsBool(hasClipRes[2])
	if err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("verify clip: %w", err)
	}
	if !hasClip {
		return SetupDrumTrackOutput{}, errors.New("clip was not created")
	}

	if err := client.Send("/live/clip/set/name", int32(trackIndex), int32(clipIndex), pattern); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("set clip name: %w", err)
	}

	noteArgs := []interface{}{int32(trackIndex), int32(clipIndex)}
	for _, n := range notes {
		noteArgs = append(noteArgs,
			int32(n.Pitch),
			float32(n.StartTime),
			float32(n.Duration),
			int32(n.Velocity),
			false,
		)
	}
	if err := client.Send("/live/clip/add/notes", noteArgs...); err != nil {
		return SetupDrumTrackOutput{}, fmt.Errorf("add notes: %w", err)
	}

	fired := false
	if input.Fire {
		if err := client.Send("/live/clip_slot/fire", int32(trackIndex), int32(clipIndex)); err != nil {
			return SetupDrumTrackOutput{}, fmt.Errorf("fire clip: %w", err)
		}
		fired = true
	}

	return SetupDrumTrackOutput{
		TrackIndex:  trackIndex,
		TrackName:   trackName,
		ClipIndex:   clipIndex,
		Loaded:      loaded,
		Pattern:     pattern,
		LengthBeats: lengthBeats,
		NotesAdded:  len(notes),
		Fired:       fired,
	}, nil
}

func buildDrumPattern(pattern string, lengthBeats float64) ([]MidiNote, error) {
	bars := int(lengthBeats / 4)
	if bars < 1 {
		bars = 1
	}
	// Keep notes inside the clip even when length is not a multiple of 4.
	end := lengthBeats

	switch strings.ToLower(pattern) {
	case "basic_backbeat":
		return drumNotesForBars(bars, end, []float64{0, 2}, []float64{1, 3}, 0.5), nil
	case "four_on_floor":
		return drumNotesForBars(bars, end, []float64{0, 1, 2, 3}, []float64{1, 3}, 0.5), nil
	case "kick_only":
		return drumNotesForBars(bars, end, []float64{0, 1, 2, 3}, nil, 0), nil
	default:
		return nil, fmt.Errorf("unsupported pattern %q (use basic_backbeat, four_on_floor, or kick_only)", pattern)
	}
}

func drumNotesForBars(bars int, end float64, kickBeats, snareBeats []float64, hatStep float64) []MidiNote {
	notes := make([]MidiNote, 0, bars*32)
	for bar := 0; bar < bars; bar++ {
		base := float64(bar * 4)
		for _, beat := range kickBeats {
			start := base + beat
			if start >= end {
				continue
			}
			notes = append(notes, MidiNote{Pitch: drumPitchKick, StartTime: start, Duration: 0.25, Velocity: 110})
		}
		for _, beat := range snareBeats {
			start := base + beat
			if start >= end {
				continue
			}
			notes = append(notes, MidiNote{Pitch: drumPitchSnare, StartTime: start, Duration: 0.25, Velocity: 100})
		}
		if hatStep > 0 {
			for t := base; t < base+4 && t < end; t += hatStep {
				notes = append(notes, MidiNote{Pitch: drumPitchHat, StartTime: t, Duration: 0.125, Velocity: 80})
			}
		}
	}
	return notes
}
