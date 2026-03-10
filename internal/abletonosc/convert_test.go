package abletonosc

import (
	"testing"
)

func TestAsFloat64(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    float64
		wantErr bool
	}{
		{"float32", float32(3.14), 3.140000104904175, false},
		{"float64", float64(2.718), 2.718, false},
		{"int32", int32(42), 42.0, false},
		{"int64", int64(100), 100.0, false},
		{"int", int(7), 7.0, false},
		{"string", "hello", 0, true},
		{"nil", nil, 0, true},
		{"bool", true, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AsFloat64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("AsFloat64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("AsFloat64(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAsInt(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    int
		wantErr bool
	}{
		{"int32", int32(42), 42, false},
		{"int64", int64(100), 100, false},
		{"int", int(7), 7, false},
		{"float32", float32(3.9), 3, false},
		{"float64", float64(9.1), 9, false},
		{"string", "hello", 0, true},
		{"nil", nil, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AsInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("AsInt(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("AsInt(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAsBool(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    bool
		wantErr bool
	}{
		{"true", true, true, false},
		{"false", false, false, false},
		{"int32_1", int32(1), true, false},
		{"int32_0", int32(0), false, false},
		{"int64_1", int64(1), true, false},
		{"int_0", int(0), false, false},
		{"float32_nonzero", float32(0.5), true, false},
		{"float32_zero", float32(0), false, false},
		{"float64_nonzero", float64(1.0), true, false},
		{"float64_zero", float64(0), false, false},
		{"string", "true", false, true},
		{"nil", nil, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AsBool(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("AsBool(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("AsBool(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
