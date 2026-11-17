package videoprocessing

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractLastFrameDecodesActualFinalFrame(t *testing.T) {
	t.Parallel()
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	for i, c := range []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}} {
		im := image.NewRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				im.SetRGBA(x, y, c)
			}
		}
		f, e := os.Create(filepath.Join(dir, string(rune('0'+i))+".png"))
		if e != nil {
			t.Fatal(e)
		}
		if e = png.Encode(f, im); e != nil {
			t.Fatal(e)
		}
		_ = f.Close()
	}
	input := filepath.Join(dir, "fixture.mkv")
	if out, e := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-framerate", "1", "-i", filepath.Join(dir, "%d.png"), "-c:v", "ffv1", "-threads", "1", "-y", input).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %v %s", e, out)
	}
	video, e := os.ReadFile(input)
	if e != nil {
		t.Fatal(e)
	}
	frame, e := NewFFmpegProcessor("").ExtractLastFrame(context.Background(), video)
	if e != nil {
		t.Fatal(e)
	}
	im, e := png.Decode(bytes.NewReader(frame))
	if e != nil {
		t.Fatal(e)
	}
	r, g, b, _ := im.At(8, 8).RGBA()
	if b < 65000 || r > 256 || g > 256 {
		t.Fatalf("extracted an earlier frame: rgb=%d,%d,%d", r, g, b)
	}
}
func TestExtractLastFrameRejectsInvalidCancelledAndNetworkInput(t *testing.T) {
	t.Parallel()
	p := NewFFmpegProcessor("")
	for _, video := range [][]byte{nil, []byte("not a video"), []byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nhttps://invalid.example/frame.ts\n#EXT-X-ENDLIST\n")} {
		if _, e := p.ExtractLastFrame(context.Background(), video); e == nil {
			t.Fatal("invalid or remote input accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.ExtractLastFrame(ctx, []byte("video")); e == nil {
		t.Fatal("cancelled extraction accepted")
	}
}
