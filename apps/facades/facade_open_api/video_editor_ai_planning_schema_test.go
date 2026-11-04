package facade_open_api

import (
	"encoding/json"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

func TestAIPlanningSchemaConstrainsCoarseThirtySecondSource(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 30, RemoveSilence: true, Sources: []models.EditorAISource{{ID: "source", Duration: 313.8873125}}}
	transcript := models.EditorAITranscript{AssetID: "source", HasSpeech: true}
	bounds := [][2]float64{{0, 28.7}, {28.7, 45.18}, {45.5, 75}, {75, 102.1}, {102.1, 128.5}, {128.5, 156.2}, {156.2, 186.15}, {186.15, 212.5}, {212.5, 238.9}, {238.9, 264.55}, {264.55, 293.35}, {293.35, 313.8873125}}
	for _, b := range bounds {
		transcript.Segments = append(transcript.Segments, models.EditorAISpeech{Start: b[0], End: b[1], Text: "บริบทคำพูดต้นฉบับ"})
	}
	evidence := buildAIEvidence(in, []models.EditorAITranscript{transcript}, nil)
	// Any pair of these real context windows exceeds the requested target.
	for i, a := range evidence {
		for _, b := range evidence[:i] {
			if a.End-a.Start+b.End-b.Start <= 30.05 {
				t.Fatal("fixture admits two windows")
			}
		}
	}
	draft := models.EditorAIDraft{}
	for _, window := range evidence[:2] {
		draft.Segments = append(draft.Segments, models.EditorAIDraftSegment{ID: uuid.NewString(), AssetID: window.AssetID, Start: window.Start, End: window.End, Volume: 1, Title: "บริบทต้นฉบับ"})
	}
	if validateAIDraft(&draft, in, nil) == nil {
		t.Fatal("impossible two-context plan accepted")
	}
	draft.Segments = draft.Segments[:1]
	if err := validateAIDraft(&draft, in, nil); err != nil {
		t.Fatalf("single coarse context cannot reach review: %v", err)
	}
	schema := aiPlanSchema(evidence, in.TargetDuration)
	var decoded struct {
		Properties struct {
			Segments struct {
				MaxItems int `json:"maxItems"`
				Items    struct {
					Properties struct {
						EvidenceID struct {
							Enum []string `json:"enum"`
						} `json:"evidenceId"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"segments"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Properties.Segments.MaxItems != 1 {
		t.Fatalf("provider allowed impossible multi-context plan: %s", schema)
	}
	ids := decoded.Properties.Segments.Items.Properties.EvidenceID.Enum
	if len(ids) != len(evidence) {
		t.Fatalf("provider schema permits unknown IDs: %s", schema)
	}
	for i, id := range ids {
		if id != evidence[i].ID {
			t.Fatal("schema changed server ID")
		}
	}
}

func TestAIPlanningSchemaCountDoesNotExcludeFeasibleMixedDurations(t *testing.T) {
	for _, tc := range []struct {
		lengths []float64
		target  float64
		want    int
	}{
		{[]float64{29, 1, 10, 3}, 30, 3},
		{[]float64{15, 15}, 30, 2},
		{[]float64{15, 15.04}, 30, 2},
		{[]float64{.2, .3, .4}, 1, 3},
	} {
		evidence := []models.EditorAIEvidence{}
		for _, length := range tc.lengths {
			evidence = append(evidence, models.EditorAIEvidence{ID: uuid.NewString(), End: length})
		}
		if got := aiPlanMaxSegments(evidence, tc.target); got != tc.want {
			t.Fatalf("%v: got %d want %d", tc, got, tc.want)
		}
	}
}

func TestAIShortDecodeContextsPermitMultipleHighlights(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 30, RemoveSilence: true, Sources: []models.EditorAISource{{ID: "source", Duration: 65}}}
	transcript := models.EditorAITranscript{AssetID: "source", HasSpeech: true}
	for _, bounds := range [][2]float64{{0, 9.1}, {9.1, 19.1}, {19.1, 29.1}, {29.1, 39.1}, {39.1, 49.1}, {49.1, 59.1}, {59.1, 65}} {
		transcript.Segments = append(transcript.Segments, models.EditorAISpeech{Start: bounds[0], End: bounds[1], Text: "บริบทที่ถอดจากเสียงช่วงนี้โดยตรง", Words: []models.EditorAIWord{}})
	}
	evidence := buildAIEvidence(in, []models.EditorAITranscript{transcript}, nil)
	if aiPlanMaxSegments(evidence, in.TargetDuration) < 3 {
		t.Fatal("short contexts still force one long span")
	}
	draft := models.EditorAIDraft{}
	for _, index := range []int{0, 2, 4} {
		context := transcript.Segments[index]
		found := false
		for _, window := range evidence {
			if window.Start == context.Start && window.End == context.End {
				draft.Segments = append(draft.Segments, models.EditorAIDraftSegment{ID: uuid.NewString(), AssetID: window.AssetID, Start: window.Start, End: window.End, Volume: 1, Title: "ไฮไลต์", EvidenceID: window.ID})
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing independent source context %d", index)
		}
	}
	if aiSelectedOverlap(draft.Segments) {
		t.Fatal("highlights reuse footage")
	}
	if err := validateAIDraft(&draft, in, nil); err != nil {
		t.Fatal(err)
	}
	if len(draft.Segments) != 3 || draft.Duration > 30.05 {
		t.Fatalf("invalid highlight draft: %+v", draft)
	}
}

func TestAIHighlightChoicesRetainMoreShortContextsAcrossLongSource(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 30, Sources: []models.EditorAISource{{ID: "source", Duration: 300}}}
	transcript := models.EditorAITranscript{AssetID: "source", HasSpeech: true}
	for i := 0; i < 30; i++ {
		transcript.Segments = append(transcript.Segments, models.EditorAISpeech{Start: float64(i * 10), End: float64((i + 1) * 10), Text: "ช่วงคำพูดต้นฉบับ"})
	}
	evidence := buildAIEvidence(in, []models.EditorAITranscript{transcript}, nil)
	singles, tail := 0, false
	for _, window := range evidence {
		if window.End-window.Start == 10 {
			singles++
			tail = tail || window.End == 300
		}
	}
	if singles < 12 || !tail || len(evidence) > 24 {
		t.Fatalf("lost representative short contexts: singles=%d tail=%v count=%d", singles, tail, len(evidence))
	}
}
