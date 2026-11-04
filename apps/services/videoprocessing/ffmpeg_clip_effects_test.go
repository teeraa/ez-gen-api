package videoprocessing

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestClipEffectFilterStrings(t *testing.T) {
	t.Parallel()
	if videoSpeedFilter(0) != "setpts=PTS-STARTPTS" || videoSpeedFilter(1) != "setpts=PTS-STARTPTS" {
		t.Fatal("normal speed must keep the legacy setpts")
	}
	if got := videoSpeedFilter(2); got != "setpts=(PTS-STARTPTS)/2.000000" {
		t.Fatal(got)
	}
	cases := map[float64]string{0: "", 1: "", 2: ",atempo=2.000000", 4: ",atempo=2,atempo=2.000000", .5: ",atempo=0.500000", .25: ",atempo=0.5,atempo=0.500000", 1.5: ",atempo=1.500000", 3: ",atempo=2,atempo=1.500000"}
	for speed, want := range cases {
		if got := audioSpeedFilters(speed); got != want {
			t.Fatalf("speed %v: %q want %q", speed, got, want)
		}
	}
	if videoFadeFilters(0, 0, 3) != "" || audioFadeFilters(0, 0, 3) != "" {
		t.Fatal("no fade must add no filter")
	}
	if got := videoFadeFilters(.5, 1, 3); got != ",fade=t=in:st=0:d=0.500000:color=black,fade=t=out:st=2.000000:d=1.000000:color=black" {
		t.Fatal(got)
	}
	if got := audioFadeFilters(0, 1, 3); got != ",afade=t=out:st=2.000000:d=1.000000" {
		t.Fatal(got)
	}
	for _, bad := range [][3]float64{{-1, 0, 3}, {0, 11, 20}, {2, 2, 3}, {math.NaN(), 0, 3}} {
		if validFades(bad[0], bad[1], bad[2]) {
			t.Fatalf("accepted fades %v", bad)
		}
	}
	if !validFades(1.5, 1.5, 3) {
		t.Fatal("fades filling the whole span must be accepted")
	}
}

func TestRealMergeClipSpeedChangesDuration(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	clip := testClip(t, d, "tone", "blue", "160x90", "25", true)
	// 1 s at 2× (0.5 s) + 0.5 s at 0.5× (1 s) = 1.5 s; the two-stage 0.25× path is covered by the filter test.
	p := render(t, MergeRequest{Clips: []Clip{{Path: clip, Speed: 2}, {Path: clip, TrimEnd: .5, Speed: .5}}, OutputAspectRatio: "16:9"})
	info, e := probe(context.Background(), p)
	if e != nil || !info.Audio || math.Abs(info.Duration-1.5) > .1 {
		t.Fatalf("speed output %+v %v", info, e)
	}
	if v := meanVolume(t, p, .7, .6); v < -40 {
		t.Fatalf("slowed clip lost its audio: %.1f dB", v)
	}
}

func TestRealMergeVideoAndAudioFades(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	clip := testClip(t, d, "white", "white", "160x90", "25", true)
	p := render(t, MergeRequest{Clips: []Clip{{Path: clip, FadeIn: .4, FadeOut: .4}}, OutputAspectRatio: "16:9"})
	first, middle := pixel(t, p, "0"), pixel(t, p, "0.5")
	if first[0] > 40 || middle[0] < 200 {
		t.Fatalf("fade from black not applied: first=%v middle=%v", first, middle)
	}
	if start, mid := meanVolume(t, p, 0, .05), meanVolume(t, p, .45, .1); start > mid-15 {
		t.Fatalf("clip audio did not fade in: start=%.1f mid=%.1f", start, mid)
	}

	silent := testClip(t, d, "silent", "green", "160x90", "25", false)
	tone := audioFixture(t, d, 2)
	faded := render(t, MergeRequest{Clips: []Clip{{Path: silent, TrimEnd: 1}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: tone, TrimStart: 0, TrimEnd: 1, At: 0, Volume: 1, FadeOut: .5}}})
	if early, late := meanVolume(t, faded, .1, .3), meanVolume(t, faded, .9, .08); late > early-12 {
		t.Fatalf("audio track did not fade out: early=%.1f late=%.1f", early, late)
	}
}

func TestRealMergeDetachedVideoSoundAsTrack(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	withSound := testClip(t, d, "withsound", "red", "160x90", "25", true)
	// The clip itself is muted; its sound comes back through a timeline audio track.
	volume := 0.0
	p := render(t, MergeRequest{Clips: []Clip{{Path: withSound, Volume: &volume}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: withSound, TrimStart: 0, TrimEnd: 1, At: 0, Volume: 1}}})
	if v := meanVolume(t, p, .2, .5); v < -40 {
		t.Fatalf("detached sound missing: %.1f dB", v)
	}
}

func TestRejectInvalidClipEffects(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	clip := testClip(t, d, "c", "blue", "160x90", "25", true)
	for name, c := range map[string]Clip{"slow": {Path: clip, Speed: .1}, "fast": {Path: clip, Speed: 8}, "nan": {Path: clip, Speed: math.NaN()}, "fade": {Path: clip, Speed: 2, FadeIn: .3, FadeOut: .3}} {
		_, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{c}, OutputAspectRatio: "16:9"})
		if e == nil || !strings.Contains(e.Error(), "invalid") {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
	_, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: clip}}, OutputAspectRatio: "16:9", AudioTracks: []AudioTrack{{Path: clip, TrimEnd: 1, Volume: 1, FadeIn: .8, FadeOut: .8}}})
	if e == nil || !strings.Contains(e.Error(), "invalid fade") {
		t.Fatalf("audio fade overflow accepted: %v", e)
	}
}

func TestClipLookFilterStrings(t *testing.T) {
	t.Parallel()
	if got := frameFilter(ClipLook{}, 1280, 720); got != "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2" {
		t.Fatalf("default frame must keep the legacy letterbox: %s", got)
	}
	if lookFilter(ClipLook{Fit: "cover", Zoom: 2}) != "" {
		t.Fatal("no colour change must add no eq")
	}
	if got := lookFilter(ClipLook{Brightness: .5, Contrast: -1, Saturation: -1}); got != ",eq=brightness=0.150000:contrast=0.500000:saturation=0.000000" {
		t.Fatal(got)
	}
	cover := frameFilter(ClipLook{Fit: "cover", PanX: -1}, 720, 1280)
	for _, want := range []string{"max(720/iw,1280/ih)*1.000000", "crop=720:1280:x='(iw-720)*0.000000':y='(ih-1280)*0.500000'"} {
		if !strings.Contains(cover, want) {
			t.Fatalf("cover filter missing %q: %s", want, cover)
		}
	}
	for _, bad := range []ClipLook{{Brightness: 1.5}, {Zoom: .5}, {Zoom: 4}, {Fit: "stretch"}, {PanY: math.NaN()}} {
		if validLook(bad) {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestRealMergeCoverCropAndColour(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	// A 16:9 source whose left half is red and right half blue, rendered into a 9:16 frame.
	src := filepath.Join(d, "split.mp4")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=320x180:d=1:r=25", "-f", "lavfi", "-i", "color=c=blue:s=320x180:d=1:r=25", "-filter_complex", "[0:v]crop=160:180:0:0[l];[1:v]crop=160:180:160:0[r];[l][r]hstack", "-c:v", "libx264", "-pix_fmt", "yuv444p", "-t", "1", src).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, out)
	}
	contain := render(t, MergeRequest{Clips: []Clip{{Path: src}}, OutputAspectRatio: "9:16"})
	if p := pixel(t, contain, "0.5"); p[0] > 160 || p[2] > 160 {
		t.Fatalf("letterboxed frame should average dark bars with colour: %v", p)
	}
	left := render(t, MergeRequest{Clips: []Clip{{Path: src, Look: ClipLook{Fit: "cover", PanX: -1}}}, OutputAspectRatio: "9:16"})
	if p := pixel(t, left, "0.5"); p[0] < 150 || p[2] > 90 {
		t.Fatalf("cover panned left should show only red: %v", p)
	}
	right := render(t, MergeRequest{Clips: []Clip{{Path: src, Look: ClipLook{Fit: "cover", PanX: 1}}}, OutputAspectRatio: "9:16"})
	if p := pixel(t, right, "0.5"); p[2] < 150 || p[0] > 90 {
		t.Fatalf("cover panned right should show only blue: %v", p)
	}
	grey := render(t, MergeRequest{Clips: []Clip{{Path: src, Look: ClipLook{Fit: "cover", PanX: -1, Saturation: -1}}}, OutputAspectRatio: "9:16"})
	if p := pixel(t, grey, "0.5"); math.Abs(float64(p[0])-float64(p[2])) > 12 || math.Abs(float64(p[0])-float64(p[1])) > 12 {
		t.Fatalf("saturation -1 should be grey: %v", p)
	}
}

func TestJoinClipsGraph(t *testing.T) {
	t.Parallel()
	legacy := joinClips([]Clip{{}, {}, {}}, []float64{1, 2, 3})
	if len(legacy) != 1 || legacy[0] != "[v0][a0][v1][a1][v2][a2]concat=n=3:v=1:a=1[cv][ca]" {
		t.Fatalf("no-transition graph changed: %v", legacy)
	}
	mixed := strings.Join(joinClips([]Clip{{}, {Transition: "fade", TransitionDuration: .5}, {}}, []float64{2, 3, 1}), ";")
	for _, want := range []string{
		"[v0][v1]xfade=transition=fade:duration=0.500000:offset=1.500000[jv1]",
		"[a0][a1]acrossfade=d=0.500000:c1=tri:c2=tri[ja1]",
		"[jv1][ja1][v2][a2]concat=n=2:v=1:a=1[jv2][ja2]",
		"[jv2]null[cv]", "[ja2]anull[ca]",
	} {
		if !strings.Contains(mixed, want) {
			t.Fatalf("graph missing %q:\n%s", want, mixed)
		}
	}
	clips, durations := []Clip{{}, {Transition: "wipeleft", TransitionDuration: 1}}, []float64{2, 2}
	if !validTransition(clips, durations, 1) {
		t.Fatal("half-length transition rejected")
	}
	for name, c := range map[string][]Clip{"first": {{Transition: "fade", TransitionDuration: .5}, {}}, "unknown": {{}, {Transition: "spin", TransitionDuration: .5}}, "too long": {{}, {Transition: "fade", TransitionDuration: 1.2}}, "too short": {{}, {Transition: "fade", TransitionDuration: .05}}, "orphan duration": {{}, {TransitionDuration: .5}}} {
		bad := false
		for i := range c {
			bad = bad || !validTransition(c, durations, i)
		}
		if !bad {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestRealMergeCrossfadeOverlapsClips(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	red, blue := testClip(t, d, "red", "red", "160x90", "25", true), testClip(t, d, "blue", "blue", "160x90", "25", true)
	// Two 1 s clips with a 0.5 s crossfade last 1.5 s; mid-transition is a red/blue blend.
	p := render(t, MergeRequest{Clips: []Clip{{Path: red}, {Path: blue, Transition: "fade", TransitionDuration: .5}}, OutputAspectRatio: "16:9"})
	info, e := probe(context.Background(), p)
	if e != nil || !info.Audio || math.Abs(info.Duration-1.5) > .1 {
		t.Fatalf("crossfade output %+v %v", info, e)
	}
	before, mid, after := pixel(t, p, "0.2"), pixel(t, p, "0.75"), pixel(t, p, "1.3")
	if before[0] < 150 || after[2] < 150 || mid[0] < 50 || mid[2] < 50 {
		t.Fatalf("crossfade colours before=%v mid=%v after=%v", before, mid, after)
	}
	wipe := render(t, MergeRequest{Clips: []Clip{{Path: red}, {Path: blue, Transition: "wipeleft", TransitionDuration: .5}, {Path: red}}, OutputAspectRatio: "16:9"})
	if info, e = probe(context.Background(), wipe); e != nil || math.Abs(info.Duration-2.5) > .1 {
		t.Fatalf("wipe + plain concat output %+v %v", info, e)
	}
}
