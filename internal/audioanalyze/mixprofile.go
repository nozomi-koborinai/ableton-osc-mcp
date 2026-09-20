package audioanalyze

import "math"

const (
	mixFrameSize = 8192 // power of two, required by fftInPlace
	mixHopSize   = 4096
	mixLoHz      = 20.0
	mixHiHz      = 16000.0
	maxRatioDB   = 120.0
)

// MixProfile is the yardstick for comparing one mix with another: how loud, how
// squashed, where the energy sits across the spectrum, and how wide it is.
type MixProfile struct {
	LUFSIntegrated float64     `json:"lufs_integrated" jsonschema:"description=Integrated loudness per ITU-R BS.1770-4 (-120 when silent or shorter than 400 ms)"`
	TruePeakDBTP   float64     `json:"true_peak_dbtp" jsonschema:"description=Inter-sample peak from 4x oversampling"`
	SamplePeakDBFS float64     `json:"sample_peak_dbfs"`
	CrestDB        float64     `json:"crest_db" jsonschema:"description=Sample peak minus RMS; below about 8 dB a full mix is being squashed"`
	Bands          []BandLevel `json:"bands" jsonschema:"description=Nine bands from 20 Hz to 16 kHz\\, each in dB relative to their total"`
	Width          []BandWidth `json:"width" jsonschema:"description=Stereo width in three bands"`
	AnalyzedSec    float64     `json:"analyzed_sec"`
}

// BandLevel is one band's share of the 20 Hz–16 kHz energy, in dB.
type BandLevel struct {
	Label string  `json:"label"`
	LoHz  float64 `json:"lo_hz"`
	HiHz  float64 `json:"hi_hz"`
	DB    float64 `json:"db"`
}

// BandWidth describes stereo width inside one band.
type BandWidth struct {
	Label          string  `json:"label"`
	LoHz           float64 `json:"lo_hz"`
	HiHz           float64 `json:"hi_hz"`
	LRCorrelation  float64 `json:"lr_correlation" jsonschema:"description=1 is mono\\, 0 is uncorrelated (wide)\\, -1 is out of phase"`
	SideMinusMidDB float64 `json:"side_minus_mid_db" jsonschema:"description=Side energy relative to mid; larger is wider"`
}

type bandEdge struct {
	label  string
	lo, hi float64
}

var mixBandEdges = []bandEdge{
	{"<60", 20, 60}, {"60-120", 60, 120}, {"120-250", 120, 250}, {"250-500", 250, 500}, {"500-1k", 500, 1000},
	{"1-2k", 1000, 2000}, {"2-4k", 2000, 4000}, {"4-8k", 4000, 8000}, {"8-16k", 8000, 16000},
}

var widthBandEdges = []bandEdge{{"250-1k", 250, 1000}, {"1-4k", 1000, 4000}, {"4-16k", 4000, 16000}}

// mixSpectra holds power summed over all analysis frames, per FFT bin.
type mixSpectra struct {
	binHz float64
	mono  []float64 // |(L+R)/2|^2
	ll    []float64 // |L|^2
	rr    []float64 // |R|^2
	cross []float64 // Re(L * conj(R))
}

func accumulateMixSpectra(left, right []float64, sampleRate int) mixSpectra {
	half := mixFrameSize / 2
	sp := mixSpectra{
		binHz: float64(sampleRate) / mixFrameSize,
		mono:  make([]float64, half),
		ll:    make([]float64, half),
		rr:    make([]float64, half),
		cross: make([]float64, half),
	}
	n := len(left)
	if n == 0 {
		return sp
	}
	window := hannWindow(mixFrameSize)
	lre, lim := make([]float64, mixFrameSize), make([]float64, mixFrameSize)
	rre, rim := make([]float64, mixFrameSize), make([]float64, mixFrameSize)

	// Frames that fit entirely, or one zero-padded frame for a short clip.
	frames := 1
	if n > mixFrameSize {
		frames = 1 + (n-mixFrameSize)/mixHopSize
	}
	for f := 0; f < frames; f++ {
		start := f * mixHopSize
		for i := 0; i < mixFrameSize; i++ {
			var l, r float64
			if start+i < n {
				l, r = left[start+i], right[start+i]
			}
			lre[i], lim[i] = l*window[i], 0
			rre[i], rim[i] = r*window[i], 0
		}
		fftInPlace(lre, lim)
		fftInPlace(rre, rim)
		for k := 0; k < half; k++ {
			mre, mim := (lre[k]+rre[k])/2, (lim[k]+rim[k])/2
			sp.mono[k] += mre*mre + mim*mim
			sp.ll[k] += lre[k]*lre[k] + lim[k]*lim[k]
			sp.rr[k] += rre[k]*rre[k] + rim[k]*rim[k]
			sp.cross[k] += lre[k]*rre[k] + lim[k]*rim[k]
		}
	}
	return sp
}

// sum adds the bins whose centre frequency falls in [lo, hi).
func (sp mixSpectra) sum(v []float64, lo, hi float64) float64 {
	var s float64
	for k := range v {
		if f := float64(k) * sp.binHz; f >= lo && f < hi {
			s += v[k]
		}
	}
	return s
}

// ratioDB is 10*log10(num/den), pinned to ±maxRatioDB so an empty numerator or
// denominator never produces an infinity.
func ratioDB(num, den float64) float64 {
	if num <= 0 {
		return -maxRatioDB
	}
	if den <= 0 {
		return maxRatioDB
	}
	return math.Max(-maxRatioDB, math.Min(maxRatioDB, 10*math.Log10(num/den)))
}

func amplitudeDB(a float64) float64 {
	if a <= 0 {
		return silenceFloorDB
	}
	return math.Max(20*math.Log10(a), silenceFloorDB)
}

// measureMix builds the MixProfile of a decoded clip. channels is the source
// channel count: a mono source is measured as one channel, as BS.1770 asks,
// even though left and right hold the same samples.
func measureMix(left, right []float64, sampleRate, channels int) MixProfile {
	chans := [][]float64{left, right}
	if channels < 2 {
		chans = [][]float64{left}
	}

	var peak, sumSq float64
	var count int
	for _, ch := range chans {
		for _, v := range ch {
			if a := math.Abs(v); a > peak {
				peak = a
			}
			sumSq += v * v
		}
		count += len(ch)
	}
	crest := 0.0
	if count > 0 && sumSq > 0 {
		crest = amplitudeDB(peak) - amplitudeDB(math.Sqrt(sumSq/float64(count)))
	}

	sp := accumulateMixSpectra(left, right, sampleRate)
	total := sp.sum(sp.mono, mixLoHz, mixHiHz)
	bands := make([]BandLevel, 0, len(mixBandEdges))
	for _, e := range mixBandEdges {
		bands = append(bands, BandLevel{Label: e.label, LoHz: e.lo, HiHz: e.hi, DB: round2(ratioDB(sp.sum(sp.mono, e.lo, e.hi), total))})
	}

	width := make([]BandWidth, 0, len(widthBandEdges))
	for _, e := range widthBandEdges {
		ll, rr, cross := sp.sum(sp.ll, e.lo, e.hi), sp.sum(sp.rr, e.lo, e.hi), sp.sum(sp.cross, e.lo, e.hi)
		corr := 1.0 // nothing in the band: report "not wide" rather than NaN
		if ll > 0 && rr > 0 {
			corr = math.Max(-1, math.Min(1, cross/math.Sqrt(ll*rr)))
		}
		side, mid := (ll+rr-2*cross)/4, (ll+rr+2*cross)/4
		// Rounding noise can leave a vanishing residue where the true value is zero.
		if side < 1e-12*(ll+rr) {
			side = 0
		}
		if mid < 1e-12*(ll+rr) {
			mid = 0
		}
		width = append(width, BandWidth{
			Label: e.label, LoHz: e.lo, HiHz: e.hi,
			LRCorrelation:  round2(corr),
			SideMinusMidDB: round2(ratioDB(side, mid)),
		})
	}

	seconds := 0.0
	if sampleRate > 0 {
		seconds = float64(len(left)) / float64(sampleRate)
	}
	return MixProfile{
		LUFSIntegrated: round2(integratedLUFS(chans, sampleRate)),
		TruePeakDBTP:   round2(truePeakDBTP(chans)),
		SamplePeakDBFS: round2(amplitudeDB(peak)),
		CrestDB:        round2(crest),
		Bands:          bands,
		Width:          width,
		AnalyzedSec:    round2(seconds),
	}
}
