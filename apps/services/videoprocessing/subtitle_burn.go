package videoprocessing

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// subtitleStyle is how burned subtitles look: bundled Kanit (Thai marks placed by libass), white
// with a dark outline and a soft shadow so they read on bright and dark shots, centred at the
// bottom. Sizes are in libass script units (an SRT script is 288 units high): 20 ≈ 7 % of the
// frame height.
const subtitleStyle = "FontName=Kanit,FontSize=20,PrimaryColour=&H00FFFFFF,OutlineColour=&H00000000,BackColour=&H80000000,BorderStyle=1,Outline=1.6,Shadow=0.8,Alignment=2,MarginV=28"

// subtitleParts burns the SubRip file srt over the stream labelled in, as out. The file and the
// font are copied into dir so the filter only ever sees parser-safe paths.
func subtitleParts(ctx context.Context, srt, in, out, dir string) (string, error) {
	if !SupportsSubtitles(ctx) {
		return "", fmt.Errorf("videoprocessing: subtitles unavailable: FFmpeg lacks the libass subtitles filter")
	}
	data, e := os.ReadFile(srt)
	if e != nil {
		return "", fmt.Errorf("videoprocessing: subtitles: %w", e)
	}
	fonts := filepath.Join(dir, "subfonts")
	if e = os.MkdirAll(fonts, 0o700); e != nil {
		return "", e
	}
	if e = os.WriteFile(filepath.Join(fonts, "Kanit-Regular.ttf"), kanitRegular, 0o600); e != nil {
		return "", e
	}
	file := filepath.Join(dir, "subtitles.srt")
	if e = os.WriteFile(file, data, 0o600); e != nil {
		return "", e
	}
	return fmt.Sprintf("%ssubtitles=filename='%s':charenc=UTF-8:fontsdir='%s':force_style='%s'%s", in, file, fonts, subtitleStyle, out), nil
}

// SupportsSubtitles reports whether FFmpeg has the libass subtitles filter.
func SupportsSubtitles(ctx context.Context) bool {
	out, e := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-filters").Output()
	return e == nil && strings.Contains(string(out), " subtitles ")
}
