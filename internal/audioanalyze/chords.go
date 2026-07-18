package audioanalyze

import (
	"math"
	"strings"
)

// Chord detection groups frame chromas into short segments and matches each
// against the 24 major/minor triad templates. It is an approximate reference
// only: dense mixes, extended chords, and inversions can shift the estimate, so
// each segment carries a confidence and low-confidence spans are marked "N.C.".

const (
	chordSegmentSec = 0.75 // window used to classify a single chord
	chordMinConf    = 0.30 // below this a segment is treated as no-chord
	maxChordSummary = 24   // cap chords listed in the summary string
)

// ChordSegment is one detected chord span, with timing relative to the analyzed
// audio.
type ChordSegment struct {
	StartSec    float64 `json:"start_sec"`
	DurationSec float64 `json:"duration_sec"`
	Chord       string  `json:"chord"`
	Confidence  float64 `json:"confidence"`
}

// estimateChords returns a merged chord progression and a compact summary
// string (e.g. "C | G | Am | F"). It returns ok=false when the audio is too
// short to segment.
func estimateChords(samples []float64, sampleRate int) ([]ChordSegment, string, bool) {
	frames := frameChromas(samples, sampleRate)
	if len(frames) == 0 || sampleRate <= 0 {
		return nil, "", false
	}
	frameSec := frameDurationSec(sampleRate)
	framesPerSegment := int(math.Round(chordSegmentSec / frameSec))
	if framesPerSegment < 1 {
		framesPerSegment = 1
	}

	var raw []ChordSegment
	for start := 0; start < len(frames); start += framesPerSegment {
		end := start + framesPerSegment
		if end > len(frames) {
			end = len(frames)
		}
		var acc [12]float64
		for _, f := range frames[start:end] {
			for pc := 0; pc < 12; pc++ {
				acc[pc] += f[pc]
			}
		}
		chord, conf := classifyChord(acc)
		raw = append(raw, ChordSegment{
			StartSec:    round2(float64(start) * frameSec),
			DurationSec: round2(float64(end-start) * frameSec),
			Chord:       chord,
			Confidence:  conf,
		})
	}
	if len(raw) == 0 {
		return nil, "", false
	}

	merged := mergeChordRuns(raw)
	return merged, chordSummary(merged), true
}

// classifyChord matches a chroma vector against 24 triad templates.
func classifyChord(chroma [12]float64) (string, float64) {
	var total float64
	for _, v := range chroma {
		total += v
	}
	if total <= 0 {
		return "N.C.", 0
	}

	bestCorr := math.Inf(-1)
	secondCorr := math.Inf(-1)
	bestName := "N.C."
	for root := 0; root < 12; root++ {
		for _, q := range chordQualities {
			tmpl := triadTemplate(root, q.intervals)
			corr := pearson(chroma[:], tmpl[:])
			if corr > bestCorr {
				secondCorr = bestCorr
				bestCorr = corr
				bestName = pitchClassNames[root] + q.suffix
			} else if corr > secondCorr {
				secondCorr = corr
			}
		}
	}

	conf := 0.0
	if !math.IsInf(secondCorr, -1) && bestCorr > 0 {
		conf = math.Max(0, math.Min(1, (bestCorr-secondCorr)/math.Max(bestCorr, 1e-9)))
	}
	conf = round2(conf)
	if bestCorr <= 0 || conf < chordMinConf {
		return "N.C.", conf
	}
	return bestName, conf
}

type chordQuality struct {
	suffix    string
	intervals [3]int
}

var chordQualities = []chordQuality{
	{suffix: "", intervals: [3]int{0, 4, 7}},  // major
	{suffix: "m", intervals: [3]int{0, 3, 7}}, // minor
}

func triadTemplate(root int, intervals [3]int) [12]float64 {
	var t [12]float64
	for _, iv := range intervals {
		t[(root+iv)%12] = 1
	}
	return t
}

// mergeChordRuns collapses consecutive segments with the same chord into a
// single span and keeps the strongest confidence seen in the run.
func mergeChordRuns(segs []ChordSegment) []ChordSegment {
	var out []ChordSegment
	for _, s := range segs {
		if len(out) > 0 && out[len(out)-1].Chord == s.Chord {
			last := &out[len(out)-1]
			last.DurationSec = round2(last.DurationSec + s.DurationSec)
			if s.Confidence > last.Confidence {
				last.Confidence = s.Confidence
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

func chordSummary(segs []ChordSegment) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		if s.Chord == "N.C." {
			continue
		}
		parts = append(parts, s.Chord)
		if len(parts) >= maxChordSummary {
			break
		}
	}
	return strings.Join(parts, " | ")
}
