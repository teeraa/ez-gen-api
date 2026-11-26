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

// dialogueTreatment makes a clean TTS line sit like production sound recorded on set: leading and
// trailing silence removed and long pauses shortened (TTS pads and drags lines), low-cut and high-cut
// like a boom microphone, gentle compression, a short early-reflection room, and a steady loudness
// so every line of a film plays at the same level. %s is the tempo stage.
const dialogueTreatment = "silenceremove=start_periods=1:start_threshold=-50dB:start_silence=0.05:stop_periods=-1:stop_threshold=-50dB:stop_duration=0.3:stop_silence=0.3,%s" +
	"highpass=f=90,lowpass=f=9500," +
	"acompressor=threshold=-22dB:ratio=2.5:attack=8:release=180:makeup=2," +
	"aecho=0.9:0.55:17|31|53:0.16|0.10|0.06," +
	"loudnorm=I=-20:TP=-2:LRA=9"

// TreatDialogue rewrites the audio file at path (any format ffmpeg reads) as an MP3 with the
// dialogue treatment applied.
func TreatDialogue(ctx context.Context, path string) error {
	out := path + ".treated.mp3"
	if e := TreatDialogueFile(ctx, path, out, 1); e != nil {
		return e
	}
	return os.Rename(out, path)
}

// TreatDialogueFile writes src treated (and sped up by tempo, 1-1.5, pitch kept) to dst as MP3.
func TreatDialogueFile(ctx context.Context, src, dst string, tempo float64) error {
	stage := ""
	if finite(tempo) && tempo > 1.001 {
		stage = fmt.Sprintf("atempo=%.3f,", math.Min(tempo, 1.5))
	}
	args := []string{"-v", "error", "-y", "-i", src, "-af", fmt.Sprintf(dialogueTreatment, stage), "-ar", "44100", "-ac", "1", "-c:a", "libmp3lame", "-q:a", "3", dst}
	if b, e := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); e != nil {
		os.Remove(dst)
		return fmt.Errorf("ffmpeg dialogue treatment: %v: %s", e, lastLines(string(b), 6))
	}
	return nil
}
