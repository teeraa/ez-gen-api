package videoprocessing

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MixAudio places audio tracks on a silent timeline of `seconds` and returns a 48 kHz stereo WAV.
// Used to build one dialogue track (every line at its time) as a reference for lip-synced video.
func MixAudio(ctx context.Context, tracks []AudioTrack, seconds float64) ([]byte, error) {
	if len(tracks) == 0 || len(tracks) > 20 || !finite(seconds) || seconds <= 0 || seconds > 60 {
		return nil, fmt.Errorf("invalid audio mix")
	}
	dir, e := os.MkdirTemp("", "audio-mix-*")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-t", fmt.Sprintf("%.3f", seconds), "-i", "anullsrc=r=48000:cl=stereo"}
	parts, inputs := []string{}, "[0:a]"
	for i, t := range tracks {
		if !finite(t.TrimStart) || !finite(t.TrimEnd) || !finite(t.At) || t.TrimStart < 0 || t.TrimEnd <= t.TrimStart || t.At < 0 || t.Volume < 0 || t.Volume > 1 {
			return nil, fmt.Errorf("audio track %d invalid timing or volume", i)
		}
		args = append(args, "-i", t.Path)
		delay := int64(math.Round(t.At * 48000))
		parts = append(parts, fmt.Sprintf("[%d:a:0]atrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%.6f%s,adelay=delays=%dS:all=1[t%d]", i+1, t.TrimStart, t.TrimEnd, t.Volume, audioFadeFilters(t.FadeIn, t.FadeOut, t.TrimEnd-t.TrimStart), delay, i))
		inputs += fmt.Sprintf("[t%d]", i)
	}
	parts = append(parts, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0,%s,atrim=duration=%.6f[out]", inputs, len(tracks)+1, audioLimiter, seconds))
	out := filepath.Join(dir, "mix.wav")
	args = append(args, "-filter_complex", strings.Join(parts, ";"), "-map", "[out]", "-c:a", "pcm_s16le", "-ar", "48000", out)
	if b, e := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); e != nil {
		return nil, fmt.Errorf("ffmpeg audio mix: %v: %s", e, lastLines(string(b), 6))
	}
	return os.ReadFile(out)
}
