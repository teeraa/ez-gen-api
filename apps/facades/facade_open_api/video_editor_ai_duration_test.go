package facade_open_api

import (
	"encoding/json"
	"fmt"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"strings"
	"testing"
)

func TestAIEvidenceCoversNonSpeechSource(t *testing.T) {
	t.Parallel()
	for _, removeSilence := range []bool{false, true} {
		in := models.EditorAIInput{TargetDuration: 60, RemoveSilence: removeSilence, Sources: []models.EditorAISource{{ID: "source", Duration: 135.5}}}
		windows := buildAIEvidence(in, nil, []models.EditorAIVisual{{AssetID: "source", Summary: "sampled source summary"}})
		end := 0.0
		seen := map[string]bool{}
		for _, window := range windows {
			if window.Start < 0 || window.End > 135.5 || window.End <= window.Start || window.End-window.Start > 60 || seen[window.ID] {
				t.Fatalf("invalid coverage after %v: %+v", end, window)
			}
			seen[window.ID] = true
			if window.End > end {
				end = window.End
			}
		}
		if end != 135.5 {
			t.Fatalf("removeSilence=%v: covered %v seconds, want 135.5", removeSilence, end)
		}
	}
}

func TestAIEvidenceCanPlanSixtySecondsWithoutRepeats(t *testing.T) {
	t.Parallel()
	in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 135.5}}}
	windows := buildAIEvidence(in, nil, nil)
	draft := models.EditorAIDraft{Revision: 1}
	for _, window := range windows[:1] {
		draft.Segments = append(draft.Segments, models.EditorAIDraftSegment{ID: window.ID, AssetID: window.AssetID, Start: window.Start, End: window.End, Volume: 1, Title: "clip", EvidenceID: window.ID})
	}
	if err := validateAIDraft(&draft, in, nil); err != nil {
		t.Fatal(err)
	}
	if draft.Duration != 60 {
		t.Fatalf("got %v seconds", draft.Duration)
	}
	known := map[string]bool{}
	for _, segment := range draft.Segments {
		known[segment.ID] = true
	}
	if err := validateAIDraft(&draft, in, known); err != nil || draft.Duration != 60 {
		t.Fatalf("manual validation changed duration: %+v, %v", draft, err)
	}
}

func TestAIEvidenceSpeechRetainsOnlyAvailableSpeech(t *testing.T) {
	t.Parallel()
	in := models.EditorAIInput{TargetDuration: 60, RemoveSilence: true, Sources: []models.EditorAISource{{ID: "source", Duration: 135.5}}}
	rows := []models.EditorAITranscript{{AssetID: "source", HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 30, End: 39, Text: "speech"}}}}
	windows := buildAIEvidence(in, rows, nil)
	duration := 0.0
	for _, window := range windows {
		if window.Start < 30 || window.End > 39 {
			t.Fatalf("invented speech window %+v", window)
		}
		duration += window.End - window.Start
	}
	if duration != 9 {
		t.Fatalf("got %v, want actual speech 9", duration)
	}
}

func TestAIPlanningEvidencePromptBudget(t *testing.T) {
	t.Parallel()
	for _, target := range []float64{1, 60} {
		in := models.EditorAIInput{TargetDuration: target}
		visuals := []models.EditorAIVisual{}
		for i := 0; i < 8; i++ {
			id := fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
			in.Sources = append(in.Sources, models.EditorAISource{ID: id, Duration: 75})
			visuals = append(visuals, models.EditorAIVisual{AssetID: id, Summary: strings.Repeat("ก", 2000), Frames: []models.EditorAIVisualFrame{{Time: 12.5, Description: strings.Repeat("ก", 1000)}, {Time: 37.5, Description: strings.Repeat("ก", 1000)}, {Time: 62.5, Description: strings.Repeat("ก", 1000)}}})
		}
		evidence := buildAIEvidence(in, nil, visuals)
		if len(evidence) > 108 || evidence[len(evidence)-1].End < 75 {
			t.Fatalf("unbounded or missing late-source choices: %d windows", len(evidence))
		}
		// Planning needs the source summary, not a second copy of all frame descriptions.
		for i := range visuals {
			visuals[i].Frames = nil
		}
		prompt := aiPlanningPrompt{Task: "planning", Brief: strings.Repeat("ก", 4000), Concept: models.EditorAIConcept{Title: strings.Repeat("ก", 200), Summary: strings.Repeat("ก", 2000), Duration: target}, Target: target, Evidence: evidence, Visuals: visuals, Instruction: strings.Repeat("x", 2000)}
		raw, err := json.Marshal(prompt)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 96<<10 {
			t.Fatalf("target %v prompt %d exceeds budget", target, len(raw))
		}
		if evidence[0].End != target {
			t.Fatalf("short target unavailable: %+v", evidence[0])
		}
	}
}

func TestAIGeneratedDurationAndManualIntent(t *testing.T) {
	t.Parallel()
	for _, seconds := range []float64{15, 56.99, 57, 59.5, 60, 60.06} {
		in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 120}}}
		d := models.EditorAIDraft{Segments: []models.EditorAIDraftSegment{{ID: "00000000-0000-0000-0000-000000000001", AssetID: "source", Start: 0, End: seconds, Volume: 1, Title: "clip"}}}
		err := validateAIDraft(&d, in, nil)
		if (err == nil) != (seconds <= 60.05) {
			t.Fatalf("duration %v: %v", seconds, err)
		}
		if err := validateAIDraft(&d, in, map[string]bool{d.Segments[0].ID: true}); err != nil {
			t.Fatalf("manual %v: %v", seconds, err)
		}
	}
}

func TestAICapacityAllowsShortSpeechAndSources(t *testing.T) {
	t.Parallel()
	for _, speech := range []bool{false, true} {
		in := models.EditorAIInput{TargetDuration: 60, RemoveSilence: speech, Sources: []models.EditorAISource{{ID: "source", Duration: 15}}}
		rows := []models.EditorAITranscript{{AssetID: "source", HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 0, End: 9, Text: "speech"}}}}
		evidence := buildAIEvidence(in, rows, nil)
		err := validateAICapacity(evidence, in)
		if err != nil {
			t.Fatalf("speech=%v: %v", speech, err)
		}
	}
	in := models.EditorAIInput{TargetDuration: 59, Sources: []models.EditorAISource{{ID: "source", Duration: 120}}}
	if err := validateAICapacity(buildAIEvidence(in, nil, nil), in); err != nil {
		t.Fatal("six second grid must not fail preflight", err)
	}
}

func TestAICapacityRejectsUntrustedWindows(t *testing.T) {
	t.Parallel()
	id := "00000000-0000-0000-0000-000000000001"
	in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 60}}}
	valid := models.EditorAIEvidence{ID: id, AssetID: "source", Start: 0, End: 60}
	for _, windows := range [][]models.EditorAIEvidence{{valid, valid}, {{ID: id, AssetID: "source", Start: 0, End: 61}}, {{ID: id, AssetID: "unknown", Start: 0, End: 60}}, {{ID: id, AssetID: "source", Start: 3, End: 2}}} {
		if err := validateAICapacity(windows, in); err == nil {
			t.Fatalf("invalid evidence accepted: %+v", windows)
		}
	}
}

func TestAITargetAwareEvidenceCanReachNonGridTargets(t *testing.T) {
	t.Parallel()
	for _, target := range []float64{6, 7, 13, 15, 59, 60, 119, 120} {
		in := models.EditorAIInput{TargetDuration: target, Sources: []models.EditorAISource{{ID: "source", Duration: 135.5}}}
		windows := buildAIEvidence(in, nil, nil)
		draft := models.EditorAIDraft{}
		for _, w := range windows {
			if draft.Duration >= target-1e-8 {
				break
			}
			draft.Segments = append(draft.Segments, models.EditorAIDraftSegment{ID: w.ID, AssetID: w.AssetID, Start: w.Start, End: w.End, Volume: 1, Title: "clip"})
			draft.Duration += w.End - w.Start
		}
		if len(draft.Segments) > 20 {
			t.Fatalf("target %v too many windows", target)
		}
		if err := validateAIDraft(&draft, in, nil); err != nil {
			t.Fatalf("target %v: %v", target, err)
		}
		if mathAbs(draft.Duration-target) > 1e-7 {
			t.Fatalf("target %v got %v", target, draft.Duration)
		}
	}
}

func TestAICapacityCountsOverlappingFootageOnce(t *testing.T) {
	t.Parallel()
	in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 60}}}
	evidence := []models.EditorAIEvidence{}
	for i := 0; i < 4; i++ {
		evidence = append(evidence, models.EditorAIEvidence{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i), AssetID: "source", Start: 0, End: 15})
	}
	err := validateAICapacity(evidence, in)
	if err != nil {
		t.Fatalf("valid overlapping evidence: %v", err)
	}
}

func TestAIGeneratedBelowTargetAccepted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ actual, target float64 }{{25.12, 30}, {15, 60}, {9, 60}} {
		in := models.EditorAIInput{TargetDuration: tc.target, Sources: []models.EditorAISource{{ID: "source", Duration: 120}}}
		d := models.EditorAIDraft{Revision: 1, Segments: []models.EditorAIDraftSegment{{ID: "00000000-0000-0000-0000-000000000001", AssetID: "source", End: tc.actual, Volume: 1, Title: "clip"}}}
		if err := validateAIDraft(&d, in, nil); err != nil || d.Duration != tc.actual {
			t.Fatalf("%+v: duration=%v err=%v", tc, d.Duration, err)
		}
	}
}

func TestAIHistoricalShortFailureRetryRespectsBudget(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"AI_INSUFFICIENT_EVIDENCE", "AI_DURATION_MISMATCH"} {
		j := models.VideoEditorAIJob{Status: "failed", Phase: "planning", FailureCode: code}
		if !aiDTO(&j).CanRetry {
			t.Fatalf("historical %s cannot retry", code)
		}
		j.CloudCallsUsed = 6
		if aiDTO(&j).CanRetry {
			t.Fatal("budget bypass")
		}
		j.CloudCallsUsed = 0
		j.PlanningAttempts = 3
		if aiDTO(&j).CanRetry {
			t.Fatal("attempt limit bypass")
		}
	}
}

func TestAIShortDraftRetainsValidation(t *testing.T) {
	t.Parallel()
	in := models.EditorAIInput{TargetDuration: 30, Sources: []models.EditorAISource{{ID: "source", Duration: 60}}}
	valid := models.EditorAIDraftSegment{ID: "00000000-0000-0000-0000-000000000001", AssetID: "source", End: 25.12, Volume: 1, Title: "clip"}
	cases := [][]models.EditorAIDraftSegment{nil, {valid, valid}}
	for _, change := range []func(*models.EditorAIDraftSegment){func(s *models.EditorAIDraftSegment) { s.ID = "invalid" }, func(s *models.EditorAIDraftSegment) { s.AssetID = "unknown" }, func(s *models.EditorAIDraftSegment) { s.End = 61 }, func(s *models.EditorAIDraftSegment) { s.End = 0 }, func(s *models.EditorAIDraftSegment) { s.End = 30.06 }} {
		s := valid
		change(&s)
		cases = append(cases, []models.EditorAIDraftSegment{s})
	}
	for _, segments := range cases {
		if err := validateAIDraft(&models.EditorAIDraft{Segments: segments}, in, nil); err == nil {
			t.Fatalf("invalid accepted: %+v", segments)
		}
	}
	if err := validateAICapacity(nil, in); err == nil {
		t.Fatal("empty evidence accepted")
	}
}
