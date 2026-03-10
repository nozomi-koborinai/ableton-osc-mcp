package abletonosc

import (
	"testing"
)

func TestParseArg(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  interface{}
	}{
		{"empty", "", ""},
		{"whitespace_only", "   ", ""},
		{"true_lower", "true", true},
		{"true_upper", "TRUE", true},
		{"true_mixed", "True", true},
		{"false_lower", "false", false},
		{"false_upper", "FALSE", false},
		{"integer", "42", int32(42)},
		{"negative_integer", "-1", int32(-1)},
		{"zero", "0", int32(0)},
		{"float", "3.14", float32(3.14)},
		{"negative_float", "-0.5", float32(-0.5)},
		{"string", "hello", "hello"},
		{"string_with_spaces", "  hello  ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseArg(tt.input)
			if got != tt.want {
				t.Errorf("ParseArg(%q) = %v (%T), want %v (%T)", tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}
