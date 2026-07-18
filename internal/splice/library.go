package splice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultMaxResults = 20
	maxMaxResults     = 50
	maxWalkFiles      = 5000
)

var audioExtensions = map[string]bool{
	".wav":  true,
	".aif":  true,
	".aiff": true,
	".flac": true,
	".mp3":  true,
	".ogg":  true,
}

type Sample struct {
	Name         string `json:"name"`
	RelativePath string `json:"relative_path"`
	AbsolutePath string `json:"absolute_path"`
}

type Library struct {
	Path   string
	Source string // env, auto, or missing
}

func Resolve(configured string) (Library, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		abs, err := filepath.Abs(expandHome(configured))
		if err != nil {
			return Library{}, fmt.Errorf("resolve splice path: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return Library{Path: abs, Source: "env"}, fmt.Errorf("splice path from ABLETON_OSC_SPLICE_PATH: %w", err)
		}
		if !info.IsDir() {
			return Library{Path: abs, Source: "env"}, errors.New("ABLETON_OSC_SPLICE_PATH must be a directory")
		}
		return Library{Path: abs, Source: "env"}, nil
	}

	for _, candidate := range defaultCandidates() {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return Library{Path: candidate, Source: "auto"}, nil
		}
	}
	return Library{Source: "missing"}, errors.New("splice library not found; set ABLETON_OSC_SPLICE_PATH or install/sync the Splice desktop app")
}

func Search(root, query string, maxResults int) ([]Sample, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("splice library path is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("splice library: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("splice library path must be a directory")
	}
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > maxMaxResults {
		maxResults = maxMaxResults
	}
	query = strings.ToLower(strings.TrimSpace(query))

	var (
		matches []Sample
		seen    int
	)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > maxWalkFiles {
			return errors.New("splice library walk limit reached; narrow the query or set a deeper ABLETON_OSC_SPLICE_PATH")
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !audioExtensions[ext] {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		base := d.Name()
		if query != "" {
			haystack := strings.ToLower(rel + " " + strings.TrimSuffix(base, ext))
			if !strings.Contains(haystack, query) {
				return nil
			}
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			abs = path
		}
		matches = append(matches, Sample{
			Name:         base,
			RelativePath: filepath.ToSlash(rel),
			AbsolutePath: abs,
		})
		if len(matches) >= maxResults {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, err
	}
	return matches, nil
}

var errStopWalk = errors.New("stop walk")

func defaultCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "Splice"),
		filepath.Join(home, "Documents", "Splice"),
	}
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
