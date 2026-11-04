package videoprocessing

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAudioMixPartsLegacyBackgroundUnchanged(t *testing.T) {
	t.Parallel()
	parts, label := audioMixParts(2, .3, 4.5, nil)
	legacy := fmt.Sprintf("[%d:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%.6f,apad,atrim=duration=%.6f[bg];[ca][bg]amix=inputs=2:duration=first:normalize=0[fa]", 2, .3, 4.5)
	if strings.Join(parts, ";") != legacy || label != "[fa]" || strings.Contains(legacy, "alimiter") {
		t.Fatalf("storyboard background graph changed:\n%s\n%s", strings.Join(parts, ";"), legacy)
	}
	parts, label = audioMixParts(-1, 0, 4.5, nil)
	if len(parts) != 0 || label != "[ca]" {
		t.Fatalf("no-audio graph %v %s", parts, label)
	}
}

func TestAudioMixPartsTimelineTracks(t *testing.T) {
	t.Parallel()
	tracks := []audioInput{{index: 3, track: AudioTrack{TrimStart: 1.25, TrimEnd: 4, At: 1.5, Volume: .5}}, {index: 4, track: AudioTrack{TrimStart: 0, TrimEnd: 2, At: 0, Volume: 0}}}
	parts, label := audioMixParts(-1, 0, 10, tracks)
	graph := strings.Join(parts, ";")
	for _, want := range []string{
		"[3:a:0]atrim=start=1.250000:end=4.000000,asetpts=PTS-STARTPTS",
		"volume=0.500000,adelay=delays=72000S:all=1[au0]",
		"[4:a:0]atrim=start=0.000000:end=2.000000",
		"volume=0.000000,adelay=delays=0S:all=1[au1]",
		"[ca][au0][au1]amix=inputs=3:duration=first:normalize=0,alimiter=limit=0.95:level=0:latency=1[fa]",
	} {
		if !strings.Contains(graph, want) {
			t.Fatalf("graph missing %q:\n%s", want, graph)
		}
	}
	if label != "[fa]" {
		t.Fatal(label)
	}
	parts, _ = audioMixParts(2, 1, 10, tracks[:1])
	if !strings.Contains(strings.Join(parts, ";"), "[ca][bg][au0]amix=inputs=3:duration=first:normalize=0,alimiter=limit=0.95:level=0:latency=1[fa]") {
		t.Fatalf("background + track %v", parts)
	}
}

func TestAbsoluteTextTimingAndPlacement(t *testing.T) {
	t.Parallel()
	durations, starts, total := []float64{2, 3}, []float64{0, 2}, 5.0
	abs := TextLayer{Absolute: true, ClipIdx: 99, StartTime: 1.5, EndTime: 9, HasXY: true, X: .25, Y: .9, Align: "left", FontSize: 60, Color: "FF0000"}
	if !validTextTiming(abs, durations, total) {
		t.Fatal("absolute layer crossing the end must be accepted and clipped")
	}
	start, end := textWindow(abs, starts, durations, total)
	if start != 1.5 || end != 5 {
		t.Fatalf("window %v %v", start, end)
	}
	seg := buildDrawtextFileSegment(abs, start, end, 720, "font.ttf", "text.txt")
	for _, want := range []string{"expansion=none", "x=w*0.250000", "y=h*0.900000-text_h/2", "fontsize=40", "fontcolor=0xFF0000", "between(t,1.500000,5.000000)"} {
		if !strings.Contains(seg, want) {
			t.Fatalf("drawtext missing %q: %s", want, seg)
		}
	}
	for _, bad := range []TextLayer{{Absolute: true, StartTime: 5, EndTime: 6}, {Absolute: true, StartTime: 2, EndTime: 2}, {Absolute: true, StartTime: -1, EndTime: 1}, {Absolute: true, StartTime: math.NaN(), EndTime: 1}} {
		if validTextTiming(bad, durations, total) {
			t.Fatalf("accepted %+v", bad)
		}
	}
	// Clip-relative layers keep their original semantics.
	rel := TextLayer{ClipIdx: 1, StartTime: .5, EndTime: 0}
	if !validTextTiming(rel, durations, total) {
		t.Fatal("relative layer rejected")
	}
	if s, e := textWindow(rel, starts, durations, total); s != 2.5 || e != 5 {
		t.Fatalf("relative window %v %v", s, e)
	}
	if validTextTiming(TextLayer{ClipIdx: 0, StartTime: 0, EndTime: 2.5}, durations, total) {
		t.Fatal("relative layer past its clip accepted")
	}
}

func sfntWithCmap(sub []byte) []byte {
	cmap := make([]byte, 12)
	binary.BigEndian.PutUint16(cmap[2:], 1)
	binary.BigEndian.PutUint16(cmap[4:], 3)
	binary.BigEndian.PutUint16(cmap[6:], 10)
	binary.BigEndian.PutUint32(cmap[8:], 12)
	cmap = append(cmap, sub...)
	font := make([]byte, 28)
	binary.BigEndian.PutUint32(font, 0x00010000)
	binary.BigEndian.PutUint16(font[4:], 1)
	copy(font[12:], "cmap")
	binary.BigEndian.PutUint32(font[20:], 28)
	binary.BigEndian.PutUint32(font[24:], uint32(len(cmap)))
	return append(font, cmap...)
}

func TestFontCoverageParser(t *testing.T) {
	t.Parallel()
	f12 := make([]byte, 28)
	binary.BigEndian.PutUint16(f12, 12)
	binary.BigEndian.PutUint32(f12[12:], 1)
	binary.BigEndian.PutUint32(f12[16:], 0x0E00)
	binary.BigEndian.PutUint32(f12[20:], 0x0E7F)
	binary.BigEndian.PutUint32(f12[24:], 5)
	if !fontCovers(sfntWithCmap(f12), thaiProbe) {
		t.Fatal("format 12 Thai coverage missed")
	}
	// Format 4 with Latin A-Z only, then the mandatory 0xFFFF terminator.
	f4 := make([]byte, 14+2*2+2+2*2+2*2+2*2)
	binary.BigEndian.PutUint16(f4, 4)
	binary.BigEndian.PutUint16(f4[6:], 4)
	binary.BigEndian.PutUint16(f4[14:], 'Z')
	binary.BigEndian.PutUint16(f4[16:], 0xFFFF)
	binary.BigEndian.PutUint16(f4[20:], 'A')
	binary.BigEndian.PutUint16(f4[22:], 0xFFFF)
	binary.BigEndian.PutUint16(f4[24:], 10)
	binary.BigEndian.PutUint16(f4[26:], 1)
	latin := sfntWithCmap(f4)
	if !fontCovers(latin, []rune{'A', 'Q'}) || fontCovers(latin, thaiProbe) {
		t.Fatal("format 4 coverage wrong")
	}
	for _, junk := range [][]byte{nil, []byte("ttcf"), latin[:20], []byte("not a font at all, definitely")} {
		if fontCovers(junk, thaiProbe) {
			t.Fatal("junk accepted")
		}
	}
	if FontCoversThai(filepath.Join(t.TempDir(), "missing.ttf")) {
		t.Fatal("missing font accepted")
	}
}

// thaiFont returns a local Thai-capable font for real-render tests, if any.
func thaiFont() string {
	for _, p := range []string{os.Getenv("FFMPEG_FONT_FILE"), "/System/Library/Fonts/Supplemental/Ayuthaya.ttf", "/System/Library/Fonts/Supplemental/Tahoma.ttf", "/Library/Fonts/Arial Unicode.ttf", "/usr/share/fonts/noto/NotoSansThai-Regular.ttf", "/usr/share/fonts/truetype/noto/NotoSansThai-Regular.ttf", "/usr/share/fonts/truetype/tlwg/Garuda.ttf"} {
		if p != "" && FontCoversThai(p) {
			return p
		}
	}
	return ""
}

func TestLocalThaiFontDetection(t *testing.T) {
	t.Parallel()
	font := thaiFont()
	if font == "" {
		t.Skip("NOT_RUN: no local Thai-capable font")
	}
	t.Logf("Thai-capable font: %s", font)
	for _, p := range []string{DefaultFontFile, "/System/Library/Fonts/Supplemental/Arial.ttf"} {
		if _, e := os.Stat(p); e == nil && FontCoversThai(p) {
			t.Fatalf("%s reported Thai coverage", p)
		}
	}
}

func meanVolume(t *testing.T, path string, start, duration float64) float64 {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "info", "-ss", fmt.Sprint(start), "-t", fmt.Sprint(duration), "-i", path, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	if e != nil {
		t.Fatalf("volumedetect: %v %s", e, out)
	}
	m := regexp.MustCompile(`mean_volume: (-?[0-9.]+|-inf) dB`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("no mean volume: %s", out)
	}
	if m[1] == "-inf" {
		return -200
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

func audioFixture(t *testing.T, dir string, seconds float64) string {
	t.Helper()
	path := filepath.Join(dir, "tone.wav")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%g", seconds), "-c:a", "pcm_s16le", path).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %s %v", out, e)
	}
	return path
}

func TestRealMergeAudioTrackOffsetVolumeAndCut(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	video := testClip(t, d, "silent", "green", "160x90", "25", false)
	tone := audioFixture(t, d, 3)
	p := render(t, MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{
		{Path: tone, TrimStart: .5, TrimEnd: 3, At: .5, Volume: 1},
		{Path: "/nonexistent/never-probed.wav", TrimStart: 0, TrimEnd: 1, At: 1, Volume: 1},
	}})
	info, e := probe(context.Background(), p)
	if e != nil || !info.Audio || math.Abs(info.Duration-1) > .1 {
		t.Fatalf("output %+v %v", info, e)
	}
	before, after := meanVolume(t, p, 0, .4), meanVolume(t, p, .6, .35)
	if before > -80 || after < -30 {
		t.Fatalf("offset not applied: before=%.1f after=%.1f", before, after)
	}
	quiet := render(t, MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: tone, TrimStart: 0, TrimEnd: 1, At: 0, Volume: .25}}})
	loud := render(t, MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: tone, TrimStart: 0, TrimEnd: 1, At: 0, Volume: 1}}})
	if diff := meanVolume(t, loud, .1, .7) - meanVolume(t, quiet, .1, .7); math.Abs(diff-12.04) > 1.5 {
		t.Fatalf("volume 0.25 should be about -12 dB, diff=%.2f", diff)
	}
}

func TestRejectInvalidAudioTracks(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	video := testClip(t, d, "v", "red", "160x90", "24", false)
	tone := audioFixture(t, d, 1)
	for _, tr := range []AudioTrack{{Path: tone, TrimStart: -1, TrimEnd: 1}, {Path: tone, TrimStart: .5, TrimEnd: .5}, {Path: tone, TrimEnd: 1, At: -1}, {Path: tone, TrimEnd: 1, Volume: 1.5}, {Path: tone, TrimEnd: 2}, {Path: video, TrimEnd: .5}, {Path: tone, TrimEnd: math.NaN()}} {
		if _, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: video, TrimEnd: .5}}, AudioTracks: []AudioTrack{tr}}); e == nil {
			t.Fatalf("accepted %+v", tr)
		}
	}
}

func TestRealThaiAbsoluteTextRender(t *testing.T) {
	ffmpegAvailable(t)
	font := thaiFont()
	if font == "" {
		t.Skip("NOT_RUN: no local Thai-capable font")
	}
	if !NewFFmpegProcessor(font).SupportsThaiTextOverlays(context.Background()) {
		t.Skip("NOT_RUN: local FFmpeg lacks the drawtext filter (needs libfreetype/libharfbuzz)")
	}
	d := t.TempDir()
	video := testClip(t, d, "v", "black", "160x90", "25", false)
	tone := audioFixture(t, d, 1)
	data, e := NewFFmpegProcessor(font).Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: tone, TrimEnd: 1, At: .5, Volume: .8}}, TextLayers: []TextLayer{{Absolute: true, Text: "สวัสดีครับ ทดสอบ่้๊๋", StartTime: .5, EndTime: 5, HasXY: true, X: .5, Y: .5, Align: "center", FontSize: 200, Color: "FFFFFF"}}})
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(d, "out.mp4")
	if e = os.WriteFile(p, data, 0600); e != nil {
		t.Fatal(e)
	}
	if px := pixel(t, p, "0.2"); px[0] > 20 {
		t.Fatalf("text visible before its start: %v", px)
	}
	if px := pixel(t, p, "0.8"); px[0] < 3 {
		t.Fatalf("text not burned after its start: %v", px)
	}
}

func TestEditorDrawtextSemantics(t *testing.T) {
	t.Parallel()
	for h, want := range map[int]int{360: 1, 720: 1, 1080: 2, 1920: 4, 1280: 2} {
		if got := EditorShadowOffset(h); got != want {
			t.Errorf("shadow offset h=%d got %d want %d", h, got, want)
		}
	}
	for align, want := range map[string]string{"left": "text_align=left", "right": "text_align=right", "center": "text_align=center", "": "text_align=center"} {
		seg := buildDrawtextFileSegment(TextLayer{Absolute: true, EditorStyle: true, HasXY: true, X: .25, Y: .9, Align: align, FontSize: 60, Color: "FFCC00"}, 1, 2, 1080, "font.ttf", "t.txt")
		for _, w := range []string{
			want + ":", "y_align=font", "expansion=none",
			"x='max(0,min(w-text_w,w*0.250000-text_w/2))'",
			"y='max(0,min(h-text_h,h*0.900000-text_h/2))'",
			"fontsize=60", "fontcolor=0xFFCC00",
			"shadowcolor=black@0.6:shadowx=2:shadowy=2",
			"between(t,1.000000,2.000000)",
		} {
			if !strings.Contains(seg, w) {
				t.Fatalf("editor drawtext (%q) missing %q: %s", align, w, seg)
			}
		}
		if strings.Contains(seg, "fix_bounds") || strings.Contains(seg, "box=") {
			t.Fatalf("unexpected option: %s", seg)
		}
	}
	// Legacy (storyboard) layers keep their unclamped, shadowless form.
	legacy := buildDrawtextFileSegment(TextLayer{HasXY: true, X: .5, Y: .5, Align: "center"}, 0, 1, 720, "f", "t")
	if strings.Contains(legacy, "shadow") || strings.Contains(legacy, "text_align") || !strings.Contains(legacy, "x=w*0.500000-text_w/2") {
		t.Fatalf("legacy drawtext changed: %s", legacy)
	}
}

func TestDrawtextEditorOptionDetection(t *testing.T) {
	t.Parallel()
	modern := "drawtext AVOptions:\n   fontfile          <string>     ..FV.......\n   text_align        <flags>      ..FV.....T. set text alignment\n   y_align           <int>        ..FV.....T.\n   shadowcolor       <color>      ..FV.....T.\n   shadowx           <int>        ..FV.....T.\n"
	if !drawtextHasEditorOptions(modern) {
		t.Fatal("6.1+ options not detected")
	}
	old := "drawtext AVOptions:\n   fontfile          <string>     ..FV.......\n   shadowcolor       <color>      ..FV.....T.\n   shadowx           <int>        ..FV.....T.\n   ft_load_flags     <flags>  set text_align flags\n"
	if drawtextHasEditorOptions(old) {
		t.Fatal("pre-6.1 drawtext accepted (text_align only in a description)")
	}
}

func maxVolume(t *testing.T, path string) float64 {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "info", "-i", path, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	if e != nil {
		t.Fatalf("volumedetect: %v %s", e, out)
	}
	m := regexp.MustCompile(`max_volume: (-?[0-9.]+) dB`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("no max volume: %s", out)
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// Overlapping full-scale timeline tracks are limited below 0 dBFS (limit 0.95).
func TestRealMergeTimelineAudioIsLimited(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	video := testClip(t, d, "v", "black", "160x90", "25", false)
	loud := filepath.Join(d, "loud.wav")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1", "-af", "volume=8", "-c:a", "pcm_s16le", loud).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %s %v", out, e)
	}
	p := render(t, MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: loud, TrimEnd: 1, Volume: 1}, {Path: loud, TrimEnd: 1, Volume: 1}}})
	if peak := maxVolume(t, p); peak > -0.1 {
		t.Fatalf("summed tracks not limited: max_volume=%.2f dB", peak)
	}
}

// columnsLit returns the first/last x of pixels brighter than 128 in a grey frame.
func columnsLit(t *testing.T, path, at string, w, h int) (int, int, int, int) {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", path, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
	if e != nil || len(out) != w*h {
		t.Fatalf("frame decode: %v len=%d", e, len(out))
	}
	x0, x1, y0, y1 := w, -1, h, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if out[y*w+x] > 128 {
				x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
			}
		}
	}
	return x0, x1, y0, y1
}

// Real editor-style render: Thai text anchored at the frame corner is clamped
// inside the frame, and right alignment keeps short lines at the block's right.
func TestRealEditorTextClampAndAlign(t *testing.T) {
	ffmpegAvailable(t)
	font := thaiFont()
	if font == "" {
		t.Skip("NOT_RUN: no local Thai-capable font")
	}
	proc := NewFFmpegProcessor(font)
	if !proc.SupportsThaiTextOverlays(context.Background()) {
		t.Skip("NOT_RUN: local FFmpeg lacks drawtext with text_align/y_align (FFmpeg 6.1+ with libfreetype)")
	}
	d := t.TempDir()
	video := testClip(t, d, "v", "black", "1280x720", "25", false)
	merge := func(l TextLayer) string {
		l.Absolute, l.EditorStyle, l.HasXY, l.EndTime, l.Color = true, true, true, 1, "FFFFFF"
		data, e := proc.Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", TextLayers: []TextLayer{l}})
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(t.TempDir(), "out.mp4")
		if e = os.WriteFile(p, data, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	// Anchor (0,0): an unclamped centre anchor would put half the block off-frame.
	x0, x1, y0, _ := columnsLit(t, merge(TextLayer{Text: "สวัสดีครับ", X: 0, Y: 0, Align: "center", FontSize: 120}), "0.5", 1280, 720)
	if x1 < 0 || x0 > 12 || y0 > 40 || x1-x0 < 150 {
		t.Fatalf("corner text not clamped inside frame: x=[%d,%d] y0=%d", x0, x1, y0)
	}
	// Bottom-right anchor clamps against the opposite edges.
	x0, x1, _, y1 := columnsLit(t, merge(TextLayer{Text: "สวัสดีครับ", X: 1, Y: 1, Align: "center", FontSize: 120}), "0.5", 1280, 720)
	if x1 < 1268 || y1 < 660 || x1-x0 < 150 {
		t.Fatalf("bottom-right text not clamped inside frame: x=[%d,%d] y1=%d", x0, x1, y1)
	}
	// Two lines, right aligned: both lines end at the same right edge.
	p := merge(TextLayer{Text: "ก\nกกกกกกกก", X: .5, Y: .5, Align: "right", FontSize: 100})
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", "0.5", "-i", p, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
	if e != nil || len(out) != 1280*720 {
		t.Fatal(e)
	}
	rowRight := func(yFrom, yTo int) (int, int) {
		l, r := 1280, -1
		for y := yFrom; y < yTo; y++ {
			for x := 0; x < 1280; x++ {
				if out[y*1280+x] > 128 {
					l, r = min(l, x), max(r, x)
				}
			}
		}
		return l, r
	}
	l1, r1 := rowRight(0, 360)
	l2, r2 := rowRight(360, 720)
	if r1 < 0 || r2 < 0 || absInt(r1-r2) > 12 || l1-l2 < 200 {
		t.Fatalf("right alignment wrong: line1=[%d,%d] line2=[%d,%d]", l1, r1, l2, r2)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestScaledEditorTextPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ x, y, w, h, wantX, wantY float64 }{
		{.5, .5, 200, 50, 540, 335}, {0, 0, 200, 50, 0, 0}, {1, 1, 200, 50, 1080, 670}, {.5, .5, 2000, 1000, 0, 0},
	} {
		x, y := scaledTextPosition(TextLayer{X: tc.x, Y: tc.y}, editorTextBox{tc.w, tc.h}, 1280, 720)
		if x != tc.wantX || y != tc.wantY {
			t.Fatalf("position (%v,%v) want (%v,%v)", x, y, tc.wantX, tc.wantY)
		}
	}
	for _, scale := range []float64{0, .1, 1, 5} {
		if !validTextScale(scale) {
			t.Fatalf("invalid valid scale %v", scale)
		}
	}
	for _, scale := range []float64{-.1, .099, 5.001, math.NaN(), math.Inf(1)} {
		if validTextScale(scale) {
			t.Fatalf("accepted scale %v", scale)
		}
	}
}

func TestRealEditorIndependentTextScale(t *testing.T) {
	ffmpegAvailable(t)
	font := thaiFont()
	if font == "" {
		t.Skip("NOT_RUN: no local Thai-capable font")
	}
	proc := NewFFmpegProcessor(font)
	if !proc.SupportsThaiTextOverlays(context.Background()) {
		t.Skip("NOT_RUN: local FFmpeg lacks drawtext with editor options")
	}
	video := testClip(t, t.TempDir(), "scaled", "black", "1280x720", "25", false)
	render := func(sx, sy float64) (int, int) {
		data, err := proc.Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", TextLayers: []TextLayer{{Text: "สวัสดีครับ", FontSize: 100, Color: "FFFFFF", HasXY: true, EditorStyle: true, Absolute: true, X: .5, Y: .5, EndTime: 1, ScaleX: sx, ScaleY: sy}}})
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "scaled.mp4")
		if err = os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		x0, x1, y0, y1 := columnsLit(t, file, "0.5", 1280, 720)
		return x1 - x0 + 1, y1 - y0 + 1
	}
	w, h := render(1, 1)
	wide, short := render(2, .5)
	if math.Abs(float64(wide)/float64(w)-2) > .15 || math.Abs(float64(short)/float64(h)-.5) > .15 {
		t.Fatalf("independent scaling failed: natural %dx%d transformed %dx%d", w, h, wide, short)
	}
}

func TestEditorTextDimensionDiagnostics(t *testing.T) {
	t.Parallel()
	output := "[drawtext warning @ 0x1234] unsupported glyph 99\n[Eval @ 0x700003b9d088] 1000222.000000\n[Eval @ 0x700003b9d088] 2000071.000000\n[Eval @ 0x700003b9d088] 1000222.000000\nframe=1\n"
	w, h, err := parseEditorTextDimensions(output)
	if err != nil || w != 222 || h != 71 {
		t.Fatalf("diagnostics parsed as %d x %d: %v", w, h, err)
	}
	for _, invalid := range []string{"", "[drawtext @ 0x123] text_w:42 text_h:12", "1000010.000000\n", "1000000.000000\n2000010.000000", "1000010.000000\n2000000.000000", "NaN\n+Inf\n"} {
		if _, _, err := parseEditorTextDimensions(invalid); err == nil {
			t.Fatalf("accepted invalid dimensions %q", invalid)
		}
	}
}
