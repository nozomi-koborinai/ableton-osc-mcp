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

type ClipWriteInput struct {
	TrackIndex int    `json:"track_index" jsonschema:"description=Track index (0-based regular tracks),minimum=0"`
	ClipIndex  int    `json:"clip_index" jsonschema:"description=Clip slot index (0-based; same row as the scene),minimum=0"`
	Notation   string `json:"notation" jsonschema:"description=Full clip notation text. This replaces every note in the clip\\, so send the whole clip\\, not just the part you changed."`
	Rev        string `json:"rev" jsonschema:"description=The rev returned by ableton_clip_read for this clip. Leave empty only to create a clip in an empty slot."`
}

type ClipWriteOutput struct {
	TrackIndex   int                 `json:"track_index"`
	ClipIndex    int                 `json:"clip_index"`
	Rev          string              `json:"rev"`
	NotesWritten int                 `json:"notes_written"`
	Verified     bool                `json:"verified"`
	Mismatches   []notation.Mismatch `json:"mismatches,omitempty"`
	// Normalized lists notes that had to be shortened because another note of the
	// same pitch started before they ended. Live enforces this itself; doing it
	// here keeps what was asked for and what got stored the same thing.
	Normalized []notation.Adjustment `json:"normalized,omitempty"`
}

func NewAbletonClipWrite(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_clip_write",
		"Ableton Live: replace every note in a MIDI clip from clip notation text. Read the clip first with ableton_clip_read and pass its rev back; the write is refused if the clip changed in the meantime. Reads the clip again afterwards and reports whether it came back identical.",
		func(_ *ai.ToolContext, input ClipWriteInput) (ClipWriteOutput, error) {
			return writeClipNotation(client, input)
		},
	)
}

func writeClipNotation(client clipNotationClient, input ClipWriteInput) (ClipWriteOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
		return ClipWriteOutput{}, err
	}

	// Stage one: the notation has to be readable, and it has to describe a state
	// Live can hold. Both are settled before Live is touched at all.
	wanted, err := notation.Parse(input.Notation)
	if err != nil {
		return ClipWriteOutput{}, actionable("notation_parse_error", err.Error(),
			"Fix the notation and send it again. Nothing was changed in Live.")
	}
	normalizedNotes, normalized, err := notation.Normalize(wanted.Notes)
	if err != nil {
		return ClipWriteOutput{}, actionable("overlapping_notes", err.Error(),
			"Remove one of the two notes or move it, then send the notation again. Nothing was changed in Live.")
	}
	wanted.Notes = normalizedNotes

	// Stage two: the header's signature has to be the song's. Positions are counted
	// in bars and beats, so notation written against another signature puts every
	// note somewhere else — and the note comparison at the end would still pass,
	// because it compares notes rather than the text that described them.
	sigNum, err := querySignaturePart(client, "/live/song/get/signature_numerator")
	if err != nil {
		return ClipWriteOutput{}, err
	}
	sigDen, err := querySignaturePart(client, "/live/song/get/signature_denominator")
	if err != nil {
		return ClipWriteOutput{}, err
	}
	if wanted.SigNum != sigNum || wanted.SigDen != sigDen {
		return ClipWriteOutput{}, actionable("signature_mismatch",
			fmt.Sprintf("notation says sig=%d/%d but the song is %d/%d",
				wanted.SigNum, wanted.SigDen, sigNum, sigDen),
			"Read the clip with ableton_clip_read and keep the sig it reports.")
	}

	track, clip := int32(input.TrackIndex), int32(input.ClipIndex)
	hasClip, err := queryBool(client, "/live/clip_slot/get/has_clip", track, clip)
	if err != nil {
		return ClipWriteOutput{}, err
	}

	beatsPerBar, err := notation.BeatsPerBar(wanted.SigNum, wanted.SigDen)
	if err != nil {
		return ClipWriteOutput{}, err
	}

	if input.Rev == "" {
		// Creating: there is nothing to compare against, so stages three and four
		// do not apply. Refuse if a clip is already there rather than replacing it.
		if hasClip {
			return ClipWriteOutput{}, actionable("clip_exists",
				fmt.Sprintf("slot [%d,%d] already holds a clip", input.TrackIndex, input.ClipIndex),
				"Read the clip with ableton_clip_read to get its rev, then write with that rev.")
		}
		length := float64(wanted.Bars) * beatsPerBar
		if length <= 0 {
			return ClipWriteOutput{}, actionable("notation_parse_error",
				"bars must be at least 1 when creating a clip",
				"Set bars= in the header to the length you want.")
		}
		if err := client.Send("/live/clip_slot/create_clip", track, clip, float32(length)); err != nil {
			return ClipWriteOutput{}, err
		}
	} else {
		if !hasClip {
			return ClipWriteOutput{}, actionable("clip_not_found",
				fmt.Sprintf("no clip in slot [%d,%d]", input.TrackIndex, input.ClipIndex),
				"Send rev as an empty string to create the clip.")
		}
		// Stage three: refuse if the clip moved under us.
		current, err := loadClipForNotation(client, input.TrackIndex, input.ClipIndex)
		if err != nil {
			return ClipWriteOutput{}, err
		}
		currentText, err := notation.Format(current)
		if err != nil {
			return ClipWriteOutput{}, err
		}
		if got := notation.Rev(currentText); got != input.Rev {
			return ClipWriteOutput{}, actionable("rev_mismatch",
				fmt.Sprintf("clip [%d,%d] changed since it was read (rev %s, now %s)",
					input.TrackIndex, input.ClipIndex, input.Rev, got),
				"Read the clip again with ableton_clip_read and redo the edit on top of it.")
		}
		// Stage four: the header has to describe the clip that is actually there.
		if current.Bars != wanted.Bars {
			return ClipWriteOutput{}, actionable("bars_mismatch",
				fmt.Sprintf("notation says bars=%d but the clip is %d bars", wanted.Bars, current.Bars),
				"Read the clip again and keep the bars value it reports.")
		}
	}

	// Stage five: replace the notes wholesale, then the name.
	if err := client.Send("/live/clip/remove/notes", track, clip); err != nil {
		return ClipWriteOutput{}, err
	}
	if len(wanted.Notes) > 0 {
		args := []interface{}{track, clip}
		for _, n := range wanted.Notes {
			args = append(args, int32(n.Pitch), float32(n.StartTime), float32(n.Duration), int32(n.Velocity), n.Mute)
		}
		if err := client.Send("/live/clip/add/notes", args...); err != nil {
			return ClipWriteOutput{}, err
		}
	}
	if err := client.Send("/live/clip/set/name", track, clip, wanted.Name); err != nil {
		return ClipWriteOutput{}, err
	}

	// Read it back and check. This is what makes "the round trip closes" a claim
	// that gets tested on every single write rather than once at design time.
	after, err := loadClipForNotation(client, input.TrackIndex, input.ClipIndex)
	if err != nil {
		return ClipWriteOutput{}, err
	}
	afterText, err := notation.Format(after)
	if err != nil {
		return ClipWriteOutput{}, err
	}
	mismatches := notation.Diff(wanted.Notes, after.Notes)

	return ClipWriteOutput{
		TrackIndex:   input.TrackIndex,
		ClipIndex:    input.ClipIndex,
		Rev:          notation.Rev(afterText),
		NotesWritten: len(wanted.Notes),
		Verified:     len(mismatches) == 0,
		Mismatches:   mismatches,
		Normalized:   normalized,
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
