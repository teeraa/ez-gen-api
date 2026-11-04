package videoprocessing

import (
	"encoding/binary"
	"os"
)

// thaiProbe covers a consonant, a vowel and a tone mark; a font missing any of
// them cannot render ordinary Thai captions.
var thaiProbe = []rune{'ก', 'า', '่'}

// FontCoversThai reports whether the TrueType/OpenType font (or the first face of
// a collection) maps the Thai probe characters through its Unicode cmap.
func FontCoversThai(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 64<<20 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return fontCovers(data, thaiProbe)
}

func fontCovers(data []byte, runes []rune) bool {
	base := 0
	if len(data) >= 16 && string(data[:4]) == "ttcf" {
		if binary.BigEndian.Uint32(data[8:12]) == 0 {
			return false
		}
		base = int(binary.BigEndian.Uint32(data[12:16]))
	}
	cmap, ok := sfntTable(data, base, "cmap")
	if !ok || len(cmap) < 4 {
		return false
	}
	n := int(binary.BigEndian.Uint16(cmap[2:4]))
	for _, r := range runes {
		found := false
		for i := 0; i < n && !found; i++ {
			rec := 4 + i*8
			if rec+8 > len(cmap) {
				return false
			}
			platform := binary.BigEndian.Uint16(cmap[rec:])
			encoding := binary.BigEndian.Uint16(cmap[rec+2:])
			if platform != 0 && !(platform == 3 && (encoding == 1 || encoding == 10)) {
				continue
			}
			off := int(binary.BigEndian.Uint32(cmap[rec+4:]))
			if off < 0 || off >= len(cmap) {
				continue
			}
			found = cmapGlyph(cmap[off:], r) != 0
		}
		if !found {
			return false
		}
	}
	return true
}

func sfntTable(data []byte, base int, tag string) ([]byte, bool) {
	if base < 0 || base+12 > len(data) {
		return nil, false
	}
	n := int(binary.BigEndian.Uint16(data[base+4:]))
	for i := 0; i < n; i++ {
		rec := base + 12 + i*16
		if rec+16 > len(data) {
			return nil, false
		}
		if string(data[rec:rec+4]) != tag {
			continue
		}
		off := int(binary.BigEndian.Uint32(data[rec+8:]))
		length := int(binary.BigEndian.Uint32(data[rec+12:]))
		if off < 0 || length < 0 || off+length > len(data) || off+length < off {
			return nil, false
		}
		return data[off : off+length], true
	}
	return nil, false
}

func cmapGlyph(t []byte, r rune) int {
	if len(t) < 2 {
		return 0
	}
	c := uint32(r)
	switch binary.BigEndian.Uint16(t) {
	case 4:
		if len(t) < 14 || c > 0xffff {
			return 0
		}
		seg := int(binary.BigEndian.Uint16(t[6:])) / 2
		ends, starts, deltas, ranges := 14, 16+seg*2, 16+seg*4, 16+seg*6
		if ranges+seg*2 > len(t) {
			return 0
		}
		for i := 0; i < seg; i++ {
			end := uint32(binary.BigEndian.Uint16(t[ends+i*2:]))
			if end < c {
				continue
			}
			start := uint32(binary.BigEndian.Uint16(t[starts+i*2:]))
			if start > c {
				return 0
			}
			delta := binary.BigEndian.Uint16(t[deltas+i*2:])
			rangeOffset := int(binary.BigEndian.Uint16(t[ranges+i*2:]))
			if rangeOffset == 0 {
				return int(uint16(c) + delta)
			}
			at := ranges + i*2 + rangeOffset + int(c-start)*2
			if at+2 > len(t) {
				return 0
			}
			g := binary.BigEndian.Uint16(t[at:])
			if g == 0 {
				return 0
			}
			return int(g + delta)
		}
	case 12:
		if len(t) < 16 {
			return 0
		}
		groups := int(binary.BigEndian.Uint32(t[12:]))
		for i := 0; i < groups; i++ {
			g := 16 + i*12
			if g+12 > len(t) {
				return 0
			}
			start, end := binary.BigEndian.Uint32(t[g:]), binary.BigEndian.Uint32(t[g+4:])
			if c >= start && c <= end {
				return int(binary.BigEndian.Uint32(t[g+8:]) + c - start)
			}
		}
	}
	return 0
}
