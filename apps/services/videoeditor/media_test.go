package videoeditor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMagicRejectsNonVideo(t *testing.T) {
	t.Parallel()
	for _, b := range [][]byte{nil, []byte("not a video at all"), []byte("RIFFabcdefghWEBP")} {
		if Magic(b) {
			t.Fatal("accepted non-video")
		}
	}
}
func TestInspectRejectsForgedMP4(t *testing.T) {
	t.Parallel()
	if !Available() {
		t.Skip("requires media tools")
	}
	path := filepath.Join(t.TempDir(), "forged")
	if e := os.WriteFile(path, []byte("0000ftypisom0000000000000000"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Inspect(context.Background(), path); !errors.Is(e, ErrInvalid) {
		t.Fatal("accepted forgedMP4", e)
	}
}

func TestMagicAcceptsAudioContainers(t *testing.T) {
	t.Parallel()
	for _, b := range [][]byte{[]byte("RIFF\x00\x00\x00\x00WAVEfmt "), []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00\x00"), {0xff, 0xfb, 0x90, 0x64, 0, 0, 0, 0, 0, 0, 0, 0, 0}} {
		if !Magic(b) {
			t.Fatalf("rejected %q", b)
		}
	}
}

func mediaFixture(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	full := append([]string{"-v", "error", "-y"}, args...)
	if out, e := exec.Command("ffmpeg", append(full, path)...).CombinedOutput(); e != nil {
		t.Fatalf("fixture %s: %v %s", name, e, out)
	}
	return path
}

func TestInspectAudioFormats(t *testing.T) {
	t.Parallel()
	if !Available() {
		t.Skip("requires media tools")
	}
	tone := []string{"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1.5"}
	cover := mediaFixture(t, "cover.png", "-f", "lavfi", "-i", "color=c=red:s=64x64", "-frames:v", "1")
	cases := map[string]struct {
		path string
		mime string
	}{
		"mp3":           {mediaFixture(t, "a.mp3", append(tone, "-c:a", "libmp3lame")...), "audio/mpeg"},
		"wav":           {mediaFixture(t, "a.wav", append(tone, "-c:a", "pcm_s16le")...), "audio/wav"},
		"m4a":           {mediaFixture(t, "a.m4a", append(tone, "-c:a", "aac")...), "audio/mp4"},
		"mp3 cover art": {mediaFixture(t, "c.mp3", append(tone, "-i", cover, "-map", "0:a", "-map", "1:v", "-c:a", "libmp3lame", "-c:v", "png", "-disposition:v", "attached_pic", "-id3v2_version", "3")...), "audio/mpeg"},
		"m4a cover art": {mediaFixture(t, "c.m4a", append(tone, "-i", cover, "-map", "0:a", "-map", "1:v", "-c:a", "aac", "-c:v", "png", "-disposition:v", "attached_pic")...), "audio/mp4"},
	}
	for name, c := range cases {
		m, e := Inspect(context.Background(), c.path)
		if e != nil {
			t.Errorf("%s rejected: %v", name, e)
			continue
		}
		if m.Kind != "audio" || m.MIME != c.mime || !m.HasAudio || m.Width != 0 || m.Height != 0 || len(m.Frames) != 0 || m.Duration < 1.4 || m.Duration > 1.7 {
			t.Errorf("%s inspected as %+v", name, m)
		}
	}
	video := mediaFixture(t, "v.mp4", "-f", "lavfi", "-i", "color=c=blue:s=160x90:d=1", "-f", "lavfi", "-i", "sine=duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	if m, e := Inspect(context.Background(), video); e != nil || m.Kind != "video" || m.Width != 160 || len(m.Frames) == 0 {
		t.Fatalf("video inspection changed: %+v %v", m, e)
	}
	silent := mediaFixture(t, "s.m4a", "-f", "lavfi", "-i", "color=c=blue:s=64x64:d=1", "-frames:v", "1", "-c:v", "png", "-disposition:v", "attached_pic", "-f", "mp4")
	if _, e := Inspect(context.Background(), silent); !errors.Is(e, ErrInvalid) {
		t.Fatal("cover art without an audio stream accepted", e)
	}
	truncated := filepath.Join(t.TempDir(), "t.wav")
	data, _ := os.ReadFile(cases["wav"].path)
	if e := os.WriteFile(truncated, data[:30], 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Inspect(context.Background(), truncated); !errors.Is(e, ErrInvalid) {
		t.Fatal("truncated wav accepted", e)
	}
}

func TestInspectAudioRejectsMultiStreamADTSAndCodecs(t *testing.T) {
	t.Parallel()
	if !Available() {
		t.Skip("requires media tools")
	}
	tone := []string{"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1"}
	two := mediaFixture(t, "two.m4a", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-f", "lavfi", "-i", "sine=frequency=660:duration=1", "-map", "0:a", "-map", "1:a", "-c:a", "aac", "-f", "mp4")
	adts := mediaFixture(t, "raw.aac", append(tone, "-c:a", "aac", "-f", "adts")...)
	opus := mediaFixture(t, "opus.m4a", append(tone, "-c:a", "libopus", "-f", "mp4")...)
	for name, path := range map[string]string{"two audio streams": two, "raw ADTS": adts, "opus in mp4": opus} {
		if _, e := Inspect(context.Background(), path); !errors.Is(e, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, e)
		}
	}
}
