package tools

import (
	"fmt"
	"strings"
)

// ActionableError is a structured failure with a concrete next manual step
// for the agent/user when automation cannot finish the job.
type ActionableError struct {
	Code     string // stable machine-readable code, e.g. "unsupported_live_version"
	Message  string // what failed
	NextStep string // one concrete action a human (or agent) should take next
}

func (e *ActionableError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	if e.Code != "" {
		b.WriteString(e.Code)
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.NextStep != "" {
		b.WriteString(" | next: ")
		b.WriteString(e.NextStep)
	}
	return b.String()
}

func actionable(code, message, next string) error {
	return &ActionableError{Code: code, Message: message, NextStep: next}
}

// wrapActionable upgrades a plain error with a code and next step when the
// underlying error is not already actionable.
func wrapActionable(err error, code, next string) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*ActionableError); ok {
		return err
	}
	return &ActionableError{Code: code, Message: err.Error(), NextStep: next}
}

// requireConfirm returns a preview-style actionable error when confirm is false.
func requireConfirm(confirm bool, action string, summary string) error {
	if confirm {
		return nil
	}
	return actionable(
		"confirm_required",
		fmt.Sprintf("preview only — would %s: %s", action, summary),
		fmt.Sprintf("Re-call with confirm=true to execute %s.", action),
	)
}
