package videoprocessing

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func ffmpegAvailable(t *testing.T) {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, e := exec.LookPath(b); e != nil {
			t.Skip(b + " unavailable; real rendering NOT_RUN")
		}
	}
}
func testClip(t *testing.T, dir, name, color, size, rate string, audio bool) string {
	t.Helper()
	path := filepath.Join(dir, name+".mp4")
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "color=c=" + color + ":s=" + size + ":r=" + rate + ":d=1"}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1", "-c:a", "aac")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", "1", path)
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %s %v", out, e)
	}
	return path
}
func render(t *testing.T, req MergeRequest) string {
	t.Helper()
	data, e := NewFFmpegProcessor("").Merge(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "render.mp4")
	if e = os.WriteFile(p, data, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func pixel(t *testing.T, path, time string) []byte {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", time, "-i", path, "-frames:v", "1", "-vf", "scale=1:1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-").Output()
	if e != nil || len(out) < 3 {
		t.Fatalf("pixel decode: %v", e)
	}
	return out
}
func TestRealMergeMixedClipsTrimOrderAndAudio(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	red := testClip(t, d, "red", "red", "160x90", "24", true)
	blue := testClip(t, d, "blue", "blue", "90x160", "15", false)
	p := render(t, MergeRequest{Clips: []Clip{{Path: blue, TrimStart: .1, TrimEnd: .6}, {Path: red, TrimStart: .2, TrimEnd: .7}}, OutputResolution: "720p", OutputAspectRatio: "16:9"})
	info, e := probe(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	if info.Width != 1280 || info.Height != 720 || !info.Audio || math.Abs(info.Duration-1) > .1 {
		t.Fatalf("unexpected output %+v", info)
	}
	first, last := pixel(t, p, "0.2"), pixel(t, p, "0.8")
	if first[2] <= first[0] || last[0] <= last[2] {
		t.Fatalf("order incorrect: %v %v", first, last)
	}
	out, e := exec.Command("ffmpeg", "-v", "info", "-i", p, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	if e != nil || strings.Contains(string(out), "mean_volume: -91") {
		t.Fatalf("clip audio was dropped: %v %s", e, out)
	}
}
func TestRealMergeSinglePortraitMuteBackground(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	silent := testClip(t, d, "silent", "green", "160x90", "25", false)
	sound := testClip(t, d, "sound", "red", "160x90", "24", true)
	p := render(t, MergeRequest{Clips: []Clip{{Path: silent, TrimEnd: .4}}, AudioPath: &sound, AudioVolume: 0, OutputAspectRatio: "9:16"})
	info, e := probe(context.Background(), p)
	if e != nil || info.Width != 720 || info.Height != 1280 {
		t.Fatalf("portrait %+v %v", info, e)
	}
	out, e := exec.Command("ffmpeg", "-v", "info", "-i", p, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	if e != nil || !strings.Contains(string(out), "max_volume: -91.0 dB") {
		t.Fatalf("zero volume not silent: %v %s", e, out)
	}
	if ms, e := ProbeVideo(context.Background(), p); e != nil || ms < 350 || ms > 500 {
		t.Fatalf("duration %d %v", ms, e)
	}
}
func TestRejectInvalidTrimAndSettings(t *testing.T) {
	ffmpegAvailable(t)
	clip := testClip(t, t.TempDir(), "clip", "red", "160x90", "24", false)
	for _, c := range []Clip{{Path: clip, TrimStart: -1}, {Path: clip, TrimEnd: 2}, {Path: clip, TrimStart: .7, TrimEnd: .2}, {Path: clip, TrimEnd: math.Inf(1)}, {Path: clip, TrimStart: math.NaN()}} {
		if _, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{c}}); e == nil {
			t.Fatalf("accepted invalid trim %+v", c)
		}
	}
	if _, _, e := OutputDimensions("4k", "16:9"); e == nil {
		t.Fatal("accepted unsupported output")
	}
}
func TestRealOverlayParserSafeText(t *testing.T) {
	ffmpegAvailable(t)
	font := ""
	for _, p := range []string{DefaultFontFile, "/System/Library/Fonts/Supplemental/Arial.ttf", "/Library/Fonts/Arial.ttf"} {
		if _, e := os.Stat(p); e == nil {
			font = p
			break
		}
	}
	if font == "" {
		t.Skip("overlay NOT_RUN: no local test font")
	}
	filters, e := exec.Command("ffmpeg", "-filters").CombinedOutput()
	if e != nil || !strings.Contains(string(filters), "drawtext") {
		t.Skip("overlay NOT_RUN: ffmpeg drawtext missing")
	}
	clip := testClip(t, t.TempDir(), "clip", "red", "160x90", "24", false)
	_, e = NewFFmpegProcessor(font).Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: clip, TrimEnd: .3}}, TextLayers: []TextLayer{{Text: "quote ' : \\ , ; [x] %{n}\nnext line", Position: "center"}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestOutputDimensionsAndCancelledRender(t *testing.T) {
	for ratio, want := range map[string][2]int{"16:9": {1920, 1080}, "9:16": {1080, 1920}, "1:1": {1080, 1080}, "4:3": {1440, 1080}, "3:4": {1080, 1440}, "21:9": {2520, 1080}} {
		w, h, e := OutputDimensions("1080p", ratio)
		if e != nil || [2]int{w, h} != want {
			t.Fatalf("%s %dx%d %v", ratio, w, h, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := NewFFmpegProcessor("").Merge(ctx, MergeRequest{Clips: []Clip{{Path: "unused"}}}); e == nil {
		t.Fatal("accepted cancelled context")
	}
}
