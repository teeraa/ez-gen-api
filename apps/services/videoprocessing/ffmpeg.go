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

// fontReferenceHeight is the output height the client authors fontSize against.
const fontReferenceHeight = 1080.0

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

// Merge trims and concatenates every clip, then applies audio mixing and text overlays,
// all in a single FFmpeg pass. Per-clip trim forces the concat filter (not the stream-copy
// demuxer), so the output is always re-encoded.
func (p *FFmpegProcessor) Merge(ctx context.Context, req MergeRequest) ([]byte, error) {
	if len(req.Clips) == 0 {
		return nil, fmt.Errorf("videoprocessing: no clips provided")
	}

	tmpDir, err := os.MkdirTemp("", "violet-export-*")
	if err != nil {
		return nil, fmt.Errorf("videoprocessing: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Probe durations and audio presence. Concat requires uniform streams, so clip audio
	// is only used when every clip has it; otherwise clip audio is dropped.
	clipHasAudio := true
	trimStart := make([]float64, len(req.Clips))
	outDur := make([]float64, len(req.Clips))
	for i, c := range req.Clips {
		full, err := probeDuration(ctx, c.Path)
		if err != nil {
			return nil, fmt.Errorf("videoprocessing: probe clip %d: %w", i, err)
		}
		has, err := probeHasAudio(ctx, c.Path)
		if err != nil {
			return nil, fmt.Errorf("videoprocessing: probe audio clip %d: %w", i, err)
		}
		if !has {
			clipHasAudio = false
		}
		ts, te := normalizeTrim(c.TrimStart, c.TrimEnd, full)
		trimStart[i] = ts
		outDur[i] = te - ts
	}
	starts := cumulativeStarts(outDur)

	// Font scaling reference: the first clip's height (concat preserves input dimensions).
	outHeight := 0
	if h, err := probeHeight(ctx, req.Clips[0].Path); err == nil {
		outHeight = h
	}

	hasBgAudio := req.AudioPath != nil
	bgAudioIdx := len(req.Clips)

	args := []string{"-y"}
	for _, c := range req.Clips {
		args = append(args, "-i", c.Path)
	}
	if hasBgAudio {
		args = append(args, "-i", *req.AudioPath)
	}

	var parts []string

	// Trim each clip and reset its timestamps.
	for i := range req.Clips {
		ts := trimStart[i]
		te := ts + outDur[i]
		parts = append(parts, fmt.Sprintf("[%d:v]trim=start=%f:end=%f,setpts=PTS-STARTPTS[v%d]", i, ts, te, i))
		if clipHasAudio {
			parts = append(parts, fmt.Sprintf("[%d:a]atrim=start=%f:end=%f,asetpts=PTS-STARTPTS[a%d]", i, ts, te, i))
		}
	}

	// Concatenate the trimmed segments.
	var concatIn strings.Builder
	for i := range req.Clips {
		concatIn.WriteString(fmt.Sprintf("[v%d]", i))
		if clipHasAudio {
			concatIn.WriteString(fmt.Sprintf("[a%d]", i))
		}
	}
	if clipHasAudio {
		parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[cv][ca]", concatIn.String(), len(req.Clips)))
	} else {
		parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[cv]", concatIn.String(), len(req.Clips)))
	}

	// Text overlays on the concatenated video.
	videoLabel := "[cv]"
	if len(req.TextLayers) > 0 {
		var tb strings.Builder
		tb.WriteString("[cv]")
		written := 0
		for _, tl := range req.TextLayers {
			idx := tl.ClipIdx
			if idx < 0 || idx >= len(starts) {
				continue
			}
			clipStart := starts[idx]
			s, e := layerWindow(tl, clipStart, outDur[idx])
			if written > 0 {
				label := fmt.Sprintf("[tx%d]", written)
				tb.WriteString(label + ";" + label)
			}
			tb.WriteString(buildDrawtextSegment(tl, s, e, outHeight, p.fontFile))
			written++
		}
		if written > 0 {
			tb.WriteString("[fv]")
			parts = append(parts, tb.String())
			videoLabel = "[fv]"
		}
	}

	// Audio routing.
	vol := req.AudioVolume
	if vol <= 0 {
		vol = 1.0
	}
	audioLabel := ""
	switch {
	case clipHasAudio && hasBgAudio:
		parts = append(parts, fmt.Sprintf("[ca][%d:a]amix=inputs=2:duration=first:weights=1 %.2f[fa]", bgAudioIdx, vol))
		audioLabel = "[fa]"
	case clipHasAudio && !hasBgAudio:
		audioLabel = "[ca]"
	case !clipHasAudio && hasBgAudio:
		// Pad background with silence so -shortest trims it to the video length.
		parts = append(parts, fmt.Sprintf("[%d:a]volume=%.2f,apad[fa]", bgAudioIdx, vol))
		audioLabel = "[fa]"
	}

	args = append(args, "-filter_complex", strings.Join(parts, ";"))
	args = append(args, "-map", videoLabel)
	if audioLabel != "" {
		args = append(args, "-map", audioLabel)
	}
	args = append(args, "-c:v", "libx264", "-preset", "fast", "-pix_fmt", "yuv420p")
	if audioLabel != "" {
		args = append(args, "-c:a", "aac")
	}
	if !clipHasAudio && hasBgAudio {
		args = append(args, "-shortest")
	}

	outPath := filepath.Join(tmpDir, "final.mp4")
	args = append(args, outPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("videoprocessing: merge: %w\n%s", err, lastLines(string(out), 12))
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("videoprocessing: read output: %w", err)
	}
	return data, nil
}

// normalizeTrim clamps the trim window to the clip's real duration.
func normalizeTrim(ts, te, full float64) (float64, float64) {
	if ts < 0 {
		ts = 0
	}
	if te <= 0 || te > full {
		te = full
	}
	if ts >= te {
		ts = 0
	}
	return ts, te
}

// layerWindow returns the absolute [start,end] of a text layer in the merged video.
func layerWindow(tl TextLayer, clipStart, clipDur float64) (float64, float64) {
	if tl.StartTime == 0 && tl.EndTime == 0 {
		return clipStart, clipStart + clipDur
	}
	s := clipStart + tl.StartTime
	e := clipStart + tl.EndTime
	if e <= s || tl.EndTime > clipDur {
		e = clipStart + clipDur
	}
	return s, e
}

func buildDrawtextSegment(tl TextLayer, startSec, endSec float64, outHeight int, fontFile string) string {
	fontSize := tl.FontSize
	if fontSize <= 0 {
		fontSize = 48
	}
	if outHeight > 0 {
		fontSize = int(float64(fontSize)*float64(outHeight)/fontReferenceHeight + 0.5)
		if fontSize < 1 {
			fontSize = 1
		}
	}
	color := tl.Color
	if color == "" {
		color = "FFFFFF"
	}

	var xExpr, yExpr string
	if tl.HasXY {
		switch tl.Align {
		case "left":
			xExpr = fmt.Sprintf("w*%f", tl.X)
		case "right":
			xExpr = fmt.Sprintf("w*%f-text_w", tl.X)
		default:
			xExpr = fmt.Sprintf("w*%f-text_w/2", tl.X)
		}
		yExpr = fmt.Sprintf("h*%f-text_h/2", tl.Y)
	} else {
		xExpr = "(w-text_w)/2"
		switch tl.Position {
		case "top":
			yExpr = "20"
		case "center":
			yExpr = "(h-text_h)/2"
		default:
			yExpr = "h-text_h-30"
		}
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
		"drawtext=%stext='%s':x=%s:y=%s:fontsize=%d:fontcolor=0x%s:enable='between(t\\,%f\\,%f)'",
		fontOpt, text, xExpr, yExpr, fontSize, color, startSec, endSec,
	)
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

// probeHeight returns the video height of the first video stream.
func probeHeight(ctx context.Context, path string) (int, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=height",
		"-print_format", "json",
		path)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe height: %w", err)
	}
	var result struct {
		Streams []struct {
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, fmt.Errorf("ffprobe height parse: %w", err)
	}
	if len(result.Streams) == 0 {
		return 0, fmt.Errorf("ffprobe height: no video stream")
	}
	return result.Streams[0].Height, nil
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
