package audioanalyze

import (
	"math"
	"testing"
)

// testBeat renders a drum pattern with chords underneath, so that the grid and
// everything built on it can be checked against what was put in.
type testBeat struct {
	bpm        float64
	offsetSec  float64 // silence before the first downbeat
	bars       int
	low        []int   // steps (of 32, two bars) with a kick
	mid        []int   // steps with a clap
	high       []int   // steps with a hat
	chords     [][]int // one chord per bar, cycled
	chordLevel float64
	pad        bool // one chord held for the whole track, fading in, instead of a change on every bar
	sampleRate int
}

func defaultTestBeat() testBeat {
	var hats []int
	for step := 0; step < 32; step += 2 {
		hats = append(hats, step)
	}
	return testBeat{
		bpm: 140, offsetSec: 0.37, bars: 8, sampleRate: 44100, chordLevel: 0.05,
		low:    []int{0, 6, 12, 16 + 3, 16 + 10},
		mid:    []int{8, 16 + 8},
		high:   hats,
		chords: [][]int{{48, 60, 64, 67}, {45, 57, 60, 64}, {41, 53, 57, 60}, {43, 55, 59, 62}},
	}
}

func (b testBeat) stepSec() float64 { return 60 / b.bpm / 4 }

func (b testBeat) render() []float64 {
	sr := float64(b.sampleRate)
	total := b.offsetSec + float64(b.bars)*16*b.stepSec() + 0.5
	out := make([]float64, int(total*sr))
	state := uint64(3)
	random := func() float64 {
		state = state*6364136223846793005 + 1442695040888963407
		return float64(state>>11) / (1 << 53)
	}
	burst := func(at float64, freqs []float64, decay, level float64) {
		start := int(at * sr)
		for i := 0; i < int(5*decay*sr) && start+i < len(out); i++ {
			tt := float64(i) / sr
			v := 0.0
			for _, f := range freqs {
				v += math.Sin(2 * math.Pi * f * tt)
			}
			out[start+i] += level * v / float64(len(freqs)) * math.Exp(-tt/decay)
		}
	}
	band := func(lo, hi float64, n int) []float64 {
		freqs := make([]float64, n)
		for i := range freqs {
			freqs[i] = lo + (hi-lo)*random()
		}
		return freqs
	}
	clap, hat := band(1500, 5000, 40), band(8000, 15000, 40)
	for bar := 0; bar < b.bars; bar++ {
		barStart := b.offsetSec + float64(bar)*16*b.stepSec()
		within := func(steps []int, play func(at float64)) {
			for _, step := range steps {
				if step/16 == bar%2 {
					play(barStart + float64(step%16)*b.stepSec())
				}
			}
		}
		within(b.low, func(at float64) { burst(at, []float64{55, 110}, 0.08, 0.6) })
		within(b.mid, func(at float64) { burst(at, clap, 0.03, 0.4) })
		within(b.high, func(at float64) { burst(at, hat, 0.01, 0.2) })
		if len(b.chords) > 0 && !b.pad {
			chord := detunedNotes(b.chords[bar%len(b.chords)], 0, b.sampleRate, 16*b.stepSec())
			start := int(barStart * sr)
			for i, v := range chord {
				if start+i < len(out) {
					out[start+i] += v * b.chordLevel / 0.1
				}
			}
		}
	}
	if b.pad {
		for i, v := range detunedNotes(b.chords[0], 0, b.sampleRate, total-b.offsetSec) {
			if at := int(b.offsetSec*sr) + i; at < len(out) {
				out[at] += v * math.Min(1, float64(i)/(2*sr)) * b.chordLevel / 0.1 // fading in over two seconds
			}
		}
	}
	return out
}

func TestBeatGridFindsTheBeatAndTheDownbeat(t *testing.T) {
	t.Parallel()

	beat := defaultTestBeat()
	grid, ok := buildBeatGrid(beat.render(), beat.sampleRate, beatGridOptions{BPM: 140.3, TuningCents: 0})
	if !ok {
		t.Fatal("buildBeatGrid returned ok=false")
	}
	beatSec := 60 / grid.BPM
	if math.Abs(grid.BPM-140) > 0.15 {
		t.Errorf("bpm = %.2f, want the estimate of 140.3 refined to 140", grid.BPM)
	}
	if off := math.Mod(grid.DownbeatSec-beat.offsetSec+100*4*beatSec, 4*beatSec); math.Min(off, 4*beatSec-off) > 0.02 {
		t.Errorf("downbeat at %.3f s, want %.3f s (or whole bars away); confidence %.2f", grid.DownbeatSec, beat.offsetSec, grid.DownbeatConfidence)
	}
	if off := math.Mod(grid.BeatOffsetSec-beat.offsetSec+100*beatSec, beatSec); math.Min(off, beatSec-off) > 0.015 {
		t.Errorf("beats at %.3f s + k x %.3f, want them on %.3f s", grid.BeatOffsetSec, beatSec, beat.offsetSec)
	}
	if grid.Bars < 7 || grid.Bars > 8 || grid.DownbeatConfidence <= 0 {
		t.Errorf("bars = %d, downbeat confidence = %.2f", grid.Bars, grid.DownbeatConfidence)
	}
}

func TestBeatGridTakesAGivenDownbeatAtItsWord(t *testing.T) {
	t.Parallel()

	beat := defaultTestBeat()
	given := beat.offsetSec + 60/140.0 // a beat later than the true one: the caller knows best
	grid, ok := buildBeatGrid(beat.render(), beat.sampleRate, beatGridOptions{BPM: 140, ExactTempo: true, DownbeatSec: given, DownbeatSet: true})
	if !ok || math.Abs(grid.DownbeatSec-given) > 1e-3 || grid.DownbeatConfidence != 1 || grid.BPM != 140 {
		t.Errorf("grid = %+v, want the downbeat at %.3f s, confidence 1, tempo untouched", grid, given)
	}
	// A bounce starts on its bar line: zero is a downbeat like any other.
	grid, ok = buildBeatGrid(beat.render(), beat.sampleRate, beatGridOptions{BPM: 140, ExactTempo: true, DownbeatSec: 0, DownbeatSet: true})
	if !ok || grid.DownbeatSec != 0 || grid.BeatOffsetSec != 0 {
		t.Errorf("grid = %+v, want everything on zero", grid)
	}
}

func TestBeatGridNeedsEnoughMusic(t *testing.T) {
	t.Parallel()

	short := defaultTestBeat()
	short.bars = 1
	if _, ok := buildBeatGrid(short.render(), short.sampleRate, beatGridOptions{BPM: 140}); ok {
		t.Error("one bar is not enough to find a bar grid in")
	}
	if _, ok := buildBeatGrid(defaultTestBeat().render(), 44100, beatGridOptions{BPM: 0}); ok {
		t.Error("no tempo, no grid")
	}
}

// Seen on a real drill beat: the tempo estimate came in at two thirds of the
// tempo (95.7 for 143), and everything built on the grid fell apart. Only one
// of the related tempos puts the onsets on its sixteenths.
func TestBeatGridSettlesOnTheTempoWhoseSixteenthsTheOnsetsSitOn(t *testing.T) {
	t.Parallel()

	beat := defaultTestBeat()
	beat.bars = 16
	beat.high = []int{0, 2, 3, 5, 6, 8, 11, 12, 15, 16, 18, 19, 21, 22, 24, 31} // hats that do not just tick eighths
	samples := beat.render()
	for name, estimate := range map[string]float64{"two thirds": 140 * 2.0 / 3, "three halves": 210, "four thirds": 140 * 4.0 / 3, "right": 140.4} {
		grid, ok := buildBeatGrid(samples, beat.sampleRate, beatGridOptions{BPM: estimate})
		if !ok || math.Abs(grid.BPM-140) > 0.3 {
			t.Errorf("estimate %s (%.1f BPM): grid at %.2f BPM, want 140", name, estimate, grid.BPM)
		}
	}
}
