package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
)

func TestAISplitCaptionTextKeepsThaiSyllables(t *testing.T) {
	text := strings.Repeat("สวัสดีครับวันนี้เราจะมาเล่าเรื่องที่น่าสนใจ", 4)
	pieces := aiSplitCaptionText(text, aiCaptionMaxRunes)
	if len(pieces) < 2 || strings.Join(pieces, "") != text {
		t.Fatalf("pieces must cover the text exactly: %q", pieces)
	}
	for _, p := range pieces {
		runes := []rune(p)
		if len(runes) > aiCaptionMaxRunes+8 || aiThaiNoBreakBefore(runes[0]) || aiThaiLeadingVowel(runes[len(runes)-1]) {
			t.Fatalf("unsafe piece %q", p)
		}
	}
	spaced := aiSplitCaptionText("หนึ่ง สอง สาม", 8)
	if len(spaced) != 2 || spaced[0] != "หนึ่ง" || spaced[1] != "สอง สาม" {
		t.Fatalf("prefer spaces: %q", spaced)
	}
}

func TestAICaptionWindowsSplitsLongCoarseSegments(t *testing.T) {
	long := strings.Repeat("ทำไมเรามีห้านิ้ว ", 12) // ~200 runes: dropped entirely before
	transcript := models.EditorAITranscript{HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 2, End: 12, Text: long}}}
	whole := aiCaptionWindows(transcript, models.EditorAIDraftSegment{Start: 0, End: 15})
	if len(whole) < 4 || whole[0].Start != 2 || whole[len(whole)-1].End != 12 {
		t.Fatalf("long segment inside the cut becomes timed pieces: %+v", whole)
	}
	for i, c := range whole {
		if utf8.RuneCountInString(c.Text) > aiCaptionMaxRunes || aiCaptionLines(c.Text) == "" || i > 0 && c.Start != whole[i-1].End {
			t.Fatalf("piece %d %+v", i, c)
		}
	}
	if partial := aiCaptionWindows(transcript, models.EditorAIDraftSegment{Start: 5, End: 15}); len(partial) != 0 {
		t.Fatal("a coarse segment cut in the middle still gets no estimated captions")
	}
}

type proofreadCaller struct {
	reply string
	asked string
}

func (c *proofreadCaller) Available() bool { return true }
func (c *proofreadCaller) Name() string    { return "fake" }
func (c *proofreadCaller) Model() string   { return "fake-1" }
func (c *proofreadCaller) Chat(_ context.Context, in editorai.ChatRequest) (editorai.ChatReply, error) {
	c.asked = in.Turns[0].Text
	return editorai.ChatReply{Text: c.reply, Usage: editorai.ChatUsage{InputTokens: 100, OutputTokens: 20}}, nil
}

func TestAIChatProofreadsCaptionsWithChosenModel(t *testing.T) {
	layers := []models.VideoEditorTextLayer{
		{ID: "a", OriginJobID: aiChatSubtitleOrigin, Text: "ราคาแค่ 59 บาด"},
		{ID: "b", OriginJobID: aiChatSubtitleOrigin, Text: "ร้านเปิดทุกวัน"},
		{ID: "c", OriginJobID: aiChatSubtitleOrigin, Text: "มาเลยครับ"},
		{ID: "manual", Text: "ข้อความของผู้ใช้"},
	}
	reply, _ := json.Marshal(map[string]any{"captions": []map[string]any{
		{"id": "a", "text": "ราคาแค่ 59 บาท", "uncertain": false},
		{"id": "b", "text": "ร้านเปิดทุกวันเลยนะครับมาเลยวันนี้ลดราคาพิเศษ", "uncertain": false}, // rewrite: too long
		{"id": "c", "text": "มาเลยค่ะ", "uncertain": true},
		{"id": "manual", "text": "แก้ของผู้ใช้", "uncertain": false},
	}})
	caller := &proofreadCaller{reply: "```json\n" + string(reply) + "\n```"}
	charged := 0
	s := &aiChatSession{ctx: context.Background(), caller: caller, charge: func() error { charged++; return nil }}
	changed, note := s.refineCaptions(layers)
	if changed != 1 || layers[0].Text != "ราคาแค่ 59 บาท" || layers[1].Text != "ร้านเปิดทุกวัน" || layers[2].Text != "มาเลยครับ" || layers[3].Text != "ข้อความของผู้ใช้" {
		t.Fatalf("changed=%d %+v", changed, layers)
	}
	if strings.Contains(caller.asked, "ข้อความของผู้ใช้") || charged != 1 || s.extraCalls != 1 || s.extra.InputTokens != 100 || !strings.Contains(note, "fake-1") {
		t.Fatalf("only chat captions are sent; the call is charged and its usage kept: %q %d %+v %q", caller.asked, charged, s.extra, note)
	}
	broken := &aiChatSession{ctx: context.Background(), caller: &proofreadCaller{reply: "ขออภัย"}}
	again := []models.VideoEditorTextLayer{{ID: "a", OriginJobID: aiChatSubtitleOrigin, Text: "ราคาแค่ 59 บาด"}}
	if n, _ := broken.refineCaptions(again); n != 0 || again[0].Text != "ราคาแค่ 59 บาด" {
		t.Fatal("an unreadable reply keeps the transcript text")
	}
}

func TestAIPartialSpeechWindowsOnlyCrossedWordlessSpeech(t *testing.T) {
	transcript := models.EditorAITranscript{HasSpeech: true, Segments: []models.EditorAISpeech{
		{Start: 0, End: 10, Text: "ช่วงยาวที่ถูกตัดคร่อม"},                                                        // crossed: window 4-10
		{Start: 10, End: 12, Text: "ช่วงที่อยู่ในคลิปทั้งก้อน"},                                                   // inside: no window
		{Start: 12, End: 20, Text: "มีเวลารายคำ", Words: []models.EditorAIWord{{Start: 12, End: 13, Text: "มี"}}}, // has word times: no window
		{Start: 20, End: 30, Text: "ข้ามท้ายคลิป"},                                                                // crossed: window 20-20.4 too short
	}}
	got := aiPartialSpeechWindows(transcript, models.EditorAIDraftSegment{ID: "c1", AssetID: "a1", Start: 4, End: 20.4})
	if len(got) != 1 || got[0] != (aiASRWindow{CutID: "c1", AssetID: "a1", Start: 4, End: 10}) {
		t.Fatalf("windows %+v", got)
	}
}

func TestAISnapToPauses(t *testing.T) {
	transcript := models.EditorAITranscript{Segments: []models.EditorAISpeech{{Start: 2, End: 6}, {Start: 6.5, End: 12}}}
	if s, e, n := aiSnapToPauses(transcript, 2.3, 11.6, .5); s != 2 || e != 12 || n != 2 {
		t.Fatalf("both ends snap: %v %v %d", s, e, n)
	}
	if s, e, n := aiSnapToPauses(transcript, 4, 9, .5); s != 4 || e != 9 || n != 0 {
		t.Fatalf("ends far from a pause stay: %v %v %d", s, e, n)
	}
	if s, e, n := aiSnapToPauses(transcript, 1, 6.9, .5); s != 1 || e != 6.5 || n != 1 {
		t.Fatalf("end just inside the next utterance moves back to its start: %v %v %d", s, e, n)
	}
	if s, e, n := aiSnapToPauses(transcript, 1, 6.2, .5); s != 1 || e != 6.2 || n != 0 {
		t.Fatalf("an end already in the gap between utterances stays: %v %v %d", s, e, n)
	}
	tight := models.EditorAITranscript{Segments: []models.EditorAISpeech{{Start: 2, End: 6}, {Start: 6.05, End: 12}}}
	if s, e, n := aiSnapToPauses(tight, 5.7, 6.3, .5); s != 5.7 || e != 6.3 || n != 0 {
		t.Fatalf("a snap that would collapse the cut below 0.3 s is not applied: %v %v %d", s, e, n)
	}
}

// Regression 2026-11-10: Typhoon captions were cut inside words ("ในต|อนนั้น", "ปล|าคือ").
func TestAICaptionsNeverCutThaiWords(t *testing.T) {
	speech := models.EditorAISpeech{Start: 10, End: 22, Text: "สิ่งมีชีวิตกลุ่มเนื้อในตอนนั้นนักวิทยาศาสตร์เชื่อว่า หรือว่าน่าจะเป็นอารมณ์แบบปลาคืออยู่ในน้ำ ซึ่งในทางวิวัฒนาการแล้ว"}
	for _, p := range aiSplitCoarseSpeech(speech) {
		lines := aiCaptionLines(p.Text)
		if lines == "" || strings.Count(lines, "\n") > 1 {
			t.Fatalf("a caption is one or two lines: %q", lines)
		}
		for _, bad := range []string{"ในต\n", "\nอน", "ปล\n", "\nาคือ", "เชื่\n", "หรื\n"} {
			if strings.Contains(lines, bad) || strings.HasPrefix(p.Text, "อว่า") || strings.HasPrefix(p.Text, "าคือ") {
				t.Fatalf("word cut %q in %q", bad, lines)
			}
		}
	}
}

// Regression 2026-11-10: captions ran past their clip into the next one (and onto a second
// text track) around transitions.
func TestAIOwnSpanKeepsCaptionsInTheirClip(t *testing.T) {
	if own, end := aiOwnSpan(19, 18.5, 13.5, true, 1); own != 19 || end != 31 {
		t.Fatalf("own %v end %v", own, end)
	}
	if own, end := aiOwnSpan(0, 0, 5, false, 0); own != 0 || end != 5 {
		t.Fatal("first clip without transitions is all its own")
	}
}

// Regression 2026-11-10: wrapping between words can need more than 6 lines for a long
// title; a 7-line layer failed the whole draft (normalizeVETextLayers allows 6).
func TestAICaptionLinesNeverExceedTextLayerLimit(t *testing.T) {
	long := strings.Repeat("Storyboard ", 13) // 143 runes of 10-letter words: 7+ lines at 24 columns
	if got := aiCaptionLines(long); got != "" && strings.Count(got, "\n") > aiCaptionMaxLines-1 {
		t.Fatalf("too many lines: %q", got)
	}
	if got := aiCaptionLines("ขั้นตอนการสร้างภาพและวิดีโอด้วย Story Flow"); got == "" || strings.Count(got, "\n") > 1 {
		t.Fatalf("short title stays: %q", got)
	}
}

func TestAIPlanFailureMessageNamesTheCheck(t *testing.T) {
	msg, ok := aiPlanFailureMessage(fmt.Errorf("wrapped: %w", aiInvalidPlan("job", "invalid_composition")))
	if !ok || !strings.Contains(msg, "ข้อความหรือเสียงประกอบ") || !errors.Is(aiInvalidPlan("job", "x"), editorai.ErrInvalid) {
		t.Fatalf("%q %v", msg, ok)
	}
}
