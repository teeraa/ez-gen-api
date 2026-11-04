package facade_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/models"
	"strings"
	"testing"
)

func TestAICoarseSpeechKeepsContextBoundariesInBothModes(t *testing.T) {
	for _, remove := range []bool{false, true} {
		in := models.EditorAIInput{TargetDuration: 60, RemoveSilence: remove, Sources: []models.EditorAISource{{ID: "source", Duration: 79}}}
		speech := []models.EditorAITranscript{{AssetID: "source", HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 0, End: 25.6, Text: "setup"}, {Start: 25.6, End: 52.1, Text: "answer"}, {Start: 52.1, End: 79, Text: "ending"}}}}
		windows := buildAIEvidence(in, speech, nil)
		boundaries := map[float64]bool{0: true, 25.6: true, 52.1: true, 79: true}
		joined := false
		for _, window := range windows {
			if !boundaries[window.Start] || !boundaries[window.End] {
				t.Fatalf("invented six-second/word boundary: %+v", window)
			}
			if window.Start == 0 && window.End == 52.1 {
				joined = true
			}
		}
		if !joined {
			t.Fatalf("mode %v lacks continuous question/answer candidate: %+v", remove, windows)
		}
	}
}
func TestAIShortTargetHasExplicitPartialContextFallback(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 10, Sources: []models.EditorAISource{{ID: "source", Duration: 30}}}
	rows := []models.EditorAITranscript{{AssetID: "source", HasSpeech: true, Segments: []models.EditorAISpeech{{End: 30, Text: "long context"}}}}
	windows := buildAIEvidence(in, rows, nil)
	if len(windows) != 1 || windows[0].End != 10 || !strings.Contains(windows[0].Description, "Partial source context") {
		t.Fatal(windows)
	}
}
func TestAIConceptUsesRequestedTargetUnlessShortfallExplained(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 120}}}
	evidence := buildAIEvidence(in, nil, nil)
	duration, reason := aiConceptDuration(30, "", in, evidence)
	if duration != 60 || reason != "" {
		t.Fatalf("unexplained model default took over target: %v %q", duration, reason)
	}
	in.Sources[0].Duration = 15
	short := buildAIEvidence(in, nil, nil)
	if d, _ := aiConceptDuration(60, "", in, short); d > 15 {
		t.Fatalf("estimate exceeds available footage: %v", d)
	}
	in.Sources[0].Duration = 120
	duration, reason = aiConceptDuration(30, "ส่วนคำตอบนี้จบสมบูรณ์ในช่วงสั้นกว่าเป้า", in, evidence)
	if duration != 30 || reason == "" {
		t.Fatal("explicit shorter alternative unavailable")
	}
}
func TestAIShortfallReasonDoesNotInventInsufficientSource(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 120}}}
	reason := aiDurationReason(30, "", in, buildAIEvidence(in, nil, nil))
	if !strings.Contains(reason, "ยังไม่ได้ยืนยัน") {
		t.Fatal(reason)
	}
	in.Sources[0].Duration = 20
	reason = aiDurationReason(20, "", in, buildAIEvidence(in, nil, nil))
	if !strings.Contains(reason, "20.0") {
		t.Fatal(reason)
	}
	if aiDurationReason(60, reason, in, nil) != "" {
		t.Fatal("stale reason remained at target")
	}
}
func TestAISelectedOverlapAndContiguousMerge(t *testing.T) {
	rows := []models.EditorAIDraftSegment{{AssetID: "s", Start: 0, End: 25.6}, {AssetID: "s", Start: 25.6, End: 52.1}}
	if aiSelectedOverlap(rows) {
		t.Fatal("adjacent rejected")
	}
	merged := aiMergeAdjacent(rows)
	if len(merged) != 1 || merged[0].End != 52.1 {
		t.Fatal(merged)
	}
	rows[1].Start = 20
	if !aiSelectedOverlap(rows) {
		t.Fatal("overlapping alternative double counts source")
	}
	rows[1].Start = 26
	if len(aiMergeAdjacent(rows)) != 2 {
		t.Fatal("silence/gap silently merged")
	}
}
