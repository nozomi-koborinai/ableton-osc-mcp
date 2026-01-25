package abletonosc

import (
	"strconv"
	"strings"
)

func ParseArg(s string) interface{} {
	ss := strings.TrimSpace(s)
	if ss == "" {
		return ""
	}
	low := strings.ToLower(ss)
	if low == "true" {
		return true
	}
	if low == "false" {
		return false
	}
	if i, err := strconv.Atoi(ss); err == nil {
		return int32(i)
	}
	if f, err := strconv.ParseFloat(ss, 64); err == nil {
		return float32(f)
	}
	return ss
}
