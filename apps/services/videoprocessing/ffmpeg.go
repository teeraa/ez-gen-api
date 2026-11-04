package videoprocessing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultFontFile = "/usr/share/fonts/dejavu/DejaVuSans.ttf"
const fontReferenceHeight = 1080.0

type FFmpegProcessor struct{ fontFile string }

func NewFFmpegProcessor(fontFile string) *FFmpegProcessor {
	if fontFile == "" {
		fontFile = DefaultFontFile
	}
	return &FFmpegProcessor{fontFile: fontFile}
}

// OutputDimensions uses the short edge for resolution, including portrait outputs.
func OutputDimensions(resolution, ratio string) (int, int, error) {
	edge := 720
	switch resolution {
	case "", "720p":
	case "1080p":
		edge = 1080
	default:
		return 0, 0, fmt.Errorf("unsupported output resolution")
	}
	if ratio == "" {
		ratio = "16:9"
	}
	ratios := map[string][2]int{"16:9": {16, 9}, "9:16": {9, 16}, "1:1": {1, 1}, "4:3": {4, 3}, "3:4": {3, 4}, "21:9": {21, 9}}
	r, ok := ratios[ratio]
	if !ok {
		return 0, 0, fmt.Errorf("unsupported output aspect ratio")
	}
	if r[0] >= r[1] {
		return (edge * r[0] / r[1]) / 2 * 2, edge, nil
	}
	return edge, (edge * r[1] / r[0]) / 2 * 2, nil
}

func (p *FFmpegProcessor) Merge(ctx context.Context, req MergeRequest) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if len(req.Clips) < 1 || len(req.Clips) > 20 {
		return nil, fmt.Errorf("videoprocessing: require 1–20 clips")
	}
	w, h, err := OutputDimensions(req.OutputResolution, req.OutputAspectRatio)
	if err != nil {
		return nil, err
	}
	if !finite(req.AudioVolume) || req.AudioVolume < 0 || req.AudioVolume > 2 {
		return nil, fmt.Errorf("invalid background audio volume")
	}
	dir, err := os.MkdirTemp("", "violet-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	// sources are trimmed source seconds; durations are output seconds after speed.
	sources := make([]float64, len(req.Clips))
	durations := make([]float64, len(req.Clips))
	starts := make([]float64, len(req.Clips))
	hasAudio := make([]bool, len(req.Clips))
	total := 0.0
	for i, c := range req.Clips {
		if c.Volume != nil && (!finite(*c.Volume) || *c.Volume < 0 || *c.Volume > 1) {
			return nil, fmt.Errorf("clip %d invalid audio volume", i)
		}
		info, e := probe(ctx, c.Path)
		if e != nil {
			return nil, fmt.Errorf("probe clip %d: %w", i, e)
		}
		if info.Width == 0 || info.Height == 0 {
			return nil, fmt.Errorf("clip %d has no video", i)
		}
		end := c.TrimEnd
		if end == 0 {
			end = info.Duration
		}
		if !finite(c.TrimStart) || !finite(end) || c.TrimStart < 0 || end > info.Duration+0.001 || c.TrimStart >= end {
			return nil, fmt.Errorf("clip %d invalid trim window", i)
		}
		if !finite(c.Speed) || c.Speed != 0 && (c.Speed < 0.25 || c.Speed > 4) {
			return nil, fmt.Errorf("clip %d invalid speed", i)
		}
		sources[i] = end - c.TrimStart
		durations[i] = sources[i] / clipSpeed(c.Speed)
		if !validFades(c.FadeIn, c.FadeOut, durations[i]) {
			return nil, fmt.Errorf("clip %d invalid fade", i)
		}
		if !validLook(c.Look) {
			return nil, fmt.Errorf("clip %d invalid look", i)
		}
		if !validTransition(req.Clips, durations, i) {
			return nil, fmt.Errorf("clip %d invalid transition", i)
		}
		// A transition starts this clip that much before the previous one ends.
		starts[i] = total - c.TransitionDuration
		total += durations[i] - c.TransitionDuration
		hasAudio[i] = info.Audio
	}
	if total > 1200 {
		return nil, fmt.Errorf("export exceeds 20 minutes")
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-filter_complex_threads", "1"}
	for _, c := range req.Clips {
		args = append(args, "-i", c.Path)
	}
	bg := req.AudioPath != nil
	if bg {
		info, e := probe(ctx, *req.AudioPath)
		if e != nil || !info.Audio {
			return nil, fmt.Errorf("invalid background audio")
		}
		args = append(args, "-i", *req.AudioPath)
	}
	if len(req.AudioTracks) > 20 {
		return nil, fmt.Errorf("too many audio tracks")
	}
	next := len(req.Clips)
	if bg {
		next++
	}
	tracks := []audioInput{}
	for i, t := range req.AudioTracks {
		if !finite(t.TrimStart) || !finite(t.TrimEnd) || !finite(t.At) || !finite(t.Volume) || t.TrimStart < 0 || t.TrimEnd <= t.TrimStart || t.At < 0 || t.Volume < 0 || t.Volume > 1 {
			return nil, fmt.Errorf("audio track %d invalid timing or volume", i)
		}
		if !validFades(t.FadeIn, t.FadeOut, t.TrimEnd-t.TrimStart) {
			return nil, fmt.Errorf("audio track %d invalid fade", i)
		}
		// Tracks with less than 1 ms left before the end would yield an empty atrim.
		if total-t.At < minTimelineItem-1e-9 {
			continue
		}
		info, e := probe(ctx, t.Path)
		if e != nil || !info.Audio || t.TrimEnd > info.Duration+0.001 {
			return nil, fmt.Errorf("audio track %d invalid source", i)
		}
		args = append(args, "-i", t.Path)
		tracks = append(tracks, audioInput{index: next, track: t})
		next++
	}
	parts := []string{}
	for i, c := range req.Clips {
		parts = append(parts, fmt.Sprintf("[%d:v:0]trim=start=%.6f:duration=%.6f,%s,%s,setsar=1,fps=30,format=yuv420p%s%s[v%d]", i, c.TrimStart, sources[i], videoSpeedFilter(c.Speed), frameFilter(c.Look, w, h), lookFilter(c.Look), videoFadeFilters(c.FadeIn, c.FadeOut, durations[i]), i))
		if hasAudio[i] {
			volume := 1.0
			if c.Volume != nil {
				volume = *c.Volume
			}
			parts = append(parts, fmt.Sprintf("[%d:a:0]atrim=start=%.6f:duration=%.6f,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo%s,volume=%.6f%s,apad,atrim=duration=%.6f[a%d]", i, c.TrimStart, sources[i], audioSpeedFilters(c.Speed), volume, audioFadeFilters(c.FadeIn, c.FadeOut, durations[i]), durations[i], i))
		} else {
			parts = append(parts, fmt.Sprintf("anullsrc=r=48000:cl=stereo,atrim=duration=%.6f[a%d]", durations[i], i))
		}
	}
	parts = append(parts, joinClips(req.Clips, durations)...)
	video := "[cv]"
	if len(req.ImageLayers) > 20 {
		return nil, fmt.Errorf("too many image layers")
	}
	for i, im := range req.ImageLayers {
		if !validImageLayer(im, total) {
			return nil, fmt.Errorf("image layer %d invalid", i)
		}
		// Stills have no duration for probe; the upload already decoded the picture.
		if st, e := os.Stat(im.Path); e != nil || st.IsDir() || st.Size() == 0 {
			return nil, fmt.Errorf("image layer %d invalid source", i)
		}
		args = append(args, "-loop", "1", "-framerate", "30", "-i", im.Path)
		width := max(2, int(math.Round(float64(w)*im.Width/2))*2)
		parts = append(parts,
			fmt.Sprintf("[%d:v:0]format=rgba,scale=%d:-2,colorchannelmixer=aa=%.6f[im%d]", next, width, im.Opacity, i),
			fmt.Sprintf("%s[im%d]overlay=x='W*%.6f-w/2':y='H*%.6f-h/2':enable='between(t,%f,%f)':eof_action=repeat[iv%d]", video, i, im.X, im.Y, im.StartTime, math.Min(im.EndTime, total), i))
		video = fmt.Sprintf("[iv%d]", i)
		next++
	}
	if len(req.TextLayers) > 0 && !p.SupportsTextOverlays(ctx) {
		return nil, fmt.Errorf("text overlays unavailable: install FFmpeg drawtext and configure a readable font")
	}
	if len(req.TextLayers) > 100 {
		return nil, fmt.Errorf("too many text layers")
	}
	fontPath := ""
	if len(req.TextLayers) > 0 {
		for _, tl := range req.TextLayers {
			if tl.EditorStyle && !p.supportsEditorText(ctx) {
				return nil, fmt.Errorf("editor text overlays unavailable: FFmpeg drawtext lacks text_align/y_align (needs FFmpeg 6.1 or newer)")
			}
		}
		// Copying the trusted configured font once gives every filter a parser-safe
		// path. Text stays in files with expansion disabled, never in filter syntax.
		fontData, e := os.ReadFile(p.fontFile)
		if e != nil {
			return nil, fmt.Errorf("text overlay font unavailable")
		}
		fontPath = filepath.Join(dir, "font.ttf")
		if e = os.WriteFile(fontPath, fontData, 0600); e != nil {
			return nil, e
		}
	}
	for i, tl := range req.TextLayers {
		if !tl.Absolute && (tl.ClipIdx < 0 || tl.ClipIdx >= len(req.Clips)) || len(tl.Text) > 8192 {
			return nil, fmt.Errorf("invalid text layer")
		}
		if !validTextTiming(tl, durations, total) {
			return nil, fmt.Errorf("invalid text timing")
		}
		if tl.EditorStyle && !tl.HasXY {
			return nil, fmt.Errorf("invalid text position")
		}
		if tl.HasXY && (!finite(tl.X) || !finite(tl.Y) || tl.X < 0 || tl.X > 1 || tl.Y < 0 || tl.Y > 1) {
			return nil, fmt.Errorf("invalid text position")
		}
		if tl.FontSize < 0 || tl.FontSize > 500 {
			return nil, fmt.Errorf("invalid font size")
		}
		if !validTextStyle(tl) || !tl.EditorStyle && (tl.OutlineWidth != 0 || tl.BoxOpacity != 0 || tl.Animation != "") {
			return nil, fmt.Errorf("invalid text style")
		}
		if tl.Color != "" && !regexp.MustCompile(`^[0-9a-fA-F]{6}$`).MatchString(tl.Color) {
			return nil, fmt.Errorf("invalid text color")
		}
		textPath := filepath.Join(dir, fmt.Sprintf("text-%d.txt", i))
		if e := os.WriteFile(textPath, []byte(tl.Text), 0600); e != nil {
			return nil, e
		}
		start, end := textWindow(tl, starts, durations, total)
		label := fmt.Sprintf("[tx%d]", i)
		if !validTextScale(tl.ScaleX) || !validTextScale(tl.ScaleY) {
			return nil, fmt.Errorf("invalid text scale")
		}
		if tl.EditorStyle && (textScale(tl.ScaleX) != 1 || textScale(tl.ScaleY) != 1) {
			imagePath := filepath.Join(dir, fmt.Sprintf("text-%d.png", i))
			box, padX, padY, e := renderScaledEditorText(ctx, tl, w, h, fontPath, textPath, imagePath)
			if e != nil {
				return nil, e
			}
			args = append(args, "-loop", "1", "-framerate", "30", "-i", imagePath)
			x, y := scaledTextPosition(tl, box, w, h)
			x, y = x-padX, y-padY
			source, yExpr := fmt.Sprintf("[%d:v:0]", next), fmt.Sprintf("%.6f", y)
			if tl.Animation != "" {
				d := textAnimDuration(start, end)
				parts = append(parts, fmt.Sprintf("%sformat=rgba,fade=t=in:st=%f:d=%f:alpha=1,fade=t=out:st=%f:d=%f:alpha=1[ta%d]", source, start, d, end-d, d, i))
				source = fmt.Sprintf("[ta%d]", i)
				if tl.Animation == "slide" {
					yExpr = fmt.Sprintf("'%.6f+%d*%f*max(0,1-(t-%f)/%f)'", y, h, textSlideRise, start, d)
				}
			}
			parts = append(parts, fmt.Sprintf("%s%soverlay=x=%.6f:y=%s:enable='between(t,%f,%f)':eof_action=repeat%s", video, source, x, yExpr, start, end, label))
			next++
		} else {
			parts = append(parts, video+buildDrawtextFileSegment(tl, start, end, h, fontPath, textPath)+label)
		}
		video = label
	}
	bgIndex := -1
	if bg {
		bgIndex = len(req.Clips)
	}
	mixParts, audio := audioMixParts(bgIndex, req.AudioVolume, total, tracks)
	parts = append(parts, mixParts...)
	output := filepath.Join(dir, "final.mp4")
	args = append(args, "-filter_complex", strings.Join(parts, ";"), "-map", video, "-map", audio, "-c:v", "libx264", "-preset", "fast", "-threads", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ar", "48000", "-movflags", "+faststart", "-t", fmt.Sprintf("%.6f", total), output)
	if out, e := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); e != nil {
		return nil, fmt.Errorf("videoprocessing: merge: %w: %s", e, lastLines(string(out), 8))
	}
	info, err := probe(ctx, output)
	if err != nil || info.Width != w || info.Height != h || !info.Audio || math.Abs(info.Duration-total) > 0.15 {
		return nil, fmt.Errorf("videoprocessing: invalid rendered output")
	}
	// Probe alone can accept truncated media; decode every frame before publishing.
	if e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-i", output, "-f", "null", "-").Run(); e != nil {
		return nil, fmt.Errorf("videoprocessing: output decode failed: %w", e)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	if len(data) < 32 {
		return nil, fmt.Errorf("empty rendered output")
	}
	return data, nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func clipSpeed(speed float64) float64 {
	if speed == 0 {
		return 1
	}
	return speed
}

// validFades: each fade is 0..10 s and both fit inside the span (1 ms tolerance).
func validFades(in, out, length float64) bool {
	return finite(in) && finite(out) && in >= 0 && out >= 0 && in <= 10 && out <= 10 && in+out <= length+0.001
}

// videoSpeedFilter resets timestamps and, off 1×, rescales them so the clip plays faster or slower.
func videoSpeedFilter(speed float64) string {
	speed = clipSpeed(speed)
	if speed == 1 {
		return "setpts=PTS-STARTPTS"
	}
	return fmt.Sprintf("setpts=(PTS-STARTPTS)/%.6f", speed)
}

// audioSpeedFilters keeps pitch with atempo. Each stage stays within 0.5–2, the
// range every supported FFmpeg accepts, so 0.25× and 4× become two stages.
func audioSpeedFilters(speed float64) string {
	speed = clipSpeed(speed)
	if speed == 1 {
		return ""
	}
	stages := []string{}
	for speed > 2 {
		stages = append(stages, "atempo=2")
		speed /= 2
	}
	for speed < 0.5 {
		stages = append(stages, "atempo=0.5")
		speed /= 0.5
	}
	if math.Abs(speed-1) > 1e-9 {
		stages = append(stages, fmt.Sprintf("atempo=%.6f", speed))
	}
	return "," + strings.Join(stages, ",")
}

var xfadeTransitions = map[string]bool{"fade": true, "fadeblack": true, "fadewhite": true, "wipeleft": true, "wiperight": true, "slideleft": true, "slideright": true, "circleopen": true}

// validTransition: none on the first clip; else a known xfade lasting 0.1..2 s and at most
// half of this and the previous clip (so two transitions never overlap inside one clip).
func validTransition(clips []Clip, durations []float64, i int) bool {
	c := clips[i]
	if c.Transition == "" {
		return c.TransitionDuration == 0
	}
	if i == 0 || !xfadeTransitions[c.Transition] || !finite(c.TransitionDuration) || c.TransitionDuration < 0.1 || c.TransitionDuration > 2 {
		return false
	}
	return c.TransitionDuration <= math.Min(durations[i], durations[i-1])/2+0.001
}

// joinClips concatenates [vN][aN] into [cv][ca]. Without transitions it is the legacy
// single concat; otherwise clips are folded left, using xfade+acrossfade where a clip
// has a transition and a two-input concat where it does not.
func joinClips(clips []Clip, durations []float64) []string {
	transitions := false
	for _, c := range clips {
		transitions = transitions || c.Transition != ""
	}
	if !transitions {
		var inputs strings.Builder
		for i := range clips {
			fmt.Fprintf(&inputs, "[v%d][a%d]", i, i)
		}
		return []string{fmt.Sprintf("%sconcat=n=%d:v=1:a=1[cv][ca]", inputs.String(), len(clips))}
	}
	parts := []string{}
	video, audio, length := "[v0]", "[a0]", durations[0]
	for i := 1; i < len(clips); i++ {
		c := clips[i]
		if c.Transition != "" {
			parts = append(parts,
				fmt.Sprintf("%s[v%d]xfade=transition=%s:duration=%.6f:offset=%.6f[jv%d]", video, i, c.Transition, c.TransitionDuration, length-c.TransitionDuration, i),
				fmt.Sprintf("%s[a%d]acrossfade=d=%.6f:c1=tri:c2=tri[ja%d]", audio, i, c.TransitionDuration, i))
		} else {
			parts = append(parts, fmt.Sprintf("%s%s[v%d][a%d]concat=n=2:v=1:a=1[jv%d][ja%d]", video, audio, i, i, i, i))
		}
		video, audio = fmt.Sprintf("[jv%d]", i), fmt.Sprintf("[ja%d]", i)
		length += durations[i] - c.TransitionDuration
	}
	return append(parts, video+"null[cv]", audio+"anull[ca]")
}

// validImageLayer: centre inside the frame, width 2..100%, opacity 0.05..1, starting before the end.
func validImageLayer(im ImageLayer, total float64) bool {
	for _, v := range []float64{im.StartTime, im.EndTime, im.X, im.Y, im.Width, im.Opacity} {
		if !finite(v) {
			return false
		}
	}
	return im.StartTime >= 0 && im.EndTime > im.StartTime && im.StartTime < total && im.X >= 0 && im.X <= 1 && im.Y >= 0 && im.Y <= 1 && im.Width >= .02 && im.Width <= 1 && im.Opacity >= .05 && im.Opacity <= 1
}

func validLook(l ClipLook) bool {
	for _, v := range []float64{l.Brightness, l.Contrast, l.Saturation, l.PanX, l.PanY} {
		if !finite(v) || v < -1 || v > 1 {
			return false
		}
	}
	return finite(l.Zoom) && (l.Zoom == 0 || l.Zoom >= 1 && l.Zoom <= 3) && (l.Fit == "" || l.Fit == "cover")
}

// frameFilter places the clip in the w×h output. The default keeps the legacy letterbox
// string; cover/zoom/pan scale by min (contain) or max (cover) of the axis ratios times
// zoom, pad anything still smaller than the frame, then crop at the pan position.
func frameFilter(l ClipLook, w, h int) string {
	zoom := l.Zoom
	if zoom == 0 {
		zoom = 1
	}
	if l.Fit == "" && zoom == 1 {
		return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", w, h, w, h)
	}
	pick := "min"
	if l.Fit == "cover" {
		pick = "max"
	}
	factor := fmt.Sprintf("%s(%d/iw,%d/ih)*%.6f", pick, w, h, zoom)
	return fmt.Sprintf("scale=w='max(2,trunc(iw*%s/2)*2)':h='max(2,trunc(ih*%s/2)*2)',pad=w='max(iw,%d)':h='max(ih,%d)':x='(ow-iw)/2':y='(oh-ih)/2',crop=%d:%d:x='(iw-%d)*%.6f':y='(ih-%d)*%.6f'",
		factor, factor, w, h, w, h, w, 0.5+l.PanX/2, h, 0.5+l.PanY/2)
}

// lookFilter maps the -1..1 adjustments onto eq; unchanged clips add no filter.
func lookFilter(l ClipLook) string {
	if l.Brightness == 0 && l.Contrast == 0 && l.Saturation == 0 {
		return ""
	}
	return fmt.Sprintf(",eq=brightness=%.6f:contrast=%.6f:saturation=%.6f", l.Brightness*0.3, 1+l.Contrast*0.5, 1+l.Saturation)
}

func videoFadeFilters(in, out, length float64) string {
	filters := ""
	if in > 0 {
		filters += fmt.Sprintf(",fade=t=in:st=0:d=%.6f:color=black", in)
	}
	if out > 0 {
		filters += fmt.Sprintf(",fade=t=out:st=%.6f:d=%.6f:color=black", math.Max(0, length-out), out)
	}
	return filters
}

func audioFadeFilters(in, out, length float64) string {
	filters := ""
	if in > 0 {
		filters += fmt.Sprintf(",afade=t=in:st=0:d=%.6f", in)
	}
	if out > 0 {
		filters += fmt.Sprintf(",afade=t=out:st=%.6f:d=%.6f", math.Max(0, length-out), out)
	}
	return filters
}

type audioInput struct {
	index int
	track AudioTrack
}

// minTimelineItem is the shortest audio/text span rendered (1 ms).
const minTimelineItem = 0.001

// audioLimiter caps summed timeline audio below full scale. level=0 disables
// alimiter's auto make-up gain so unclipped material keeps its exact level, and
// latency=1 compensates the lookahead so audio stays in sync.
const audioLimiter = "alimiter=limit=0.95:level=0:latency=1"

// audioMixParts mixes the concatenated clip audio [ca] with the optional
// background input and timeline audio tracks. Output length follows [ca], so
// audio extending past the video is cut. normalize=0 keeps per-source volumes
// exact instead of dividing by the number of inputs; when timeline tracks are
// present a limiter follows the mix so overlapping loud sources cannot clip. The
// storyboard background-only graph is unchanged (no limiter).
func audioMixParts(bgIndex int, bgVolume, total float64, tracks []audioInput) ([]string, string) {
	parts := []string{}
	inputs := "[ca]"
	if bgIndex >= 0 {
		parts = append(parts, fmt.Sprintf("[%d:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%.6f,apad,atrim=duration=%.6f[bg]", bgIndex, bgVolume, total))
		inputs += "[bg]"
	}
	for i, t := range tracks {
		delay := int64(math.Round(t.track.At * 48000))
		parts = append(parts, fmt.Sprintf("[%d:a:0]atrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%.6f%s,adelay=delays=%dS:all=1[au%d]", t.index, t.track.TrimStart, t.track.TrimEnd, t.track.Volume, audioFadeFilters(t.track.FadeIn, t.track.FadeOut, t.track.TrimEnd-t.track.TrimStart), delay, i))
		inputs += fmt.Sprintf("[au%d]", i)
	}
	n := len(tracks)
	if bgIndex >= 0 {
		n++
	}
	if n == 0 {
		return parts, "[ca]"
	}
	limiter := ""
	if len(tracks) > 0 {
		limiter = "," + audioLimiter
	}
	parts = append(parts, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0%s[fa]", inputs, n+1, limiter))
	return parts, "[fa]"
}
func validTextTiming(tl TextLayer, durations []float64, total float64) bool {
	if !finite(tl.StartTime) || !finite(tl.EndTime) || tl.StartTime < 0 || tl.EndTime < 0 {
		return false
	}
	if tl.Absolute {
		return tl.EndTime > tl.StartTime && tl.StartTime < total
	}
	d := durations[tl.ClipIdx]
	return tl.StartTime < d && (tl.EndTime == 0 || tl.EndTime <= d && tl.EndTime > tl.StartTime)
}
func textWindow(tl TextLayer, starts, durations []float64, total float64) (float64, float64) {
	if tl.Absolute {
		return tl.StartTime, math.Min(tl.EndTime, total)
	}
	return layerWindow(tl, starts[tl.ClipIdx], durations[tl.ClipIdx])
}
func layerWindow(tl TextLayer, start, duration float64) (float64, float64) {
	end := tl.EndTime
	if end == 0 {
		end = duration
	}
	return start + tl.StartTime, start + end
}
func buildDrawtextFileSegment(tl TextLayer, start, end float64, height int, font, text string) string {
	if tl.EditorStyle {
		return buildEditorDrawtext(tl, start, end, height, font, text)
	}
	size := tl.FontSize
	if size == 0 {
		size = 48
	}
	size = int(math.Max(1, math.Round(float64(size)*float64(height)/fontReferenceHeight)))
	color := tl.Color
	if color == "" {
		color = "FFFFFF"
	}
	x, y := "(w-text_w)/2", "h-text_h-30"
	if tl.HasXY {
		x = fmt.Sprintf("w*%f-text_w/2", tl.X)
		if tl.Align == "left" {
			x = fmt.Sprintf("w*%f", tl.X)
		}
		if tl.Align == "right" {
			x = fmt.Sprintf("w*%f-text_w", tl.X)
		}
		y = fmt.Sprintf("h*%f-text_h/2", tl.Y)
	} else {
		if tl.Position == "top" {
			y = "20"
		}
		if tl.Position == "center" {
			y = "(h-text_h)/2"
		}
	}
	return fmt.Sprintf("drawtext=fontfile='%s':textfile='%s':expansion=none:x=%s:y=%s:fontsize=%d:fontcolor=0x%s:enable='between(t,%f,%f)'", font, text, x, y, size, color, start, end)
}

// EditorShadowOffset is the drop-shadow offset in output pixels:
// round(2*height/1080), at least 1.
func EditorShadowOffset(height int) int {
	return int(math.Max(1, math.Round(2*float64(height)/fontReferenceHeight)))
}

// editorFontSize is the output pixel size of an editor layer's 1080-reference font size.
func editorFontSize(tl TextLayer, height int) int {
	size := tl.FontSize
	if size == 0 {
		size = 48
	}
	return int(math.Max(1, math.Round(float64(size)*float64(height)/fontReferenceHeight)))
}

// buildEditorDrawtext renders a video-editor text layer. Semantics (mirrored by
// the Web preview, see video-editor-api-contract.md):
//   - the text block is widest-line wide and lines*font-line-height tall
//     (y_align=font), no auto-wrap;
//   - (X,Y) is the block centre; the block is clamped inside the frame (top/left
//     win when it is larger than the frame);
//   - lines are aligned left/center/right inside the block (text_align, 6.1+);
//   - a black@0.6 drop shadow is offset by EditorShadowOffset(height) both ways.
func buildEditorDrawtext(tl TextLayer, start, end float64, height int, font, text string) string {
	size := editorFontSize(tl, height)
	color := tl.Color
	if color == "" {
		color = "FFFFFF"
	}
	align := "center"
	if tl.Align == "left" || tl.Align == "right" {
		align = tl.Align
	}
	shadow := EditorShadowOffset(height)
	x := fmt.Sprintf("max(0,min(w-text_w,w*%f-text_w/2))", tl.X)
	y := fmt.Sprintf("max(0,min(h-text_h,h*%f-text_h/2))", tl.Y)
	if tl.Animation == "slide" {
		y += fmt.Sprintf("+h*%f*max(0,1-(t-%f)/%f)", textSlideRise, start, textAnimDuration(start, end))
	}
	return fmt.Sprintf("drawtext=fontfile='%s':textfile='%s':expansion=none:text_align=%s:y_align=font:x='%s':y='%s':fontsize=%d:fontcolor=0x%s:shadowcolor=black@0.6:shadowx=%d:shadowy=%d%s%s:enable='between(t,%f,%f)'", font, text, align, x, y, size, color, shadow, shadow, editorTextStyle(tl, size, height), textAlpha(tl, start, end), start, end)
}

// textSlideRise is how far (fraction of the frame height) "slide" text starts below its place.
const textSlideRise = 0.04

// textAnimDuration: entrance/exit length, 0.3 s or a third of a short layer.
func textAnimDuration(start, end float64) float64 { return math.Min(0.3, (end-start)/3) }

func validTextStyle(tl TextLayer) bool {
	hex := regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	return tl.OutlineWidth >= 0 && tl.OutlineWidth <= 20 && (tl.OutlineWidth == 0 || hex.MatchString(tl.OutlineColor)) &&
		finite(tl.BoxOpacity) && tl.BoxOpacity >= 0 && tl.BoxOpacity <= 1 && (tl.BoxOpacity == 0 || hex.MatchString(tl.BoxColor)) &&
		(tl.Animation == "" || tl.Animation == "fade" || tl.Animation == "slide")
}

// textOutline/textBoxPad scale the 1080-reference style sizes to the output height.
func textOutline(tl TextLayer, height int) int {
	if tl.OutlineWidth == 0 {
		return 0
	}
	return int(math.Max(1, math.Round(float64(tl.OutlineWidth)*float64(height)/fontReferenceHeight)))
}
func textBoxPad(tl TextLayer, fontSize int) int {
	if tl.BoxOpacity == 0 {
		return 0
	}
	return int(math.Max(1, math.Round(float64(fontSize)*0.3)))
}

// textPad is how far style pixels reach outside the measured text box.
func textPad(tl TextLayer, fontSize, height int) int {
	return textOutline(tl, height) + textBoxPad(tl, fontSize)
}

// editorTextStyle draws outline and box outside the text box, so placement and clamping
// (computed on text_w/text_h) match the preview, which paints them without layout.
func editorTextStyle(tl TextLayer, fontSize, height int) string {
	style := ""
	if outline := textOutline(tl, height); outline > 0 {
		style += fmt.Sprintf(":borderw=%d:bordercolor=0x%s", outline, tl.OutlineColor)
	}
	if pad := textBoxPad(tl, fontSize); pad > 0 {
		style += fmt.Sprintf(":box=1:boxcolor=0x%s@%.6f:boxborderw=%d", tl.BoxColor, tl.BoxOpacity, pad+textOutline(tl, height))
	}
	return style
}

// textAlpha fades animated layers in and out over textAnimDuration.
func textAlpha(tl TextLayer, start, end float64) string {
	if tl.Animation == "" {
		return ""
	}
	d := textAnimDuration(start, end)
	return fmt.Sprintf(":alpha='min(1,max(0,min((t-%f)/%f,(%f-t)/%f)))'", start, d, end, d)
}

type mediaInfo struct {
	Duration      float64
	Width, Height int
	Audio         bool
}

func probe(ctx context.Context, path string) (mediaInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_format", "-show_streams", "-of", "json", path).Output()
	if err != nil {
		return mediaInfo{}, fmt.Errorf("ffprobe failed: %w", err)
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		}
		Streams []struct {
			CodecType     string `json:"codec_type"`
			Width, Height int
		}
	}
	if err = json.Unmarshal(out, &raw); err != nil {
		return mediaInfo{}, err
	}
	duration, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil || !finite(duration) || duration <= 0 {
		return mediaInfo{}, fmt.Errorf("invalid media duration")
	}
	info := mediaInfo{Duration: duration}
	for _, s := range raw.Streams {
		if s.CodecType == "video" && info.Width == 0 {
			info.Width = s.Width
			info.Height = s.Height
		}
		if s.CodecType == "audio" {
			info.Audio = true
		}
	}
	return info, nil
}
func probeDuration(ctx context.Context, path string) (float64, error) {
	i, e := probe(ctx, path)
	return i.Duration, e
}
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ProbeVideo rejects media without a video stream and returns its actual duration.
func ProbeVideo(ctx context.Context, path string) (int64, error) {
	info, err := probe(ctx, path)
	if err != nil {
		return 0, err
	}
	if info.Width <= 0 || info.Height <= 0 {
		return 0, fmt.Errorf("media has no playable video stream")
	}
	decodeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := exec.CommandContext(decodeCtx, "ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").Run(); err != nil {
		return 0, fmt.Errorf("video decode failed: %w", err)
	}
	return int64(math.Round(info.Duration * 1000)), nil
}

// SupportsThaiTextOverlays reports whether video-editor text layers can be
// rendered: drawtext with text_align/y_align (FFmpeg 6.1+) and a configured font
// containing Thai glyphs; the default DejaVu font does not, which would burn
// empty boxes.
func (p *FFmpegProcessor) SupportsThaiTextOverlays(ctx context.Context) bool {
	return p.supportsEditorText(ctx) && FontCoversThai(p.fontFile)
}

// supportsEditorText requires drawtext options introduced in FFmpeg 6.1. On
// older builds multi-line alignment cannot match the preview (no per-block width
// and glyph-dependent line heights), so editor text is reported unavailable.
func (p *FFmpegProcessor) supportsEditorText(ctx context.Context) bool {
	if !p.SupportsTextOverlays(ctx) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-h", "filter=drawtext").Output()
	return err == nil && drawtextHasEditorOptions(string(out))
}

func drawtextHasEditorOptions(help string) bool {
	found := map[string]bool{}
	for _, line := range strings.Split(help, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && strings.HasPrefix(fields[1], "<") {
			found[fields[0]] = true
		}
	}
	return found["text_align"] && found["y_align"] && found["shadowx"] && found["shadowcolor"]
}

// SupportsTextOverlays checks local rendering prerequisites, not font glyph coverage.
func (p *FFmpegProcessor) SupportsTextOverlays(ctx context.Context) bool {
	if info, err := os.Stat(p.fontFile); err != nil || info.IsDir() {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] == "drawtext" {
			return true
		}
	}
	return false
}

func textScale(value float64) float64 {
	if value == 0 {
		return 1
	}
	return value
}
func validTextScale(value float64) bool {
	return finite(value) && (value == 0 || value >= .1 && value <= 5)
}

type editorTextBox struct{ width, height float64 }

func scaledTextPosition(tl TextLayer, box editorTextBox, width, height int) (float64, float64) {
	return math.Max(0, math.Min(float64(width)-box.width, tl.X*float64(width)-box.width/2)), math.Max(0, math.Min(float64(height)-box.height, tl.Y*float64(height)-box.height/2))
}

// Measuring on a tiny frame avoids allocating a full inverse-scale video canvas.
// print() uses two disjoint numeric ranges so FFmpeg diagnostics cannot be mistaken for dimensions.
func editorTextDimensions(ctx context.Context, filter string) (int, int, error) {
	filter = strings.Replace(filter, "x='max(0,min(w-text_w,w*0.000000-text_w/2))'", "x='print(text_w+1000000,24)'", 1)
	filter = strings.Replace(filter, "y='max(0,min(h-text_h,h*0.000000-text_h/2))'", "y='print(text_h+2000000,24)'", 1)
	out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "warning", "-f", "lavfi", "-i", "color=c=black:s=16x16:r=1", "-vf", filter, "-frames:v", "1", "-f", "null", "-").CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("measure editor text: %w", err)
	}
	return parseEditorTextDimensions(string(out))
}

func parseEditorTextDimensions(output string) (int, int, error) {
	width, height := 0, 0
	valueLine := regexp.MustCompile(`^(?:\[[^\]\r\n]+\] )?([0-9]+\.[0-9]+)$`)
	for _, line := range strings.Split(output, "\n") {
		match := valueLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		value, e := strconv.ParseFloat(match[1], 64)
		if e != nil {
			continue
		}
		if value >= 1000000 && value < 2000000 {
			width = int(math.Round(value - 1000000))
		}
		if value >= 2000000 && value < 3000000 {
			height = int(math.Round(value - 2000000))
		}
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid measured editor text dimensions")
	}
	return width, height, nil
}

// renderScaledEditorText returns the scaled text box plus how far (output px) the style
// padding extends left/up of it inside the raster.
func renderScaledEditorText(ctx context.Context, tl TextLayer, width, height int, font, text, output string) (editorTextBox, float64, float64, error) {
	measure := tl
	// The raster is a still; its animation is applied to the overlay instead.
	measure.X, measure.Y, measure.Animation = 0, 0, ""
	filter := buildEditorDrawtext(measure, 0, 1, height, font, text)
	pad := textPad(tl, editorFontSize(tl, height), height)
	naturalW, naturalH, err := editorTextDimensions(ctx, filter)
	if err != nil {
		return editorTextBox{}, 0, 0, err
	}
	sx, sy := textScale(tl.ScaleX), textScale(tl.ScaleY)
	box := editorTextBox{float64(naturalW) * sx, float64(naturalH) * sy}
	shadow := EditorShadowOffset(height)
	// Oversized text clamps to the top/left, so pixels beyond the output need not be rasterized.
	sourceW := min(naturalW+shadow+1, int(math.Ceil(float64(width)/sx))+shadow+1) + 2*pad
	sourceH := min(naturalH+shadow+1, int(math.Ceil(float64(height)/sy))+shadow+1) + 2*pad
	scaledW := min(width+int(math.Ceil(float64(shadow+2*pad)*sx))+1, max(1, int(math.Round(float64(sourceW)*sx))))
	scaledH := min(height+int(math.Ceil(float64(shadow+2*pad)*sy))+1, max(1, int(math.Round(float64(sourceH)*sy))))
	// Normal editor layers are limited to 500 characters, six lines, and a 200px reference font.
	// Reject unexpectedly large font metrics rather than allocate an unbounded raster.
	if sourceW > 40000 || sourceH > 4000 || int64(sourceW)*int64(sourceH) > 64000000 {
		return editorTextBox{}, 0, 0, fmt.Errorf("editor text raster exceeds safe dimensions")
	}
	filter = strings.Replace(filter, "x='max(0,min(w-text_w,w*0.000000-text_w/2))'", fmt.Sprintf("x=%d", pad), 1)
	filter = strings.Replace(filter, "y='max(0,min(h-text_h,h*0.000000-text_h/2))'", fmt.Sprintf("y=%d", pad), 1)
	filter = "format=rgba," + filter + fmt.Sprintf(",scale=%d:%d:flags=lanczos,setsar=1", scaledW, scaledH)
	out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black@0.0:s=%dx%d:r=1,format=rgba", sourceW, sourceH), "-vf", filter, "-frames:v", "1", "-threads", "1", "-y", output).CombinedOutput()
	if err != nil {
		return editorTextBox{}, 0, 0, fmt.Errorf("rasterize editor text: %w: %s", err, lastLines(string(out), 2))
	}
	return box, float64(pad) * sx, float64(pad) * sy, nil
}
