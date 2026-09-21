package tools

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	defaultAuditionBars = 8
	maxAuditionBars     = 32
	minAuditionVariants = 2
	maxAuditionVariants = 8
	maxAuditionPlays    = 16
	maxAuditionDeltaDB  = 24.0
	maxAuditionLabelLen = 8
	maxAuditionDescLen  = 60
)

type AuditionInput struct {
	Variants       []AuditionVariant `json:"variants" jsonschema:"description=2 to 8 variants. Make one of them X: a label and a description with no clips\\, mix or devices\\, so the current state is always heard next to the changes"`
	BarsPerVariant int               `json:"bars_per_variant,omitempty" jsonschema:"description=Bars each variant plays (default 8). The call lasts plays x bars in real time: five variants of 8 bars at 140 BPM take about 70 seconds,minimum=1,maximum=32"`
	Play           []string          `json:"play,omitempty" jsonschema:"description=Labels to play\\, in order (up to 16). Omit to play every variant once in the order given. Repeat labels to alternate\\, e.g. J K J K"`
	Commit         string            `json:"commit,omitempty" jsonschema:"description=Label to write into the set instead of playing anything. Only after the listener has chosen it"`
	StopAfter      bool              `json:"stop_after,omitempty" jsonschema:"description=Stop playback when done"`
}

type AuditionVariant struct {
	Label       string           `json:"label" jsonschema:"description=Short name the listener answers with\\, e.g. X or A or B (1-8 characters)"`
	Description string           `json:"description" jsonschema:"description=The one thing this variant changes\\, in the listener's language (1-60 characters). Shown in Live while it plays"`
	Clips       []AuditionClip   `json:"clips,omitempty" jsonschema:"description=Clips that play during this variant instead of what the track plays now"`
	Mix         []AuditionMix    `json:"mix,omitempty" jsonschema:"description=Track volume changes\\, relative to the current level"`
	Devices     []AuditionDevice `json:"devices,omitempty" jsonschema:"description=Devices switched on or off during this variant. Switching an instrument off silences its track"`
}

type AuditionClip struct {
	TrackIndex int `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int `json:"clip_index" jsonschema:"description=Clip slot on this track. The clip must exist already,minimum=0"`
}

type AuditionMix struct {
	TrackIndex int     `json:"track_index" jsonschema:"minimum=0"`
	DeltaDB    float64 `json:"delta_db" jsonschema:"description=Change in dB from the current level,minimum=-24,maximum=24"`
}

type AuditionDevice struct {
	TrackIndex  int  `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int  `json:"device_index" jsonschema:"minimum=0"`
	Active      bool `json:"active" jsonschema:"description=true switches the device on\\, false bypasses it"`
}

type AuditionPlayed struct {
	Label       string  `json:"label"`
	Description string  `json:"description"`
	StartBar    int     `json:"start_bar"`
	StartSec    float64 `json:"start_sec"` // from the start of the first variant
}

type AuditionCommittedMix struct {
	TrackIndex int    `json:"track_index"`
	VolumeDB   string `json:"volume_db"`
}

type AuditionCommit struct {
	Label   string                 `json:"label"`
	Clips   []AuditionClip         `json:"clips,omitempty"`
	Mix     []AuditionCommittedMix `json:"mix,omitempty"`
	Devices []AuditionDevice       `json:"devices,omitempty"`
}

type AuditionOutput struct {
	Played          []AuditionPlayed `json:"played"`
	BarsPerVariant  int              `json:"bars_per_variant"`
	TempoBPM        float64          `json:"tempo_bpm"`
	DurationSec     float64          `json:"duration_sec"`
	PlaybackStarted bool             `json:"playback_started,omitempty"`
	Restored        bool             `json:"restored"`
	Committed       *AuditionCommit  `json:"committed,omitempty"`
	Prompt          string           `json:"prompt,omitempty"`
}

// auditionOrder is the validated shape of a request.
type auditionOrder struct {
	bars   int
	play   []int // indices into Variants, in the order they sound
	commit int   // index of the variant to write in, or -1
}

func invalidAudition(format string, args ...interface{}) error {
	return actionable("invalid_audition", fmt.Sprintf(format, args...),
		"Fix that and call again. Nothing in Live was touched.")
}

func NewAbletonAudition(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_audition",
		"Ableton Live: play 2-8 labelled variants back to back, switching on bar lines, so the listener can answer with one letter. Each variant is the current state plus its own changes — clips to play instead, track volume changes in dB, devices switched on or off — and a variant with no changes (X) keeps the current state in the line-up. Runs in real time and blocks until the end: one pass is plays × bars_per_variant bars, after up to a bar of waiting. It starts playback if it is stopped, sets clip launch quantization to 1 bar for the duration, and adds one audio track named 'Audition' at the end of the set whose name shows which variant is sounding. Faders, devices, playing clips and quantization are put back when it ends, when it fails, and when the listener stops playback. It plays what exists: write variant clips first (e.g. ableton_clip_write). Use when the listener is to choose between alternatives by ear; change one thing per variant. Pass commit only after the listener has named their choice: it plays nothing, writes that variant into the set and puts nothing back.",
		func(_ *ai.ToolContext, input AuditionInput) (AuditionOutput, error) {
			return runAudition(client, time.Sleep, input)
		},
	)
}

// validateAuditionShape checks everything that can be checked without Live.
func validateAuditionShape(input AuditionInput) (auditionOrder, error) {
	order := auditionOrder{bars: input.BarsPerVariant, commit: -1}
	if order.bars == 0 {
		order.bars = defaultAuditionBars
	}
	if order.bars < 1 || order.bars > maxAuditionBars {
		return order, invalidAudition("bars_per_variant must be 1 to %d, got %d", maxAuditionBars, input.BarsPerVariant)
	}
	if n := len(input.Variants); n < minAuditionVariants || n > maxAuditionVariants {
		return order, invalidAudition("an audition takes %d to %d variants, got %d", minAuditionVariants, maxAuditionVariants, n)
	}

	byLabel := make(map[string]int, len(input.Variants))
	for i, v := range input.Variants {
		label := strings.TrimSpace(v.Label)
		if n := utf8.RuneCountInString(label); n < 1 || n > maxAuditionLabelLen {
			return order, invalidAudition("variant %d: label must be 1 to %d characters", i, maxAuditionLabelLen)
		}
		key := strings.ToLower(label)
		if _, dup := byLabel[key]; dup {
			return order, invalidAudition("label %q is used twice", label)
		}
		byLabel[key] = i
		if n := utf8.RuneCountInString(strings.TrimSpace(v.Description)); n < 1 || n > maxAuditionDescLen {
			return order, invalidAudition("variant %s: description must be 1 to %d characters", label, maxAuditionDescLen)
		}
		if err := validateVariantTargets(label, v); err != nil {
			return order, err
		}
	}

	find := func(field, label string) (int, error) {
		i, ok := byLabel[strings.ToLower(strings.TrimSpace(label))]
		if !ok {
			return 0, invalidAudition("%s names %q, which is not one of the variants", field, label)
		}
		return i, nil
	}
	if input.Commit != "" {
		if len(input.Play) > 0 {
			return order, invalidAudition("give play or commit, not both: commit plays nothing")
		}
		i, err := find("commit", input.Commit)
		if err != nil {
			return order, err
		}
		order.commit = i
		return order, nil
	}
	if len(input.Play) > maxAuditionPlays {
		return order, invalidAudition("play takes up to %d labels, got %d", maxAuditionPlays, len(input.Play))
	}
	for _, label := range input.Play {
		i, err := find("play", label)
		if err != nil {
			return order, err
		}
		order.play = append(order.play, i)
	}
	if len(order.play) == 0 {
		for i := range input.Variants {
			order.play = append(order.play, i)
		}
	}
	return order, nil
}

func validateVariantTargets(label string, v AuditionVariant) error {
	clipTracks := map[int]bool{}
	for _, c := range v.Clips {
		if c.TrackIndex < 0 || c.ClipIndex < 0 {
			return invalidAudition("variant %s: track_index and clip_index must be 0 or more", label)
		}
		if clipTracks[c.TrackIndex] {
			return invalidAudition("variant %s names two clips for track %d; a track plays one clip at a time", label, c.TrackIndex)
		}
		clipTracks[c.TrackIndex] = true
	}
	mixTracks := map[int]bool{}
	for _, m := range v.Mix {
		if m.TrackIndex < 0 {
			return invalidAudition("variant %s: track_index must be 0 or more", label)
		}
		if math.IsNaN(m.DeltaDB) || math.Abs(m.DeltaDB) > maxAuditionDeltaDB {
			return invalidAudition("variant %s: delta_db must be within ±%.0f dB, got %v", label, maxAuditionDeltaDB, m.DeltaDB)
		}
		if mixTracks[m.TrackIndex] {
			return invalidAudition("variant %s changes the volume of track %d twice", label, m.TrackIndex)
		}
		mixTracks[m.TrackIndex] = true
	}
	devices := map[[2]int]bool{}
	for _, d := range v.Devices {
		if d.TrackIndex < 0 || d.DeviceIndex < 0 {
			return invalidAudition("variant %s: track_index and device_index must be 0 or more", label)
		}
		key := [2]int{d.TrackIndex, d.DeviceIndex}
		if devices[key] {
			return invalidAudition("variant %s switches device %d of track %d twice", label, d.DeviceIndex, d.TrackIndex)
		}
		devices[key] = true
	}
	return nil
}
