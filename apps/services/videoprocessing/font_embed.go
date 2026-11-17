package videoprocessing

import (
	_ "embed"
	"os"
	"unicode"
)

// Kanit (SIL OFL 1.1, fonts/OFL.txt), Latin + Thai subsets merged into one face.
// Regular is the fallback when the configured font has no Thai glyphs; ExtraBold is
// the editor's "heavy" text font (the Web preview loads the same file).
var (
	//go:embed fonts/Kanit-Regular.ttf
	kanitRegular []byte
	//go:embed fonts/Kanit-ExtraBold.ttf
	kanitExtraBold []byte
)

// TextFontHeavy selects the bundled Kanit ExtraBold for a text layer.
const TextFontHeavy = "heavy"

// baseFont is the configured font, or bundled Kanit Regular when that font is missing
// or the layers contain Thai it cannot draw (the default DejaVu would burn empty boxes).
func (p *FFmpegProcessor) baseFont(layers []TextLayer) []byte {
	data, err := os.ReadFile(p.fontFile)
	if err != nil {
		return kanitRegular
	}
	for _, tl := range layers {
		for _, r := range tl.Text {
			if unicode.Is(unicode.Thai, r) {
				if FontCoversThai(p.fontFile) {
					return data
				}
				return kanitRegular
			}
		}
	}
	return data
}
