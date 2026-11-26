package videoprocessing

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func masterNeedsFFmpeg(t *testing.T) {
	t.Helper()
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg not installed")
	}
}

func masterRun(t *testing.T, args ...string) string {
	t.Helper()
	out, e := exec.Command("ffmpeg", args...).CombinedOutput()
	if e != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, e, lastLines(string(out), 6))
	}
	return string(out)
}

// masterFilm writes a 12 s film whose "dialogue" (a 600 Hz tone at about -20 dBFS RMS) plays
// at 2–5 s and 8–10 s over -50 dBFS room tone, like a dubbed episode.
func masterFilm(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "film.mp4")
	masterRun(t, "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=gray:s=320x180:r=30:d=12",
		"-f", "lavfi", "-i", "sine=f=600:d=12:sample_rate=48000", "-f", "lavfi", "-i", "anoisesrc=c=brown:a=0.003:d=12:r=48000",
		"-filter_complex", "[1:a]volume='if(between(t,2,5)+between(t,8,10),1.1,0)':eval=frame[sp];[sp][2:a]amix=inputs=2:normalize=0,aformat=channel_layouts=stereo[a]",
		"-map", "0:v", "-map", "[a]", "-c:v", "libx264", "-c:a", "aac", "-ar", "48000", "-shortest", p)
	return p
}

// masterLUFS measures integrated loudness.
func masterLUFS(t *testing.T, path string) float64 {
	t.Helper()
	out := masterRun(t, "-hide_banner", "-nostats", "-i", path, "-af", "ebur128", "-f", "null", "-")
	m := regexp.MustCompile(`I:\s+(-?[0-9.]+) LUFS`).FindAllStringSubmatch(out, -1)
	if len(m) == 0 {
		t.Fatalf("no loudness in %s", lastLines(out, 4))
	}
	v, _ := strconv.ParseFloat(m[len(m)-1][1], 64)
	return v
}

// masterBand is the mean level (dB) below 250 Hz — the music bed only — in [start, start+d).
func masterBand(t *testing.T, path string, start, d string) float64 {
	t.Helper()
	out := masterRun(t, "-hide_banner", "-nostats", "-ss", start, "-t", d, "-i", path, "-af", "lowpass=f=250,lowpass=f=250,volumedetect", "-vn", "-f", "null", "-")
	m := regexp.MustCompile(`mean_volume: (-?[0-9.]+) dB`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no level in %s", lastLines(out, 4))
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

func TestMasterAudioDucksMusicUnderDialogueAndHitsDeliveryLoudness(t *testing.T) {
	masterNeedsFFmpeg(t)
	dir := t.TempDir()
	film := masterFilm(t, dir)
	music := filepath.Join(dir, "music.wav")
	masterRun(t, "-v", "error", "-y", "-f", "lavfi", "-i", "sine=f=120:d=20:sample_rate=48000", "-ac", "2", music)
	out := filepath.Join(dir, "out.mp4")
	if e := MasterAudio(context.Background(), film, out, &AudioTrack{Path: music, TrimEnd: 11, At: 1, Volume: 0.6, FadeOut: 1}); e != nil {
		t.Fatal(e)
	}
	if got := masterLUFS(t, out); got < MasterLoudness-1 || got > MasterLoudness+1 {
		t.Fatalf("integrated loudness %.1f LUFS, want %.1f ±1", got, MasterLoudness)
	}
	free, speech := masterBand(t, out, "6.2", "1"), masterBand(t, out, "3", "1.5")
	if free-speech < 8 {
		t.Fatalf("music ducks under dialogue: %.1f dB between lines vs %.1f dB under a line", free, speech)
	}
	if before := masterBand(t, out, "0.2", "0.5"); before > free-30 {
		t.Fatalf("music starts at 1 s: %.1f dB before it", before)
	}
	codec := masterRun(t, "-hide_banner", "-i", out, "-f", "null", "-t", "0.1", "-")
	if !strings.Contains(codec, "h264") {
		t.Fatal("video stream copied")
	}
}

func TestMasterAudioNormalizesWithoutMusicAndLeavesSilenceAlone(t *testing.T) {
	masterNeedsFFmpeg(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.mp4")
	if e := MasterAudio(context.Background(), masterFilm(t, dir), out, nil); e != nil {
		t.Fatal(e)
	}
	if got := masterLUFS(t, out); got < MasterLoudness-1 || got > MasterLoudness+1 {
		t.Fatalf("integrated loudness %.1f LUFS", got)
	}
	silent := filepath.Join(dir, "silent.mp4")
	masterRun(t, "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=gray:s=320x180:r=30:d=3", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-t", "3", "-c:v", "libx264", "-c:a", "aac", silent)
	if e := MasterAudio(context.Background(), silent, filepath.Join(dir, "silent-out.mp4"), nil); e != nil {
		t.Fatalf("a silent film is mastered without loudness gain: %v", e)
	}
}

func TestMasterAudioRejectsBadMusic(t *testing.T) {
	for _, m := range []AudioTrack{{Path: "m.wav", TrimEnd: 0}, {Path: "m.wav", TrimEnd: 2, Volume: 2}, {Path: "", TrimEnd: 2}, {Path: "m.wav", TrimEnd: 2, At: -1}} {
		if _, _, e := masterGraph(&m); e == nil {
			t.Fatalf("music %+v must be rejected", m)
		}
	}
	g, in, e := masterGraph(&AudioTrack{Path: "m.wav", TrimEnd: 5, At: 1.5, Volume: 0.35})
	if e != nil || len(in) != 2 || !strings.Contains(g, "adelay=delays=72000S") || !strings.Contains(g, "[m0][key]sidechaincompress") || !strings.HasSuffix(g, "[mix]") {
		t.Fatalf("graph %q inputs %v err %v", g, in, e)
	}
}

func TestParseLoudnorm(t *testing.T) {
	out := "noise {\n\"x\":1}\n[Parsed_loudnorm_0 @ 0x1]\n{\n\t\"input_i\" : \"-23.50\",\n\t\"input_tp\" : \"-4.10\",\n\t\"input_lra\" : \"6.00\",\n\t\"input_thresh\" : \"-33.90\",\n\t\"target_offset\" : \"0.20\"\n}\n"
	s, ok := parseLoudnorm(out)
	if !ok || s.I != -23.5 || s.TP != -4.1 || s.LRA != 6 || s.Thresh != -33.9 || s.Offset != 0.2 {
		t.Fatalf("%+v %v", s, ok)
	}
	if _, ok = parseLoudnorm(strings.Replace(out, "-23.50", "-inf", 1)); ok {
		t.Fatal("silence is not normalized")
	}
}
