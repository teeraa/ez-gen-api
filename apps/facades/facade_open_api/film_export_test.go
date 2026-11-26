package facade_open_api

import (
	"math"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
)

func TestFilmExportClips(t *testing.T) {
	d := filmDocument{Shots: []filmShot{
		{ID: "a", Dialogue: []filmLine{{Line: "ไปกันเถอะ"}, {Line: "เร็วเข้า"}}, Video: &filmVideo{Status: "succeeded", ClipKey: "k1"}},
		{ID: "b", Video: &filmVideo{Status: "processing"}},
		{ID: "c"},
		{ID: "d", Video: &filmVideo{Status: "succeeded", ClipKey: "k2"}},
	}}
	keys, subs, skipped := filmExportClips(&d, nil)
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" || skipped != 2 {
		t.Fatalf("keys=%v skipped=%d", keys, skipped)
	}
	if subs[0] != "ไปกันเถอะ เร็วเข้า" || subs[1] != "" {
		t.Fatalf("subs=%q", subs)
	}
	if filmExportSource(keys) == filmExportSource([]string{"k2", "k1"}) {
		t.Fatal("source must depend on clip order")
	}
}

func TestFilmCuesTimedTrimmedAndUntimed(t *testing.T) {
	captions := [][]filmCaption{
		// Clip 1: two dubbed lines; the clip starts 1 s in (trim), so the first line starts at 0.
		{{Text: "สวัสดีครับ", At: 1, Seconds: 1.5, Timed: true}, {Text: "ไปกันเถอะ", At: 3.2, Seconds: 1, Timed: true}},
		// Clip 2: native speech, text only: spread over the clip after a lead-in.
		{{Text: "ไม่เป็นไร"}, {Text: ""}, {Text: "ค่อย ๆ คิด"}},
		// Clip 3: no dialogue.
		{},
	}
	spans := []filmClipSpan{{Offset: 0, TrimStart: 1, Kept: 4}, {Offset: 4, Kept: 5}, {Offset: 9, Kept: 3}}
	cues := filmCues(captions, spans, 12)
	if len(cues) != 4 {
		t.Fatalf("four cues: %+v", cues)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	if !near(cues[0].Start, 0) || !near(cues[0].End, 1.5) || !near(cues[1].Start, 2.2) || cues[1].Text != "ไปกันเถอะ" {
		t.Fatalf("timed lines follow the trim: %+v", cues[:2])
	}
	if !near(cues[2].Start, 4.3) || !near(cues[3].End, 8.7) || cues[2].End > cues[3].Start {
		t.Fatalf("untimed lines share their clip: %+v", cues[2:])
	}
	if len(filmCues(captions, nil, 12)) != 0 {
		t.Fatal("no spans, no cues")
	}
}

func TestFilmExportCaptionsUseDubbedTimesAndMovedLines(t *testing.T) {
	d := filmDocument{Shots: []filmShot{
		{ID: "a", LineAt: []float64{2}, Dialogue: []filmLine{{Line: "หนึ่ง"}}, Video: &filmVideo{Status: "succeeded", ClipKey: "k1", Lines: []filmDubLine{{Text: "หนึ่ง", At: 0.5, Seconds: 1}}}},
		{ID: "b", Video: &filmVideo{Status: "failed"}},
		{ID: "c", Dialogue: []filmLine{{Line: "สอง"}}, Video: &filmVideo{Status: "succeeded", ClipKey: "k2", Native: true, Lines: []filmDubLine{{Text: "สอง", At: 1, Seconds: 1}}}},
	}}
	got := filmExportCaptions(&d)
	if len(got) != 2 || len(got[0]) != 1 || !got[0][0].Timed || got[0][0].At != 2 || got[1][0].Timed || got[1][0].Text != "สอง" {
		t.Fatalf("captions %+v", got)
	}
}

func TestFilmFinishByStyle(t *testing.T) {
	for style, want := range map[string]string{"cinematic": videoprocessing.FinishFilm, "realistic": videoprocessing.FinishFilm, "anime": "", "": ""} {
		if got := filmFinish(style); got != want {
			t.Errorf("filmFinish(%q) = %q, want %q", style, got, want)
		}
	}
}
