// Package subtitles turns dialogue lines into timed subtitle cues and writes them as SubRip (.srt)
// and WebVTT (.vtt), following the Netflix Thai Timed Text Style Guide: at most 35 characters a
// line (tone marks and above/below vowels are not counted), at most two lines, at least 5/6 s on
// screen and two frames between cues.
package subtitles

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	// MaxLineWidth is the Thai line limit, counted with Width.
	MaxLineWidth = 35
	// MaxLines is the number of lines one cue may show.
	MaxLines = 2
	// MinDuration is the shortest time a cue stays on screen when there is room.
	MinDuration = 5.0 / 6
	// MinGap is the gap kept between two cues (two frames at 24 fps).
	MinGap = 2.0 / 24
	// minCue drops slivers left after trimming or overlap.
	minCue = 0.2
)

// Cue is one subtitle: Text may hold up to MaxLines lines separated by "\n".
type Cue struct {
	Start, End float64
	Text       string
}

// Width counts the characters of s that take a place on the line: combining marks (Thai tone
// marks and the vowels written above or below a consonant) do not.
func Width(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) && !unicode.IsControl(r) {
			n++
		}
	}
	return n
}

// Thai letters a line must not break before (they close the previous syllable) or after (they
// are written before the consonant they belong to).
func noBreakBefore(r rune) bool {
	return unicode.Is(unicode.Mn, r) || strings.ContainsRune("ะาำๅๆฯ", r) || unicode.IsPunct(r)
}
func noBreakAfter(r rune) bool { return strings.ContainsRune("เแโใไ", r) }

// splitWord cuts a word wider than width into pieces at places a Thai line may break.
func splitWord(word string, width int) []string {
	rs := []rune(word)
	out := []string{}
	for len(rs) > 0 {
		if Width(string(rs)) <= width {
			out = append(out, string(rs))
			break
		}
		// Without a dictionary the best guesses for a word start are a leading vowel (เ แ โ ใ ไ
		// always open a syllable) and the place after ๆ or ฯ; after ะ or ำ the syllable ends (after
		// า it often does not: ตาม, บาล). Take the best such place in the second half of the line,
		// else any safe place.
		cut, best, bestScore, w := 0, 0, 0, 0
		for i, r := range rs {
			if !unicode.Is(unicode.Mn, r) {
				w++
			}
			if w > width {
				break
			}
			if i+1 < len(rs) && !noBreakBefore(rs[i+1]) && !noBreakAfter(r) {
				cut = i + 1
				score := 0
				switch {
				case noBreakAfter(rs[i+1]) || strings.ContainsRune("ๆฯ", r):
					score = 2
				case strings.ContainsRune("ะำ", r):
					score = 1
				}
				if score > 0 && score >= bestScore && 2*w >= width {
					best, bestScore = i+1, score
				}
			}
		}
		if best > 0 {
			cut = best
		}
		if cut == 0 { // nowhere to break politely: cut at the limit, after any marks
			for cut < len(rs) && (Width(string(rs[:cut+1])) <= width || unicode.Is(unicode.Mn, rs[cut])) {
				cut++
			}
		}
		out = append(out, string(rs[:cut]))
		rs = rs[cut:]
	}
	return out
}

// Wrap breaks text into lines of at most width, at spaces first (Thai separates phrases with
// spaces) and inside a too-long phrase at syllable-safe places.
func Wrap(text string, width int) []string {
	lines, line := []string{}, ""
	for _, word := range strings.Fields(sanitize(text)) {
		for _, piece := range splitWord(word, width) {
			switch {
			case line == "":
				line = piece
			case Width(line)+1+Width(piece) <= width:
				line += " " + piece
			default:
				lines, line = append(lines, line), piece
			}
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// Chunks groups the wrapped lines of text into cue texts of at most MaxLines lines.
func Chunks(text string) []string {
	lines := Wrap(text, MaxLineWidth)
	out := []string{}
	for i := 0; i < len(lines); i += MaxLines {
		out = append(out, strings.Join(lines[i:min(i+MaxLines, len(lines))], "\n"))
	}
	return out
}

// Place spreads one spoken line over [start, end): a line too long for one cue becomes several,
// each on screen for a share of the time that follows its length.
func Place(text string, start, end float64) []Cue {
	chunks := Chunks(text)
	if len(chunks) == 0 || !finite(start) || !finite(end) || end-start < minCue {
		return nil
	}
	total := 0
	for _, c := range chunks {
		total += Width(c) + 1
	}
	out, at := []Cue{}, start
	for i, c := range chunks {
		next := at + (end-start)*float64(Width(c)+1)/float64(total)
		if i == len(chunks)-1 {
			next = end
		}
		out = append(out, Cue{Start: at, End: next, Text: c})
		at = next
	}
	return out
}

// Tidy orders cues on a film of `total` seconds: each stays at least MinDuration when the next
// cue and the film end allow, keeps MinGap before the next one, and slivers are dropped.
func Tidy(cues []Cue, total float64) []Cue {
	in := []Cue{}
	for _, c := range cues {
		c.Start, c.End = math.Max(0, c.Start), math.Min(total, c.End)
		if c.Text != "" && finite(c.Start) && finite(c.End) && c.End-c.Start >= minCue {
			in = append(in, c)
		}
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].Start < in[j].Start })
	out := []Cue{}
	for i, c := range in {
		limit := total
		if i+1 < len(in) {
			limit = in[i+1].Start - MinGap
		}
		if c.End-c.Start < MinDuration {
			c.End = math.Max(c.End, math.Min(c.Start+MinDuration, limit))
		}
		c.End = math.Min(c.End, limit)
		if c.End-c.Start >= minCue {
			out = append(out, c)
		}
	}
	return out
}

// SRT writes cues as SubRip.
func SRT(cues []Cue) []byte {
	var b bytes.Buffer
	for i, c := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, stamp(c.Start, ","), stamp(c.End, ","), c.Text)
	}
	return b.Bytes()
}

// VTT writes cues as WebVTT.
func VTT(cues []Cue) []byte {
	var b bytes.Buffer
	b.WriteString("WEBVTT\n\n")
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	for _, c := range cues {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", stamp(c.Start, "."), stamp(c.End, "."), r.Replace(c.Text))
	}
	return b.Bytes()
}

func stamp(s float64, sep string) string {
	ms := int64(math.Round(math.Max(0, s) * 1000))
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, ms/60000%60, ms/1000%60, sep, ms%1000)
}

// sanitize keeps cue text plain: no blank lines (they end a cue), no ASS override braces or
// backslash codes when the SRT is burned in.
func sanitize(s string) string {
	return strings.NewReplacer("{", "(", "}", ")", "\\", "/").Replace(s)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
