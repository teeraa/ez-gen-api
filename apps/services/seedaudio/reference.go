package seedaudio

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Reference clip limits from the seed-audio-1.0 docs: up to 30 s and 10 MB per clip.
const (
	MaxReferenceBytes   = 10 << 20
	MaxReferenceSeconds = 30.0
	// Shorter clips give the model too little of the voice to copy.
	MinReferenceSeconds = 5.0
	// MP3 frames overshoot -t slightly (30 s becomes ~30.05 s), so trim a little under the cap.
	referenceTrimSeconds = MaxReferenceSeconds - 0.5
)

var (
	// ErrReferenceInvalid: the upload is not readable audio.
	ErrReferenceInvalid = errors.New("reference audio is not readable")
	// ErrReferenceShort: readable audio, but shorter than MinReferenceSeconds.
	ErrReferenceShort = errors.New("reference audio is too short")
	// ErrReferenceTool: ffmpeg/ffprobe is not installed on the server.
	ErrReferenceTool = errors.New("ffmpeg is not available")
)

// ReferenceToolsAvailable reports whether PrepareReference can run.
func ReferenceToolsAvailable() bool {
	_, a := exec.LookPath("ffmpeg")
	_, b := exec.LookPath("ffprobe")
	return a == nil && b == nil
}

// PrepareReference turns any readable upload or browser recording (webm, ogg, m4a, wav, mp3…)
// into a mono 24 kHz MP3 of at most MaxReferenceSeconds, which every seed-audio
// reference accepts. It returns the MP3 and its duration in seconds.
func PrepareReference(ctx context.Context, data []byte) ([]byte, float64, error) {
	if !ReferenceToolsAvailable() {
		return nil, 0, ErrReferenceTool
	}
	if len(data) == 0 || len(data) > MaxReferenceBytes*3 {
		return nil, 0, ErrReferenceInvalid
	}
	dir, e := os.MkdirTemp("", "voice-ref-")
	if e != nil {
		return nil, 0, e
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "input"), filepath.Join(dir, "reference.mp3")
	if e = os.WriteFile(in, data, 0o600); e != nil {
		return nil, 0, e
	}
	// Only the first audio stream is used; video and cover art are dropped.
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-protocol_whitelist", "file,pipe", "-i", in,
		"-map", "0:a:0", "-vn", "-t", strconv.FormatFloat(referenceTrimSeconds, 'f', 1, 64),
		"-ac", "1", "-ar", "24000", "-c:a", "libmp3lame", "-b:a", "128k", "-threads", "1", "-y", out)
	if cmd.Run() != nil {
		return nil, 0, ErrReferenceInvalid
	}
	raw, e := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", out).Output()
	if e != nil {
		return nil, 0, ErrReferenceInvalid
	}
	seconds, e := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if e != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return nil, 0, ErrReferenceInvalid
	}
	if seconds < MinReferenceSeconds {
		return nil, 0, ErrReferenceShort
	}
	clip, e := os.ReadFile(out)
	if e != nil || len(clip) == 0 || len(clip) > MaxReferenceBytes {
		return nil, 0, ErrReferenceInvalid
	}
	return clip, math.Min(seconds, MaxReferenceSeconds), nil
}
