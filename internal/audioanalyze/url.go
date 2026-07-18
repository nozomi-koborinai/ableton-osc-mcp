package audioanalyze

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	// urlAnalyzeTimeout bounds the whole fetch+decode. The full track is
	// streamed (bounded by maxFileBytes), analyzed in memory, and discarded;
	// nothing is written to disk.
	urlAnalyzeTimeout = 240 * time.Second
)

// AnalyzeURL streams a short window of audio referenced by a URL through
// yt-dlp and ffmpeg, analyzes it in memory, and discards it. No audio is ever
// written to disk. It extracts only factual metadata (tempo, length, levels)
// and never transcribes melodies or notes.
//
// The user is responsible for the legality of accessing the URL: yt-dlp
// touches the source site directly and some sites' terms prohibit this.
func AnalyzeURL(ctx context.Context, rawURL string, projectTempo float64) (Result, error) {
	clean, err := validateAudioURL(rawURL)
	if err != nil {
		return Result{}, err
	}
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return Result{}, errors.New("yt-dlp not found on PATH; install yt-dlp to analyze URLs (this server never downloads on its own)")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return Result{}, errors.New("ffmpeg not found on PATH; install ffmpeg to analyze URLs")
	}

	ctx, cancel := context.WithTimeout(ctx, urlAnalyzeTimeout)
	defer cancel()

	dl := exec.CommandContext(ctx, ytdlp, ytDlpArgs(clean)...)
	ff := exec.CommandContext(ctx, ffmpeg, ffmpegArgs()...)

	dlOut, err := dl.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("pipe yt-dlp output: %w", err)
	}
	ff.Stdin = dlOut
	ffOut, err := ff.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("pipe ffmpeg output: %w", err)
	}

	var dlErr, ffErr bytes.Buffer
	dl.Stderr = &dlErr
	ff.Stderr = &ffErr

	if err := ff.Start(); err != nil {
		return Result{}, fmt.Errorf("start ffmpeg: %w", err)
	}
	if err := dl.Start(); err != nil {
		return Result{}, fmt.Errorf("start yt-dlp: %w", err)
	}

	wav, readErr := io.ReadAll(io.LimitReader(ffOut, maxFileBytes+1))

	// Stop both processes once we have enough audio (e.g. hit the byte cap on a
	// very long source), then reap them. yt-dlp finishing first also closes the
	// pipe so ffmpeg exits on its own in the common case.
	cancel()
	dlWaitErr := dl.Wait()
	ffWaitErr := ff.Wait()

	if len(wav) == 0 {
		if ctx.Err() == context.DeadlineExceeded {
			return Result{}, errors.New("timed out fetching audio from URL")
		}
		return Result{}, fmt.Errorf("no audio decoded from URL: %s", firstNonEmpty(strings.TrimSpace(dlErr.String()), strings.TrimSpace(ffErr.String()), "yt-dlp/ffmpeg produced no output"))
	}
	if readErr != nil {
		return Result{}, fmt.Errorf("read decoded audio: %w", readErr)
	}
	_ = dlWaitErr
	_ = ffWaitErr

	out, err := analyzeWAVStream(bytes.NewReader(wav), projectTempo)
	if err != nil {
		return Result{}, err
	}
	out.Path = clean
	out.Note = "URL reference analysis: streamed in memory, analyzed, and discarded. Nothing was saved and no melody/notes were extracted. You are responsible for your right to use the source."
	return out, nil
}

// ytDlpArgs streams best available audio to stdout without touching disk.
func ytDlpArgs(u string) []string {
	return []string{
		"-q",
		"--no-playlist",
		"--no-part",
		"-f", "bestaudio/best",
		"-o", "-",
		u,
	}
}

// ffmpegArgs decodes stdin to a full-length mono 44.1kHz WAV stream. Overall
// size is bounded downstream by maxFileBytes rather than a fixed duration.
func ffmpegArgs() []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-ac", "1",
		"-ar", "44100",
		"-f", "wav",
		"pipe:1",
	}
}

func validateAudioURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", errors.New("url must start with http:// or https://")
	}
	if u.Host == "" {
		return "", errors.New("url has no host")
	}
	return u.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
