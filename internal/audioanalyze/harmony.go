package audioanalyze

import (
	"math"
	"strings"
)

const (
	harmonyBeatsPerSegment = 2 // one decision per half bar
	harmonyUpperMinFreq    = 200.0
	harmonyUpperMaxFreq    = 2000.0
	harmonyBassFrameSize   = 16384 // 2.7-2.9 Hz per bin: enough to tell an 808's notes apart
	harmonyBassHopSize     = 4096
	harmonyBassMinFreq     = 30.0
	harmonyBassMaxFreq     = 200.0
	harmonyBassPeakShare   = 0.25 // the bass note is the lowest peak that reaches this share of the strongest
	harmonyBassSure        = 0.5  // share of the bass votes one pitch class needs to be taken for the bass
	harmonyBassBonus       = 0.15 // added to the fit of a chord whose root the bass plays
	harmonyMinFit          = 0.3
	harmonyMinKeyMargin    = 0.05
	harmonyCycleAgreement  = 0.7
	harmonyCycleTolerance  = 0.1
	harmonySilenceShare    = 0.02 // a segment this quiet next to the loudest one has no chord
)

// HarmonyChord is one chord on the bar grid.
type HarmonyChord struct {
	Bar        int     `json:"bar" jsonschema:"description=Bar on the grid\\, counted from 1"`
	Beat       float64 `json:"beat" jsonschema:"description=1 or 3"`
	Beats      float64 `json:"beats"`
	StartSec   float64 `json:"start_sec"`
	Chord      string  `json:"chord" jsonschema:"description=e.g. Bmaj9\\, G#m7/B\\, or N.C."`
	Root       string  `json:"root,omitempty"`
	Quality    string  `json:"quality,omitempty"`
	Bass       string  `json:"bass,omitempty" jsonschema:"description=Lowest note heard\\, from 30-200 Hz"`
	Degree     string  `json:"degree,omitempty" jsonschema:"description=Root against the detected key\\, e.g. i or ♭VI: what to transpose by"`
	Confidence float64 `json:"confidence" jsonschema:"description=How well the chord fits what was heard\\, 0-1"`
}

// Harmony is the chords of a track on its bar grid, and the loop they make.
type Harmony struct {
	Chords       []HarmonyChord `json:"chords"`
	CycleBars    int            `json:"cycle_bars,omitempty" jsonschema:"description=Length of the progression that repeats"`
	Cycle        []string       `json:"cycle,omitempty" jsonschema:"description=One round of it\\, a chord per half bar"`
	CycleDegrees []string       `json:"cycle_degrees,omitempty"`
	Summary      string         `json:"summary" jsonschema:"description=One round\\, bar by bar: D#m(add9) | D#m(add9) | Badd9 | C#add9"`
	Note         string         `json:"note"`
}

type harmonyQuality struct {
	suffix    string
	intervals []int
	minor     bool
}

var harmonyQualities = []harmonyQuality{
	{"", []int{0, 4, 7}, false},
	{"m", []int{0, 3, 7}, true},
	{"sus2", []int{0, 2, 7}, false},
	{"sus4", []int{0, 5, 7}, false},
	{"dim", []int{0, 3, 6}, true},
	{"7", []int{0, 4, 7, 10}, false},
	{"maj7", []int{0, 4, 7, 11}, false},
	{"m7", []int{0, 3, 7, 10}, true},
	{"add9", []int{0, 4, 7, 2}, false},
	{"m(add9)", []int{0, 3, 7, 2}, true},
	{"maj9", []int{0, 4, 7, 11, 2}, false},
	{"m9", []int{0, 3, 7, 10, 2}, true},
}

var degreeNames = [12]string{"I", "♭II", "II", "♭III", "III", "IV", "♭V", "V", "♭VI", "VI", "♭VII", "VII"}

// estimateHarmony names a chord for every half bar: the root from the low end,
// where an 808 or a bass says it plainly, the rest from the middle.
func estimateHarmony(samples []float64, sampleRate int, grid BeatGrid, tuningCents float64, key KeyResult, keyOK bool) (Harmony, bool) {
	segmentSec := harmonyBeatsPerSegment * grid.beatSec()
	segments := grid.Bars * beatsPerBar / harmonyBeatsPerSegment
	if segments < 2 || sampleRate <= 0 {
		return Harmony{}, false
	}
	segmentOf := func(centreSec float64) int {
		if centreSec < grid.DownbeatSec {
			return -1
		}
		return int((centreSec - grid.DownbeatSec) / segmentSec)
	}

	upper := make([][12]float64, segments)
	for f, frame := range frameChromasIn(samples, sampleRate, tuningCents, harmonyUpperMinFreq, harmonyUpperMaxFreq) {
		centre := (float64(f*keyHopSize) + keyFrameSize/2) / float64(sampleRate)
		if s := segmentOf(centre); s >= 0 && s < segments {
			for pc, v := range frame {
				upper[s][pc] += v
			}
		}
	}
	bass := bassVotes(samples, sampleRate, tuningCents, segments, segmentOf)

	loudest := 0.0
	energy := make([]float64, segments)
	for s := range upper {
		for _, v := range upper[s] {
			energy[s] += v
		}
		loudest = math.Max(loudest, energy[s])
	}

	tonic := -1
	if keyOK && key.Confidence >= harmonyMinKeyMargin {
		tonic = pitchClassIndex(key.Tonic)
	}
	labels := make([]HarmonyChord, segments)
	for s := range labels {
		chord := HarmonyChord{Chord: "N.C."}
		if energy[s] > harmonySilenceShare*loudest {
			chord = nameChord(upper[s], bass[s], tonic)
		}
		chord.Bar = s*harmonyBeatsPerSegment/beatsPerBar + 1
		chord.Beat = float64(s*harmonyBeatsPerSegment%beatsPerBar + 1)
		chord.Beats = harmonyBeatsPerSegment
		chord.StartSec = round2(grid.DownbeatSec + float64(s)*segmentSec)
		labels[s] = chord
	}

	out := Harmony{Note: "Chords are read from the full mix: a loud vocal or lead blurs the extensions (7ths, 9ths); roots and the loop are the sturdier part."}
	if grid.DownbeatConfidence < 0.2 {
		out.Note += " The downbeat is a guess here: bars may be rotated by one to three beats; pass downbeat_sec to pin it."
	}
	for _, chord := range labels { // runs of the same chord read as one
		if n := len(out.Chords); n > 0 && out.Chords[n-1].Chord == chord.Chord {
			out.Chords[n-1].Beats += chord.Beats
			out.Chords[n-1].Confidence = math.Max(out.Chords[n-1].Confidence, chord.Confidence)
			continue
		}
		out.Chords = append(out.Chords, chord)
	}
	out.CycleBars, out.Cycle, out.CycleDegrees = findChordCycle(labels)
	round := out.Cycle
	if len(round) == 0 {
		for _, chord := range labels[:min(len(labels), 16)] {
			round = append(round, chord.Chord)
		}
	}
	out.Summary = summarizeBars(round)
	return out, true
}

// frameChromasIn is the tuned chroma of one frequency range.
func frameChromasIn(samples []float64, sampleRate int, tuningCents, minFreq, maxFreq float64) [][12]float64 {
	window := hannWindow(keyFrameSize)
	buf := make([]float64, keyFrameSize)
	minBin := max(1, int(math.Floor(minFreq*float64(keyFrameSize)/float64(sampleRate))))
	maxBin := min(keyFrameSize/2, int(math.Ceil(maxFreq*float64(keyFrameSize)/float64(sampleRate))))
	var frames [][12]float64
	for start := 0; start+keyFrameSize <= len(samples); start += keyHopSize {
		for i := range buf {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		var chroma [12]float64
		for bin := minBin; bin <= maxBin; bin++ {
			freq := float64(bin) * float64(sampleRate) / float64(keyFrameSize)
			chroma[pitchClassOf(freq, tuningCents)] += math.Hypot(re[bin], im[bin])
		}
		frames = append(frames, chroma)
	}
	return frames
}

func pitchClassOf(freq, tuningCents float64) int {
	pc := int(math.Round(69+12*math.Log2(freq/440)-tuningCents/100)) % 12
	if pc < 0 {
		pc += 12
	}
	return pc
}

// bassVotes says, per segment, which pitch class the bass plays. Down there a
// semitone is narrower than an FFT bin, so bins cannot be summed into pitch
// classes: each long frame votes with its lowest strong peak, read to a fraction
// of a bin. The lowest, because an 808's third harmonic is another note.
func bassVotes(samples []float64, sampleRate int, tuningCents float64, segments int, segmentOf func(float64) int) [][12]float64 {
	votes := make([][12]float64, segments)
	if len(samples) < harmonyBassFrameSize {
		return votes
	}
	window := hannWindow(harmonyBassFrameSize)
	buf := make([]float64, harmonyBassFrameSize)
	binHz := float64(sampleRate) / harmonyBassFrameSize
	minBin, maxBin := max(2, int(math.Ceil(harmonyBassMinFreq/binHz))), int(math.Floor(harmonyBassMaxFreq/binHz))
	mags := make([]float64, maxBin+2)
	for start := 0; start+harmonyBassFrameSize <= len(samples); start += harmonyBassHopSize {
		s := segmentOf((float64(start) + harmonyBassFrameSize/2) / float64(sampleRate))
		if s < 0 || s >= segments {
			continue
		}
		for i := range buf {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		strongest := 0.0
		for bin := minBin - 1; bin <= maxBin+1; bin++ {
			mags[bin] = math.Hypot(re[bin], im[bin])
			strongest = math.Max(strongest, mags[bin])
		}
		for bin := minBin; bin <= maxBin && strongest > 0; bin++ {
			if mags[bin] < harmonyBassPeakShare*strongest || mags[bin] <= mags[bin-1] || mags[bin] < mags[bin+1] {
				continue
			}
			shift := 0.0
			if a, b, c := math.Log(mags[bin-1]+1e-12), math.Log(mags[bin]), math.Log(mags[bin+1]+1e-12); a-2*b+c < 0 {
				shift = 0.5 * (a - c) / (a - 2*b + c)
			}
			votes[s][pitchClassOf((float64(bin)+shift)*binHz, tuningCents)] += mags[bin]
			break
		}
	}
	return votes
}

// nameChord fits the chord qualities on every root to what the middle of the
// spectrum holds. The fit itself asks an added tone to be there at about half
// the level of the chord tones before an extended chord beats its triad. Some
// chords are the same notes under two names (Dsus2, Asus4): the bass, when it
// is clear, speaks for the root it plays.
func nameChord(upper, bass [12]float64, tonic int) HarmonyChord {
	bassPC, bassTotal := -1, 0.0
	for pc, v := range bass {
		bassTotal += v
		if bassPC < 0 || v > bass[bassPC] {
			bassPC = pc
		}
	}
	if bassTotal <= 0 || bass[bassPC] < harmonyBassSure*bassTotal {
		bassPC = -1
	}

	best, bestFit := HarmonyChord{Chord: "N.C."}, 0.0
	var bestQuality harmonyQuality
	bestRoot := -1
	for root := 0; root < 12; root++ {
		for _, quality := range harmonyQualities {
			var template [12]float64
			for _, interval := range quality.intervals {
				template[(root+interval)%12] = 1
			}
			fit := pearson(upper[:], template[:])
			score := fit
			if root == bassPC {
				score += harmonyBassBonus
			}
			if score > bestFit {
				bestFit, bestRoot, bestQuality = score, root, quality
				best.Confidence = round2(math.Max(0, math.Min(1, fit)))
			}
		}
	}
	if bestRoot < 0 || best.Confidence < harmonyMinFit {
		return HarmonyChord{Chord: "N.C.", Confidence: best.Confidence}
	}
	best.Root, best.Quality = pitchClassNames[bestRoot], bestQuality.suffix
	best.Chord = best.Root + best.Quality
	if bassPC >= 0 {
		best.Bass = pitchClassNames[bassPC]
		for _, interval := range bestQuality.intervals[1:] {
			if (bestRoot+interval)%12 == bassPC {
				best.Chord += "/" + best.Bass
			}
		}
	}
	if tonic >= 0 {
		best.Degree = degreeNames[(bestRoot-tonic+12)%12]
		if bestQuality.minor {
			best.Degree = strings.ToLower(best.Degree)
		}
	}
	return best
}

func pitchClassIndex(name string) int {
	for i, n := range pitchClassNames {
		if n == name {
			return i
		}
	}
	return -1
}

// findChordCycle looks for the length after which the half-bar labels repeat:
// the shortest of 1, 2, 4, 8, 16 bars that agrees with itself about as well as
// the best of them does. A loop of eight bars whose halves differ in one bar
// agrees with itself at four bars too, but clearly less well.
func findChordCycle(labels []HarmonyChord) (int, []string, []string) {
	perBar := beatsPerBar / harmonyBeatsPerSegment
	agreement := map[int]float64{}
	best := 0.0
	for _, bars := range []int{1, 2, 4, 8, 16} {
		shift := bars * perBar
		same, compared := 0, 0
		for i := 0; i+shift < len(labels); i++ {
			if labels[i].Chord == "N.C." || labels[i+shift].Chord == "N.C." {
				continue
			}
			compared++
			if labels[i].Chord == labels[i+shift].Chord {
				same++
			}
		}
		if compared >= shift { // at least one full round to compare against
			agreement[bars] = float64(same) / float64(compared)
			best = math.Max(best, agreement[bars])
		}
	}
	for _, bars := range []int{1, 2, 4, 8, 16} {
		share, ok := agreement[bars]
		if !ok || share < harmonyCycleAgreement || share < best-harmonyCycleTolerance {
			continue
		}
		shift := bars * perBar
		cycle, degrees := make([]string, shift), make([]string, shift)
		for position := 0; position < shift; position++ {
			counts := map[string]int{}
			for i := position; i < len(labels); i += shift {
				counts[labels[i].Chord]++
				if counts[labels[i].Chord] > counts[cycle[position]] || cycle[position] == "" {
					cycle[position], degrees[position] = labels[i].Chord, labels[i].Degree
				}
			}
		}
		withDegrees := false
		for _, degree := range degrees {
			withDegrees = withDegrees || degree != ""
		}
		if !withDegrees {
			degrees = nil
		}
		return bars, cycle, degrees
	}
	return 0, nil, nil
}

// summarizeBars writes half-bar labels bar by bar: one name when a bar holds one
// chord, two when it changes in the middle.
func summarizeBars(halfBars []string) string {
	var bars []string
	for i := 0; i < len(halfBars); i += 2 {
		if i+1 < len(halfBars) && halfBars[i+1] != halfBars[i] {
			bars = append(bars, halfBars[i]+" "+halfBars[i+1])
		} else {
			bars = append(bars, halfBars[i])
		}
	}
	return strings.Join(bars, " | ")
}
