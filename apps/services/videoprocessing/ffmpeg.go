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

// FFmpegProcessor implements VideoProcessor using a locally installed ffmpeg binary.
type FFmpegProcessor struct{}

func NewFFmpegProcessor() *FFmpegProcessor {
	return &FFmpegProcessor{}
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

	// Probe clip durations only when text layers are present (needed for timing).
	var clipDurations []float64
	if hasText {
		var err error
		clipDurations, err = probeClipDurations(ctx, req.ClipPaths)
		if err != nil {
			return fmt.Errorf("videoprocessing: probe durations: %w", err)
		}
	}

	args := []string{"-y", "-i", inputPath}
	audioInputIdx := -1
	if hasAudio {
		audioInputIdx = 1
		args = append(args, "-i", *req.AudioPath)
	}

	var fc strings.Builder
	videoOut := "" // empty = no video filter
	audioOut := "" // empty = map 0:a directly

	if hasAudio {
		vol := req.AudioVolume
		if vol <= 0 {
			vol = 1.0
		}
		fmt.Fprintf(&fc, "[0:a][%d:a]amix=inputs=2:duration=first:weights=1 %.2f[fa]", audioInputIdx, vol)
		audioOut = "[fa]"
	}

	if hasText {
		starts := cumulativeStarts(clipDurations)
		if fc.Len() > 0 {
			fc.WriteString(";")
		}
		fc.WriteString("[0:v]")
		for i, tl := range req.TextLayers {
			idx := tl.SceneIdx
			if idx < 0 || idx >= len(starts) {
				continue
			}
			startSec := starts[idx]
			endSec := startSec + clipDurations[idx]
			fc.WriteString(buildDrawtextSegment(tl, startSec, endSec))
			if i < len(req.TextLayers)-1 {
				label := fmt.Sprintf("[tx%d]", i)
				fc.WriteString(label + ";" + label)
			}
		}
		fc.WriteString("[fv]")
		videoOut = "[fv]"
	}

	if fc.Len() > 0 {
		args = append(args, "-filter_complex", fc.String())
	}

	if videoOut != "" {
		args = append(args, "-map", videoOut)
	} else {
		args = append(args, "-map", "0:v")
	}
	if audioOut != "" {
		args = append(args, "-map", audioOut)
	} else {
		args = append(args, "-map", "0:a")
	}

	// Re-encode video only when text overlay is applied (stream copy otherwise).
	if hasText {
		args = append(args, "-c:v", "libx264", "-preset", "fast")
	} else {
		args = append(args, "-c:v", "copy")
	}
	args = append(args, "-c:a", "aac", outPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("videoprocessing: post-process: %w\n%s", err, lastLines(string(out), 10))
	}
	return nil
}

func buildDrawtextSegment(tl TextLayer, startSec, endSec float64) string {
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

	// Commas inside between() must be escaped so FFmpeg doesn't treat them as
	// filter-chain separators.
	return fmt.Sprintf(
		"drawtext=text='%s':x=(w-text_w)/2:y=%s:fontsize=%d:fontcolor=0x%s:enable='between(t\\,%f\\,%f)'",
		text, yExpr, fontSize, color, startSec, endSec,
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
