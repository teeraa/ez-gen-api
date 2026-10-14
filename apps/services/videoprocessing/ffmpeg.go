package videoprocessing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultFontFile is where the Alpine ttf-dejavu package installs DejaVu Sans.
// drawtext needs an explicit font file because the runtime image has no fontconfig.
const DefaultFontFile = "/usr/share/fonts/dejavu/DejaVuSans.ttf"

// FFmpegProcessor implements VideoProcessor using a locally installed ffmpeg binary.
type FFmpegProcessor struct {
	fontFile string
}

// NewFFmpegProcessor uses fontFile for text overlays; an empty string falls back to
// DefaultFontFile.
func NewFFmpegProcessor(fontFile string) *FFmpegProcessor {
	if fontFile == "" {
		fontFile = DefaultFontFile
	}
	return &FFmpegProcessor{fontFile: fontFile}
}

func (p *FFmpegProcessor) Merge(ctx context.Context, req MergeRequest) ([]byte, error) {
	if len(req.ClipPaths) == 0 {
		return nil, fmt.Errorf("videoprocessing: no clips provided")
	}

	tmpDir, err := os.MkdirTemp("", "violet-export-*")
	if err != nil {
		return nil, fmt.Errorf("videoprocessing: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	mergedPath := filepath.Join(tmpDir, "merged.mp4")
	if err := p.concatClips(ctx, req.ClipPaths, tmpDir, mergedPath); err != nil {
		return nil, err
	}

	outPath := mergedPath
	if req.AudioPath != nil || len(req.TextLayers) > 0 {
		finalPath := filepath.Join(tmpDir, "final.mp4")
		if err := p.postProcess(ctx, mergedPath, req, finalPath); err != nil {
			return nil, err
		}
		outPath = finalPath
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("videoprocessing: read output: %w", err)
	}
	return data, nil
}

// concatClips joins clips using the concat demuxer with stream copy (fast, no re-encode).
func (p *FFmpegProcessor) concatClips(ctx context.Context, clips []string, tmpDir, outPath string) error {
	listPath := filepath.Join(tmpDir, "clips.txt")
	var b strings.Builder
	for _, c := range clips {
		b.WriteString("file '")
		b.WriteString(strings.ReplaceAll(c, "'", `'\''`))
		b.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0600); err != nil {
		return fmt.Errorf("videoprocessing: write concat list: %w", err)
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy",
		outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("videoprocessing: concat clips: %w\n%s", err, lastLines(string(out), 10))
	}
	return nil
}

// postProcess applies audio mixing and/or text burn-in in a single FFmpeg pass.
func (p *FFmpegProcessor) postProcess(ctx context.Context, inputPath string, req MergeRequest, outPath string) error {
	hasAudio := req.AudioPath != nil
	hasText := len(req.TextLayers) > 0

	// Generated clips are often video-only; probe so we never map a missing audio stream.
	clipHasAudio, err := probeHasAudio(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("videoprocessing: probe audio: %w", err)
	}

	// Probe clip durations only when text layers are present (needed for timing).
	var clipDurations []float64
	if hasText {
		clipDurations, err = probeClipDurations(ctx, req.ClipPaths)
		if err != nil {
			return fmt.Errorf("videoprocessing: probe durations: %w", err)
		}
	}

	vol := req.AudioVolume
	if vol <= 0 {
		vol = 1.0
	}

	args := []string{"-y", "-i", inputPath}
	if hasAudio {
		args = append(args, "-i", *req.AudioPath)
	}

	var filters []string
	videoOut := ""      // filter output label for video, "" = map 0:v directly
	audioOut := ""      // filter output label for audio, "" = no filter
	audioMapDirect := "" // direct stream map for audio, "" = none

	// ── Audio routing ──────────────────────────────────────────────────────────
	switch {
	case clipHasAudio && hasAudio:
		// Mix clip audio with background; cap length to the clip audio.
		filters = append(filters, fmt.Sprintf("[0:a][1:a]amix=inputs=2:duration=first:weights=1 %.2f[fa]", vol))
		audioOut = "[fa]"
	case clipHasAudio && !hasAudio:
		audioMapDirect = "0:a"
	case !clipHasAudio && hasAudio:
		// No clip audio: pad background with silence so -shortest trims it to the video.
		filters = append(filters, fmt.Sprintf("[1:a]volume=%.2f,apad[fa]", vol))
		audioOut = "[fa]"
	}
	// else: no audio anywhere → output has no audio track.

	// ── Text overlay ────────────────────────────────────────────────────────────
	if hasText {
		starts := cumulativeStarts(clipDurations)
		var fc strings.Builder
		fc.WriteString("[0:v]")
		for i, tl := range req.TextLayers {
			idx := tl.SceneIdx
			if idx < 0 || idx >= len(starts) {
				continue
			}
			startSec := starts[idx]
			endSec := startSec + clipDurations[idx]
			fc.WriteString(buildDrawtextSegment(tl, startSec, endSec, p.fontFile))
			if i < len(req.TextLayers)-1 {
				label := fmt.Sprintf("[tx%d]", i)
				fc.WriteString(label + ";" + label)
			}
		}
		fc.WriteString("[fv]")
		filters = append(filters, fc.String())
		videoOut = "[fv]"
	}

	if len(filters) > 0 {
		args = append(args, "-filter_complex", strings.Join(filters, ";"))
	}

	// Map video.
	if videoOut != "" {
		args = append(args, "-map", videoOut)
	} else {
		args = append(args, "-map", "0:v")
	}
	// Map audio (only when there is one).
	switch {
	case audioOut != "":
		args = append(args, "-map", audioOut)
	case audioMapDirect != "":
		args = append(args, "-map", audioMapDirect)
	}

	// Re-encode video only when a text overlay is applied (stream copy otherwise).
	if hasText {
		args = append(args, "-c:v", "libx264", "-preset", "fast")
	} else {
		args = append(args, "-c:v", "copy")
	}
	if audioOut != "" || audioMapDirect != "" {
		args = append(args, "-c:a", "aac")
	}
	// apad produces an infinite stream; -shortest stops at the video's end.
	if !clipHasAudio && hasAudio {
		args = append(args, "-shortest")
	}
	args = append(args, outPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("videoprocessing: post-process: %w\n%s", err, lastLines(string(out), 10))
	}
	return nil
}

func buildDrawtextSegment(tl TextLayer, startSec, endSec float64, fontFile string) string {
	fontSize := tl.FontSize
	if fontSize <= 0 {
		fontSize = 48
	}
	color := tl.Color
	if color == "" {
		color = "FFFFFF"
	}
	var yExpr string
	switch tl.Position {
	case "top":
		yExpr = "20"
	case "center":
		yExpr = "(h-text_h)/2"
	default:
		yExpr = "h-text_h-30"
	}

	// Escape single-quotes and colons for FFmpeg's filter parser.
	text := strings.ReplaceAll(tl.Text, `'`, `'\''`)
	text = strings.ReplaceAll(text, `:`, `\:`)

	fontOpt := ""
	if fontFile != "" {
		fontOpt = fmt.Sprintf("fontfile='%s':", fontFile)
	}

	// Commas inside between() must be escaped so FFmpeg doesn't treat them as
	// filter-chain separators.
	return fmt.Sprintf(
		"drawtext=%stext='%s':x=(w-text_w)/2:y=%s:fontsize=%d:fontcolor=0x%s:enable='between(t\\,%f\\,%f)'",
		fontOpt, text, yExpr, fontSize, color, startSec, endSec,
	)
}

// probeClipDurations runs ffprobe on each clip and returns its duration in seconds.
func probeClipDurations(ctx context.Context, paths []string) ([]float64, error) {
	durations := make([]float64, len(paths))
	for i, p := range paths {
		d, err := probeDuration(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("probe %q: %w", p, err)
		}
		durations[i] = d
	}
	return durations, nil
}

// probeHasAudio reports whether the file has at least one audio stream.
func probeHasAudio(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-select_streams", "a",
		"-show_entries", "stream=index",
		"-print_format", "json",
		path)
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("ffprobe audio: %w", err)
	}
	var result struct {
		Streams []struct {
			Index int `json:"index"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return false, fmt.Errorf("ffprobe audio parse: %w", err)
	}
	return len(result.Streams) > 0, nil
}

func probeDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		path)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, fmt.Errorf("ffprobe parse: %w", err)
	}
	var dur float64
	if _, err := fmt.Sscanf(result.Format.Duration, "%f", &dur); err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w", err)
	}
	return dur, nil
}

// cumulativeStarts returns the start time of each clip in the merged video.
func cumulativeStarts(durations []float64) []float64 {
	starts := make([]float64, len(durations))
	var acc float64
	for i, d := range durations {
		starts[i] = acc
		acc += d
	}
	return starts
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
