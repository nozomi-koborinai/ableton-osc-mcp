package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// jsonschema tag values are comma-separated, so a bare comma inside a
// description silently truncates it — "Beats, Tones, Texture" arrives as
// "Beats". Struct tags also pass through strconv.Unquote, so the escape has to
// be a doubled backslash in source (`\\,`) to leave the library a single one.
//
// This test reads the package source rather than reflecting over types,
// because a truncated description is indistinguishable from a short one once
// the schema has been built.
func TestDescriptionCommasAreEscaped(t *testing.T) {
	schemaKey := regexp.MustCompile(`^(minimum|maximum|exclusiveMinimum|exclusiveMaximum|` +
		`enum|default|required|pattern|format|minLength|maxLength|minItems|maxItems|` +
		`uniqueItems|title|example|oneof|anyof|readOnly|writeOnly|deprecated)=`)
	tagPattern := regexp.MustCompile(`jsonschema:"([^"]*)"`)

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			m := tagPattern.FindStringSubmatch(line)
			if m == nil || !strings.Contains(m[1], "description=") {
				continue
			}
			checked++
			text := strings.SplitN(m[1], "description=", 2)[1]
			// Everything after the description must be a schema key; anything
			// else means a comma cut the description short.
			for _, seg := range splitOnBareCommas(text)[1:] {
				if !schemaKey.MatchString(seg) {
					t.Errorf("%s:%d: description is truncated at a comma; write it as \\\\, \n  lost: %q",
						file, i+1, seg)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no jsonschema descriptions found - the scan is broken")
	}
	t.Logf("checked %d descriptions", checked)
}

// splitOnBareCommas splits on commas that are not preceded by a backslash,
// mirroring what invopop/jsonschema does at runtime.
func splitOnBareCommas(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == ',' && !strings.HasSuffix(cur.String(), `\`) {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	return append(out, cur.String())
}

// Every tool parameter should tell the model what it means. Index parameters
// are the ones that hurt most when missing: nothing else says they are 0-based.
func TestEveryParameterHasDescription(t *testing.T) {
	fieldPattern := regexp.MustCompile(`json:"([a-z0-9_]+)(?:,[a-z]+)*"`)

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	inputStruct := regexp.MustCompile(`^type (\w+(?:Input|Target)) struct \{`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		current := ""
		for i, line := range strings.Split(string(src), "\n") {
			if m := inputStruct.FindStringSubmatch(line); m != nil {
				current = m[1]
				continue
			}
			if strings.TrimSpace(line) == "}" {
				current = ""
				continue
			}
			if current == "" {
				continue
			}
			m := fieldPattern.FindStringSubmatch(line)
			if m == nil || strings.Contains(line, "description=") {
				continue
			}
			t.Errorf("%s:%d: %s.%s has no description", file, i+1, current, m[1])
		}
	}
}
