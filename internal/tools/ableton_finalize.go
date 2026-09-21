package tools

import (
	"errors"
	"math"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

type FinalizeAudioInput struct {
	Path        string   `json:"path" jsonschema:"description=Absolute path of the recording to finalize (.wav\\, .aif or .aiff)\\, e.g. a file_paths entry of ableton_bounce_session_pass"`
	OutputPath  string   `json:"output_path,omitempty" jsonschema:"description=Absolute .wav path to write. Default: next to the source\\, <name>_44k1_24b.wav"`
	SampleRate  int      `json:"sample_rate,omitempty" jsonschema:"description=44100 (default) or 48000"`
	BitDepth    int      `json:"bit_depth,omitempty" jsonschema:"description=24 (default) or 16 (dithered)"`
	CeilingDBTP *float64 `json:"ceiling_dbtp,omitempty" jsonschema:"description=True-peak ceiling in dBTP (default -1.0),minimum=-6,maximum=0"`
	MaxGainDB   *float64 `json:"max_gain_db,omitempty" jsonschema:"description=How far a quiet recording may be lifted towards the ceiling (default 6 dB). Gain only\\, never limiting,minimum=0,maximum=12"`
	TrimTail    *bool    `json:"trim_tail,omitempty" jsonschema:"description=Cut the silence after the last sound\\, keeping 0.2 s (default true)"`
	FadeOutSec  *float64 `json:"fade_out_sec,omitempty" jsonschema:"description=Length of the fade-out at the end (default 1.5 s\\, 0 for none),minimum=0,maximum=10"`
	Overwrite   bool     `json:"overwrite,omitempty" jsonschema:"description=Replace the output file if it exists. The source file is never replaced"`
}

type FinalizeLevelsOutput struct {
	TruePeakDBTP   float64 `json:"true_peak_dbtp"`
	LUFSIntegrated float64 `json:"lufs_integrated"`
	SamplePeakDBFS float64 `json:"sample_peak_dbfs"`
	SampleRate     int     `json:"sample_rate"`
	DurationSec    float64 `json:"duration_sec"`
}

type FinalizeIssueOutput struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Blocking bool   `json:"blocking" jsonschema:"description=true: do not deliver this file as it is"`
}

type FinalizeAudioOutput struct {
	OutputPath  string                `json:"output_path"`
	SampleRate  int                   `json:"sample_rate"`
	BitDepth    int                   `json:"bit_depth"`
	Channels    int                   `json:"channels"`
	DurationSec float64               `json:"duration_sec"`
	GainDB      float64               `json:"gain_db"`
	TrimmedSec  float64               `json:"trimmed_sec" jsonschema:"description=Silence cut from the end"`
	Source      FinalizeLevelsOutput  `json:"source"`
	Delivered   FinalizeLevelsOutput  `json:"delivered" jsonschema:"description=Measured from the written file"`
	Issues      []FinalizeIssueOutput `json:"issues"`
	OK          bool                  `json:"ok" jsonschema:"description=false when a blocking issue was found"`
}

func NewAbletonFinalizeAudio(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_finalize_audio",
		"Turn a recorded .wav/.aif (such as a bounce) into a delivery file: cuts the silence after the last sound, takes out DC, fades out, converts to 44.1 kHz (or 48), sets the level against a true-peak ceiling (default -1 dBTP) with gain alone — no limiting, and a quiet recording is lifted by max_gain_db at most — and writes a 24-bit (or dithered 16-bit) WAV next to the source. It writes one new file on disk, never changes the source, and refuses to replace an existing delivery unless overwrite is set. Reports what the written file measures (true peak, LUFS, duration) and `issues`; a clipped source, an ending that is cut off or a true peak over the ceiling give ok=false, and such a file is not ready to hand in. Works without Live. Not for analysis (ableton_analyze_local_audio), and not a way to make a mix louder.",
		func(_ *ai.ToolContext, input FinalizeAudioInput) (FinalizeAudioOutput, error) {
			return finalizeAudio(input)
		},
	)
}

func finalizeAudio(input FinalizeAudioInput) (FinalizeAudioOutput, error) {
	opts := audioanalyze.DefaultFinalizeOptions()
	opts.OutputPath, opts.Overwrite = input.OutputPath, input.Overwrite
	if input.SampleRate != 0 {
		opts.SampleRate = input.SampleRate
	}
	if input.BitDepth != 0 {
		opts.BitDepth = input.BitDepth
	}
	if input.CeilingDBTP != nil {
		opts.CeilingDBTP = *input.CeilingDBTP
	}
	if input.MaxGainDB != nil {
		opts.MaxGainDB = *input.MaxGainDB
	}
	if input.TrimTail != nil {
		opts.TrimTail = *input.TrimTail
	}
	if input.FadeOutSec != nil {
		opts.FadeOutSec = *input.FadeOutSec
	}

	got, err := audioanalyze.Finalize(input.Path, opts)
	if errors.Is(err, audioanalyze.ErrOutputExists) {
		return FinalizeAudioOutput{}, actionable("output_exists", err.Error(),
			"Pass another output_path, or overwrite=true once the person has agreed to replace that file.")
	}
	if err != nil {
		return FinalizeAudioOutput{}, wrapUnsupportedFormat(err)
	}

	out := FinalizeAudioOutput{
		OutputPath: got.OutputPath, SampleRate: got.SampleRate, BitDepth: got.BitDepth, Channels: got.Channels,
		DurationSec: round2(got.DurationSec), GainDB: round2(got.GainDB), TrimmedSec: round2(got.TrimmedSec),
		Source: finalizeLevels(got.Source), Delivered: finalizeLevels(got.Delivered),
		Issues: []FinalizeIssueOutput{}, OK: got.OK,
	}
	for _, issue := range got.Issues {
		out.Issues = append(out.Issues, FinalizeIssueOutput{Code: issue.Code, Message: issue.Message, Blocking: issue.Blocking})
	}
	return out, nil
}

func finalizeLevels(l audioanalyze.AudioLevels) FinalizeLevelsOutput {
	return FinalizeLevelsOutput{
		TruePeakDBTP: round2(l.TruePeakDBTP), LUFSIntegrated: round2(l.LUFSIntegrated), SamplePeakDBFS: round2(l.SamplePeakDBFS),
		SampleRate: l.SampleRate, DurationSec: round2(l.DurationSec),
	}
}

// round2 keeps two decimals and never prints a negative zero.
func round2(v float64) float64 {
	if r := math.Round(v*100) / 100; r != 0 {
		return r
	}
	return 0
}
