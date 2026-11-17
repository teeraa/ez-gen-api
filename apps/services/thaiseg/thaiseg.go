// Package thaiseg finds Thai word boundaries with a dictionary (maximal matching), so
// captions are split and wrapped between words instead of in the middle of one. Thai
// has no spaces between words; breaking only on characters cuts words like "ตอน" or
// "ปลา" in half.
package thaiseg

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"
)

// words_th.txt: PyThaiNLP word list, CC0-1.0 (see NOTICE.md).
//
//go:embed words_th.txt
var rawWords string

const maxWordRunes = 40

var (
	loadOnce sync.Once
	words    map[string]struct{}
	longest  int
)

func load() {
	words = make(map[string]struct{}, 64000)
	for _, line := range strings.Split(rawWords, "\n") {
		word := strings.TrimSpace(line)
		if word == "" || strings.ContainsAny(word, " \t") {
			continue
		}
		n := len([]rune(word))
		if n > maxWordRunes {
			continue
		}
		words[word] = struct{}{}
		if n > longest {
			longest = n
		}
	}
}

// IsThai reports a rune of the Thai block.
func IsThai(r rune) bool { return r >= 0x0E00 && r <= 0x0E7F }

// NoBreakBefore reports runes that belong to the previous character: combining marks
// (above/below vowels, tone marks) and the following vowels ะ า ำ ๅ.
func NoBreakBefore(r rune) bool {
	return unicode.Is(unicode.Mn, r) || r == 'ะ' || r == 'า' || r == 'ำ' || r == 'ๅ'
}

// LeadingVowel reports เ แ โ ใ ไ, which are written before the consonant they follow.
func LeadingVowel(r rune) bool { return r >= 'เ' && r <= 'ไ' }

// canBreak reports whether a word may end between runes[i-1] and runes[i].
func canBreak(runes []rune, i int) bool {
	return i <= 0 || i >= len(runes) || !NoBreakBefore(runes[i]) && !LeadingVowel(runes[i-1])
}

// cluster returns the end of the smallest unit starting at i that never splits a
// syllable mark from its consonant (used for text the dictionary does not know).
func cluster(runes []rune, i, end int) int {
	j := i
	if LeadingVowel(runes[j]) && j+1 < end {
		j++
	}
	j++
	for j < end && NoBreakBefore(runes[j]) {
		j++
	}
	return j
}

// Segment splits text into tokens whose concatenation is exactly text. Thai runs are
// segmented with the dictionary, preferring the fewest unknown characters and then the
// fewest tokens; consecutive unknown pieces stay one token. Spaces and non-Thai runs
// (Latin, digits, punctuation) are their own tokens.
func Segment(text string) []string {
	loadOnce.Do(load)
	runes := []rune(text)
	out := []string{}
	for i := 0; i < len(runes); {
		j := i + 1
		switch {
		case unicode.IsSpace(runes[i]):
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			out = append(out, string(runes[i:j]))
		case IsThai(runes[i]):
			for j < len(runes) && IsThai(runes[j]) {
				j++
			}
			out = append(out, segmentThai(runes[i:j])...)
		default:
			for j < len(runes) && !IsThai(runes[j]) && !unicode.IsSpace(runes[j]) {
				j++
			}
			out = append(out, string(runes[i:j]))
		}
		i = j
	}
	return out
}

func segmentThai(runes []rune) []string {
	n := len(runes)
	type state struct {
		unknown, tokens, prev int
		known, ok             bool
	}
	best := make([]state, n+1)
	best[0].ok = true
	better := func(to int, s state) {
		cur := best[to]
		if !cur.ok || s.unknown < cur.unknown || s.unknown == cur.unknown && s.tokens < cur.tokens {
			best[to] = s
		}
	}
	for i := 0; i < n; i++ {
		if !best[i].ok {
			continue
		}
		from := best[i]
		for l := 1; l <= longest && i+l <= n; l++ {
			if !canBreak(runes, i+l) {
				continue
			}
			if _, ok := words[string(runes[i:i+l])]; ok {
				better(i+l, state{unknown: from.unknown, tokens: from.tokens + 1, prev: i, known: true, ok: true})
			}
		}
		j := cluster(runes, i, n)
		better(j, state{unknown: from.unknown + (j - i), tokens: from.tokens + 1, prev: i, known: false, ok: true})
	}
	type piece struct {
		start, end int
		known      bool
	}
	pieces := []piece{}
	for at := n; at > 0; at = best[at].prev {
		pieces = append(pieces, piece{best[at].prev, at, best[at].known})
	}
	out := []string{}
	for k := len(pieces) - 1; k >= 0; k-- {
		p := pieces[k]
		if !p.known && len(out) > 0 && k+1 < len(pieces) && !pieces[k+1].known {
			out[len(out)-1] += string(runes[p.start:p.end])
			continue
		}
		out = append(out, string(runes[p.start:p.end]))
	}
	return out
}

// Width is the visible width of text: combining marks take no column of their own.
func Width(text string) int {
	n := 0
	for _, r := range text {
		if !unicode.Is(unicode.Mn, r) {
			n++
		}
	}
	return n
}

// Wrap breaks text into lines of at most width visible columns, only between words.
// A single word wider than a line is cut at a syllable-safe point.
func Wrap(text string, width int) []string {
	lines, line := []string{}, ""
	flush := func() {
		if t := strings.TrimSpace(line); t != "" {
			lines = append(lines, t)
		}
		line = ""
	}
	for _, token := range Segment(text) {
		if strings.TrimSpace(token) == "" {
			if line != "" {
				line += " "
			}
			continue
		}
		if Width(line+token) <= width {
			line += token
			continue
		}
		flush()
		for Width(token) > width {
			head, rest := splitWidth(token, width)
			lines = append(lines, head)
			token = rest
		}
		line = token
	}
	flush()
	return lines
}

// splitWidth cuts one over-long word at the last syllable-safe point within width.
func splitWidth(token string, width int) (string, string) {
	runes := []rune(token)
	cut, seen := 0, 0
	for i, r := range runes {
		if !unicode.Is(unicode.Mn, r) {
			seen++
		}
		if seen > width {
			break
		}
		if canBreak(runes, i+1) {
			cut = i + 1
		}
	}
	if cut == 0 || cut >= len(runes) {
		cut = len(runes) / 2
		for cut < len(runes) && !canBreak(runes, cut) {
			cut++
		}
		if cut >= len(runes) {
			return token, ""
		}
	}
	return string(runes[:cut]), string(runes[cut:])
}
