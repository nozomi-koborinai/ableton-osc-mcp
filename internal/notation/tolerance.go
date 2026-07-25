package notation

// BeatTolerance is the largest difference in beats that still counts as a match
// when a clip written from notation is read back from Live.
//
// Measured against Live 11.0.12 on 2026-07-25 with cmd/measure_roundtrip. OSC
// carries note times as float32, so the error is relative to the value: notes
// near the clip start came back within 4.2e-8 beats, a note at beat 127 within
// 2.3e-6, and one at beat 511 within 7.9e-6. This constant is the worst of those
// rounded up with room to spare.
//
// The absolute form holds because the error grows with position: at 1e-4 beats
// it stops covering float32 rounding somewhere past beat 800, roughly bar 200 in
// 4/4. Session clips are nowhere near that long. If the notation is ever pointed
// at something that is, this has to become a relative comparison.
//
// It is far below anything musical. At 120 BPM this is 50 microseconds, and the
// finest grid anyone edits on, a 128th note, is over three hundred times larger.
const BeatTolerance = 0.0001
