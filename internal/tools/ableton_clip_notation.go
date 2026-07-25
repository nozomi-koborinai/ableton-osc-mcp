package tools

import (
	"fmt"
	"math"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/notation"
)

// clipNotationClient is the slice of the OSC client these tools need, kept narrow
// so tests can stand in for Live.
type clipNotationClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

type ClipReadInput struct {
	TrackIndex int `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"description=Clip slot index (0-based; same row as the scene),minimum=0"`
}

type ClipReadOutput struct {
	TrackIndex int    `json:"track_index"`
	ClipIndex  int    `json:"clip_index"`
	Notation   string `json:"notation"`
	Rev        string `json:"rev"`
}

func NewAbletonClipRead(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_clip_read",
		"Ableton Live: read a MIDI clip as clip notation text plus a rev fingerprint. The notation carries every note in the clip (position, pitch, length, velocity, mute) and is the only way to inspect or change note content. Pass the rev back to ableton_clip_write.",
		func(_ *ai.ToolContext, input ClipReadInput) (ClipReadOutput, error) {
			return readClipNotation(client, input)
		},
	)
}

func readClipNotation(client clipNotationClient, input ClipReadInput) (ClipReadOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
		return ClipReadOutput{}, err
	}
	track, clip := int32(input.TrackIndex), int32(input.ClipIndex)

	hasClip, err := queryBool(client, "/live/clip_slot/get/has_clip", track, clip)
	if err != nil {
		return ClipReadOutput{}, err
	}
	if !hasClip {
		return ClipReadOutput{}, actionable("clip_not_found",
			fmt.Sprintf("no clip in slot [%d,%d]", input.TrackIndex, input.ClipIndex),
			"Pick a slot that holds a clip, or create one with ableton_clip_write using an empty rev.")
	}

	isAudio, err := queryBool(client, "/live/clip/get/is_audio_clip", track, clip)
	if err != nil {
		return ClipReadOutput{}, err
	}
	if isAudio {
		return ClipReadOutput{}, actionable("clip_is_audio",
			fmt.Sprintf("clip [%d,%d] is an audio clip and holds no notes", input.TrackIndex, input.ClipIndex),
			"Clip notation covers MIDI notes only. Use the audio clip tools for pitch, warp and region.")
	}

	clipData, err := loadClipForNotation(client, input.TrackIndex, input.ClipIndex)
	if err != nil {
		return ClipReadOutput{}, err
	}
	text, err := notation.Format(clipData)
	if err != nil {
		return ClipReadOutput{}, err
	}
	return ClipReadOutput{
		TrackIndex: input.TrackIndex,
		ClipIndex:  input.ClipIndex,
		Notation:   text,
		Rev:        notation.Rev(text),
	}, nil
}

// loadClipForNotation gathers everything the notation header and note lines need.
func loadClipForNotation(client clipNotationClient, trackIndex, clipIndex int) (notation.Clip, error) {
	track, clip := int32(trackIndex), int32(clipIndex)

	nameRes, err := client.Query("/live/clip/get/name", track, clip)
	if err != nil {
		return notation.Clip{}, err
	}
	if err := ensureResponseLen(nameRes, 3); err != nil {
		return notation.Clip{}, err
	}
	name := fmt.Sprint(nameRes[2])

	lengthRes, err := client.Query("/live/clip/get/length", track, clip)
	if err != nil {
		return notation.Clip{}, err
	}
	if err := ensureResponseLen(lengthRes, 3); err != nil {
		return notation.Clip{}, err
	}
	length, err := abletonosc.AsFloat64(lengthRes[2])
	if err != nil {
		return notation.Clip{}, err
	}

	sigNum, err := querySignaturePart(client, "/live/song/get/signature_numerator")
	if err != nil {
		return notation.Clip{}, err
	}
	sigDen, err := querySignaturePart(client, "/live/song/get/signature_denominator")
	if err != nil {
		return notation.Clip{}, err
	}
	beatsPerBar, err := notation.BeatsPerBar(sigNum, sigDen)
	if err != nil {
		return notation.Clip{}, err
	}

	notesRes, err := client.Query("/live/clip/get/notes", track, clip)
	if err != nil {
		return notation.Clip{}, err
	}
	_, _, midiNotes, err := parseClipNotesResponse(notesRes)
	if err != nil {
		return notation.Clip{}, err
	}

	notes := make([]notation.Note, 0, len(midiNotes))
	for _, n := range midiNotes {
		notes = append(notes, notation.Note{
			Pitch:     n.Pitch,
			StartTime: n.StartTime,
			Duration:  n.Duration,
			Velocity:  n.Velocity,
			Mute:      n.Mute != nil && *n.Mute,
		})
	}

	return notation.Clip{
		Name:   name,
		Bars:   int(math.Round(length / beatsPerBar)),
		SigNum: sigNum,
		SigDen: sigDen,
		Notes:  notes,
	}, nil
}

func queryBool(client clipNotationClient, address string, args ...interface{}) (bool, error) {
	res, err := client.Query(address, args...)
	if err != nil {
		return false, err
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, err
	}
	return abletonosc.AsBool(res[2])
}

func querySignaturePart(client clipNotationClient, address string) (int, error) {
	res, err := client.Query(address)
	if err != nil {
		return 0, err
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, err
	}
	return abletonosc.AsInt(res[0])
}
