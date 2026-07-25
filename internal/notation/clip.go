package notation

// Note is one MIDI note as Live stores it. Live's note is exactly these five
// fields, which is why a notation carrying all five loses nothing.
type Note struct {
	Pitch     int
	StartTime float64 // beats from the clip start
	Duration  float64 // beats
	Velocity  int     // 1..127
	Mute      bool
}

// Clip is everything the notation says about a MIDI clip. Envelopes and
// audio-clip properties are absent on purpose: the notation neither reads nor
// writes them, so leaving them out keeps the round-trip claim honest.
type Clip struct {
	Name   string
	Bars   int
	SigNum int
	SigDen int
	Notes  []Note
}
