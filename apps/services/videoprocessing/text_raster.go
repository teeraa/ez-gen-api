package videoprocessing

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// rasterTextNeeded reports whether an editor layer is drawn as a still image (then
// overlaid) instead of a live drawtext: stretched text, a rounded box, or an accent colour.
func rasterTextNeeded(tl TextLayer) bool {
	return tl.EditorStyle && (textScale(tl.ScaleX) != 1 || textScale(tl.ScaleY) != 1 || tl.BoxOpacity > 0 || tl.AccentText != "")
}

// textBoxRadius is the rounded box's corner radius in output pixels: 0.6 × the font
// size, at most half the box (a pill). The Web preview uses the same rule
// (tracks.ts textStylePreview).
func textBoxRadius(fontSize int, boxW, boxH float64) float64 {
	return math.Min(0.6*float64(fontSize), math.Min(boxW, boxH)/2)
}

// rasterEditorText draws tl's text (no drawtext box) at (pad,pad) on a transparent
// w×h canvas and decodes it.
func rasterEditorText(ctx context.Context, tl TextLayer, height int, font, text string, pad, w, h int, output string) (*image.NRGBA, error) {
	filter := buildEditorDrawtext(tl, 0, 1, height, font, text)
	filter = strings.Replace(filter, "x='max(0,min(w-text_w,w*0.000000-text_w/2))'", fmt.Sprintf("x=%d", pad), 1)
	filter = strings.Replace(filter, "y='max(0,min(h-text_h,h*0.000000-text_h/2))'", fmt.Sprintf("y=%d", pad), 1)
	out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black@0.0:s=%dx%d:r=1,format=rgba", w, h), "-vf", "format=rgba,"+filter, "-frames:v", "1", "-threads", "1", "-y", output).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("rasterize editor text: %w: %s", err, lastLines(string(out), 2))
	}
	return readNRGBA(output)
}

func readNRGBA(path string) (*image.NRGBA, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("read text raster: %w", err)
	}
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out, nil
}

// measureEditorText returns drawtext's text_w for s in tl's font and size.
func measureEditorText(ctx context.Context, tl TextLayer, height int, font, dir, s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	path := filepath.Join(dir, "measure.txt")
	if err := os.WriteFile(path, []byte(s), 0600); err != nil {
		return 0, err
	}
	w, _, err := editorTextDimensions(ctx, buildEditorDrawtext(tl, 0, 1, height, font, path))
	return w, err
}

type accentSpan struct{ line, x0, x1 int }

// accentSpans finds every occurrence of tl.AccentText per line and returns its
// horizontal range inside the text block (block-relative px). Lines share one height
// (y_align=font) and are aligned inside the widest line like drawtext text_align.
func accentSpans(ctx context.Context, tl TextLayer, height int, font, dir, text string, blockW int) ([]accentSpan, error) {
	spans := []accentSpan{}
	for i, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, tl.AccentText) {
			continue
		}
		lineW, err := measureEditorText(ctx, tl, height, font, dir, line)
		if err != nil {
			return nil, err
		}
		left := (blockW - lineW) / 2
		switch tl.Align {
		case "left":
			left = 0
		case "right":
			left = blockW - lineW
		}
		for from := 0; ; {
			at := strings.Index(line[from:], tl.AccentText)
			if at < 0 {
				break
			}
			start := from + at
			end := start + len(tl.AccentText)
			x0, err := measureEditorText(ctx, tl, height, font, dir, line[:start])
			if err != nil {
				return nil, err
			}
			x1, err := measureEditorText(ctx, tl, height, font, dir, line[:end])
			if err != nil {
				return nil, err
			}
			spans = append(spans, accentSpan{i, left + x0, left + x1})
			from = end
		}
	}
	return spans, nil
}

// paintAccent copies accent-coloured pixels over base inside each span's line band.
// Both rasters carry the same outline and shadow, so only the fill colour changes.
func paintAccent(base, accent *image.NRGBA, spans []accentSpan, pad, lines, blockH int) {
	b := base.Bounds()
	lineH := float64(blockH) / float64(lines)
	for _, s := range spans {
		y0, y1 := pad+int(math.Round(float64(s.line)*lineH)), pad+int(math.Round(float64(s.line+1)*lineH))
		if s.line == 0 {
			y0 = 0
		}
		if s.line == lines-1 {
			y1 = b.Dy()
		}
		draw.Draw(base, image.Rect(pad+s.x0, y0, pad+s.x1, y1).Intersect(b), accent, image.Pt(pad+s.x0, y0), draw.Src)
	}
}

// roundedBox puts text over a rounded box filling (0,0)-(w,h) in color@opacity,
// anti-aliased with 4×4 coverage sampling at the corners.
func roundedBox(text *image.NRGBA, w, h int, radius float64, hex string, opacity float64) *image.NRGBA {
	out := image.NewNRGBA(text.Bounds())
	v, _ := strconv.ParseUint(hex, 16, 32)
	fill := color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
	mask := image.NewAlpha(text.Bounds())
	for y := 0; y < h && y < mask.Rect.Dy(); y++ {
		for x := 0; x < w && x < mask.Rect.Dx(); x++ {
			hits := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px, py := float64(x)+(float64(sx)+.5)/4, float64(y)+(float64(sy)+.5)/4
					cx, cy := math.Max(radius, math.Min(float64(w)-radius, px)), math.Max(radius, math.Min(float64(h)-radius, py))
					if (px-cx)*(px-cx)+(py-cy)*(py-cy) <= radius*radius {
						hits++
					}
				}
			}
			mask.SetAlpha(x, y, color.Alpha{uint8(math.Round(float64(hits) / 16 * opacity * 255))})
		}
	}
	draw.DrawMask(out, out.Bounds(), image.NewUniform(fill), image.Point{}, mask, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), text, text.Bounds().Min, draw.Over)
	return out
}

func writePNG(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err = png.Encode(file, img); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
