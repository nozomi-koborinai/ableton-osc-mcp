package tools

type EmptyInput struct{}

type MidiNote struct {
	Pitch     int     `json:"pitch" jsonschema:"description=MIDI note number,minimum=0,maximum=127"`
	StartTime float64 `json:"start_time" jsonschema:"description=Start time in beats (float),minimum=0"`
	Duration  float64 `json:"duration" jsonschema:"description=Duration in beats (float),minimum=0.01"`
	Velocity  int     `json:"velocity" jsonschema:"description=Velocity 1-127,minimum=1,maximum=127"`
	Mute      *bool   `json:"mute,omitempty" jsonschema:"description=Optional mute flag"`
}
