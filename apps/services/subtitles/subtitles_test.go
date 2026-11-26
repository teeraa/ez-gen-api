package subtitles

import (
	"strings"
	"testing"
)

func TestWidthSkipsToneMarksAndUpperLowerVowels(t *testing.T) {
	for s, want := range map[string]int{"ไม่ได้": 4, "น้ำ": 2, "ที่นี่": 2, "สวัสดีครับ": 7, "abc": 3, "กุ้ง": 2} {
		if got := Width(s); got != want {
			t.Errorf("Width(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestWrapKeepsLinesWithinLimitAndBreaksAtSpaces(t *testing.T) {
	text := "เธอจะไปไหนน่ะ รอฉันด้วยสิ ฉันยังไม่ได้กินข้าวเลยนะ แล้วเราจะไปทันรถไฟเที่ยวสุดท้ายไหม"
	lines := Wrap(text, MaxLineWidth)
	if len(lines) < 2 {
		t.Fatalf("long line wraps: %q", lines)
	}
	for _, l := range lines {
		if Width(l) > MaxLineWidth {
			t.Fatalf("line %q is %d wide", l, Width(l))
		}
	}
	if strings.Join(lines, " ") != text {
		t.Fatalf("wrapping at spaces keeps the words: %q", lines)
	}
}

func TestWrapSplitsALongThaiRunAtSafePlaces(t *testing.T) {
	text := strings.Repeat("ไม่เป็นไรเลยน้ำแข็ง", 4) // no spaces, 60+ wide
	lines := Wrap(text, MaxLineWidth)
	if strings.Join(lines, "") != text {
		t.Fatalf("no character lost: %q", lines)
	}
	for _, l := range lines {
		rs := []rune(l)
		if Width(l) > MaxLineWidth || noBreakBefore(rs[0]) || noBreakAfter(rs[len(rs)-1]) {
			t.Fatalf("bad break: %q", l)
		}
	}
}

func TestPlaceSplitsLongLinesIntoTwoLineCuesOverTheSpokenTime(t *testing.T) {
	text := strings.Repeat("พูดยาวมากจนต้องแบ่ง ", 12)
	cues := Place(text, 10, 22)
	if len(cues) < 2 || cues[0].Start != 10 || cues[len(cues)-1].End != 22 {
		t.Fatalf("cues cover the line: %+v", cues)
	}
	for i, c := range cues {
		if n := strings.Count(c.Text, "\n") + 1; n > MaxLines {
			t.Fatalf("cue %d has %d lines", i, n)
		}
		if i > 0 && c.Start != cues[i-1].End {
			t.Fatalf("cues follow each other: %+v", cues)
		}
	}
	if Place("", 0, 2) != nil || Place("ก", 1, 1.1) != nil {
		t.Fatal("nothing to show")
	}
}

func TestTidyMinimumDurationGapAndFilmEnd(t *testing.T) {
	cues := Tidy([]Cue{
		{Start: 5, End: 5.3, Text: "b"},
		{Start: 1, End: 1.4, Text: "a"},  // short, room after → 5/6 s
		{Start: 5.5, End: 7, Text: "c"},  // b is short but c follows: b ends a gap before c
		{Start: 9.5, End: 12, Text: "d"}, // past the film end
		{Start: 9.95, End: 10.05, Text: "e"},
	}, 10)
	if len(cues) != 4 || cues[0].Text != "a" || cues[0].End < 1+MinDuration-1e-9 {
		t.Fatalf("sorted, a lengthened: %+v", cues)
	}
	if got := cues[1].End; got > 5.5-MinGap+1e-9 {
		t.Fatalf("b leaves a gap before c: %v", got)
	}
	if cues[3].Text != "d" || cues[3].End > 10 {
		t.Fatalf("d cut at the film end, sliver e dropped: %+v", cues)
	}
}

func TestSRTAndVTT(t *testing.T) {
	cues := []Cue{{Start: 0.5, End: 2.25, Text: "สวัสดี <ครับ>\nบรรทัดสอง"}, {Start: 3661.0004, End: 3662, Text: "a & b"}}
	srt := string(SRT(cues))
	if !strings.HasPrefix(srt, "1\n00:00:00,500 --> 00:00:02,250\nสวัสดี <ครับ>\nบรรทัดสอง\n\n2\n01:01:01,000 --> 01:01:02,000\n") {
		t.Fatalf("srt:\n%s", srt)
	}
	vtt := string(VTT(cues))
	if !strings.HasPrefix(vtt, "WEBVTT\n\n00:00:00.500 --> 00:00:02.250\nสวัสดี &lt;ครับ&gt;\n") || !strings.Contains(vtt, "a &amp; b") {
		t.Fatalf("vtt:\n%s", vtt)
	}
	if strings.Contains(strings.Join(Chunks("x {\\an8} y"), ""), "{") {
		t.Fatal("ASS overrides removed")
	}
}
