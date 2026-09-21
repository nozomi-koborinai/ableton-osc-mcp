package audioanalyze

import (
	"math"
	"testing"
)

func resampleTestTone(freq, amplitude float64, rate int, seconds float64) []float64 {
	x := make([]float64, int(seconds*float64(rate)))
	for i := range x {
		x[i] = amplitude * math.Sin(2*math.Pi*freq*float64(i)/float64(rate))
	}
	return x
}

// toneAmplitude is the amplitude of one frequency in the middle of a signal
// (the ends carry the filter's run-in), by correlating with a sine and a cosine.
func toneAmplitude(x []float64, freq float64, rate int) float64 {
	from, to := len(x)/10, len(x)-len(x)/10
	var s, c float64
	for i := from; i < to; i++ {
		phase := 2 * math.Pi * freq * float64(i) / float64(rate)
		s += x[i] * math.Sin(phase)
		c += x[i] * math.Cos(phase)
	}
	return 2 * math.Hypot(s, c) / float64(to-from)
}

func middleRMS(x []float64) float64 {
	from, to := len(x)/10, len(x)-len(x)/10
	sum := 0.0
	for _, v := range x[from:to] {
		sum += v * v
	}
	return math.Sqrt(sum / float64(to-from))
}

func TestResampleKeepsTonesInTheAudioBand(t *testing.T) {
	t.Parallel()

	for _, rates := range [][2]int{{48000, 44100}, {44100, 48000}, {96000, 44100}} {
		from, to := rates[0], rates[1]
		for _, tc := range []struct {
			freq      float64
			tolerance float64 // dB
		}{{1000, 0.02}, {18000, 0.05}, {20000, 0.1}} {
			out, err := resampleRational(resampleTestTone(tc.freq, 0.5, from, 1), from, to)
			if err != nil {
				t.Fatalf("%d->%d: %v", from, to, err)
			}
			if want := int(math.Round(float64(from) * float64(to) / float64(from))); math.Abs(float64(len(out)-want)) > 1 {
				t.Errorf("%d->%d: %d samples out for one second, want %d", from, to, len(out), want)
			}
			// The tone has to come out at the same frequency and the same level.
			level := 20 * math.Log10(toneAmplitude(out, tc.freq, to)/0.5)
			if math.Abs(level) > tc.tolerance {
				t.Errorf("%d->%d: %.0f Hz comes out %+.3f dB, want within %.2f dB", from, to, tc.freq, level, tc.tolerance)
			}
		}
	}
}

func TestResampleRejectsWhatWouldAlias(t *testing.T) {
	t.Parallel()

	// 30 kHz does not fit into 44.1 kHz. Left in, it would fold down to 14.1 kHz.
	// (The tone needs a 96 kHz source: sampled at 48 kHz it would not be 30 kHz.)
	out, err := resampleRational(resampleTestTone(30000, 0.5, 96000, 1), 96000, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if level := 20 * math.Log10(middleRMS(out)/(0.5/math.Sqrt2)); level > -90 {
		t.Errorf("30 kHz leaves %.1f dB behind after 96k->44.1k, want -90 dB or less", level)
	}
	// From 48 kHz the filter has 20 kHz to 23.8 kHz to fall in. What is left of
	// that band folds to above 20.3 kHz, out of hearing; from 23.9 kHz on it is gone.
	edge, err := resampleRational(resampleTestTone(23900, 0.5, 48000, 1), 48000, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if level := 20 * math.Log10(middleRMS(edge)/(0.5/math.Sqrt2)); level > -90 {
		t.Errorf("23.9 kHz leaves %.1f dB behind after 48k->44.1k, want -90 dB or less", level)
	}

	// Going up, the copies of the band above the old Nyquist must not appear.
	up, err := resampleRational(resampleTestTone(20000, 0.5, 44100, 1), 44100, 48000)
	if err != nil {
		t.Fatal(err)
	}
	if image := 20 * math.Log10(toneAmplitude(up, 44100-20000, 48000)/0.5); image > -90 {
		t.Errorf("the image of 20 kHz at 24.1 kHz is at %.1f dB after 44.1k->48k, want -90 dB or less", image)
	}
}

func TestResampleKeepsTimeAndLevelOfAStep(t *testing.T) {
	t.Parallel()

	// One second of silence, then a DC level: the step must stay where it was,
	// and the level must come out exactly (a gain error here is a gain error everywhere).
	x := make([]float64, 96000)
	for i := 48000; i < len(x); i++ {
		x[i] = 0.25
	}
	out, err := resampleRational(x, 48000, 44100)
	if err != nil {
		t.Fatal(err)
	}
	if got := out[len(out)-1000]; math.Abs(got-0.25) > 1e-6 {
		t.Errorf("DC level = %v, want 0.25", got)
	}
	half := -1
	for i, v := range out {
		if v >= 0.125 {
			half = i
			break
		}
	}
	if math.Abs(float64(half)-44100) > 1 {
		t.Errorf("the step crosses half way at sample %d, want 44100: the filter must not delay the signal", half)
	}
}

func TestResampleSameRateIsACopy(t *testing.T) {
	t.Parallel()

	x := []float64{0.1, -0.2, 0.3}
	out, err := resampleRational(x, 44100, 44100)
	if err != nil || len(out) != 3 || out[1] != -0.2 {
		t.Fatalf("out = %v, %v", out, err)
	}
	out[1] = 9
	if x[1] != -0.2 {
		t.Error("the input was changed through the returned slice")
	}
	if _, err := resampleRational(x, 44101, 48000); err == nil {
		t.Error("a rate pair that needs thousands of filter phases should be refused")
	}
}
