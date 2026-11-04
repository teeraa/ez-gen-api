package facade_open_api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
)

func refinementTestTranscript() models.EditorAITranscript {
	return models.EditorAITranscript{AssetID: "source", Language: "th", Text: "กา แฟ ดี ไม่หวาน", HasSpeech: true, Segments: []models.EditorAISpeech{
		{Start: 0, End: 9, Text: "กา แฟ ดี", Words: []models.EditorAIWord{{Start: 0, End: 2, Text: "กา"}, {Start: 2, End: 9, Text: "แฟ ดี"}}},
		{Start: 10, End: 12, Text: "ไม่หวาน", Words: []models.EditorAIWord{{Start: 10, End: 12, Text: "ไม่หวาน"}}},
	}}
}
func TestAIRefinementPreservesRawTimingAndUncertainSpeech(t *testing.T) {
	raw := refinementTestTranscript()
	original, _ := json.Marshal(raw)
	refined, err := normalizeAIRefinement(raw, []models.EditorAIRefinedSegment{{SegmentIndex: 1, Text: "หวานมาก", Uncertain: true}, {SegmentIndex: 0, Text: "กาแฟดี"}})
	if err != nil {
		t.Fatal(err)
	}
	if refined.Text != "กาแฟดี\nไม่หวาน" || refined.Segments[0].SegmentIndex != 0 || !refined.Segments[1].Uncertain {
		t.Fatalf("unsafe interpretation: %+v", refined)
	}
	after, _ := json.Marshal(raw)
	if string(after) != string(original) {
		t.Fatal("normalizing altered raw ASR")
	}
	raw.Refinement = refined
	compact := compactAITranscripts([]models.EditorAITranscript{raw})
	if compact[0].Text != refined.Text {
		t.Fatal("concept prompt uses raw ASR")
	}
	in := models.EditorAIInput{TargetDuration: 12, RemoveSilence: true, Sources: []models.EditorAISource{{ID: "source", Duration: 12}}}
	windows := buildAIEvidence(in, []models.EditorAITranscript{raw}, nil)
	if len(windows) != 2 || !strings.Contains(windows[0].Description, "กาแฟดี") || !strings.Contains(windows[1].Description, "uncertain ASR") || strings.Contains(windows[1].Description, "หวานมาก") {
		t.Fatalf("unsafe planning context: %+v", windows)
	}
	raw.Refinement = nil
	rawWindows := buildAIEvidence(in, []models.EditorAITranscript{raw}, nil)
	for i := range windows {
		if windows[i].Start != rawWindows[i].Start || windows[i].End != rawWindows[i].End {
			t.Fatal("refinement moved evidence times")
		}
	}
	if compactAITranscripts([]models.EditorAITranscript{raw})[0].Text != raw.Text {
		t.Fatal("legacy raw fallback lost")
	}
}
func TestAIRefinementRejectsInvalidMappings(t *testing.T) {
	raw := refinementTestTranscript()
	cases := [][]models.EditorAIRefinedSegment{
		nil,
		{{SegmentIndex: 0, Text: "one"}},
		{{SegmentIndex: 0, Text: "one"}, {SegmentIndex: 0, Text: "duplicate"}},
		{{SegmentIndex: -1, Text: "one"}, {SegmentIndex: 1, Text: "two"}},
		{{SegmentIndex: 0, Text: "one"}, {SegmentIndex: 2, Text: "outside"}},
		{{SegmentIndex: 0, Text: " "}, {SegmentIndex: 1, Text: "two"}},
		{{SegmentIndex: 0, Text: strings.Repeat("x", 10001)}, {SegmentIndex: 1, Text: "two"}},
	}
	for i, segments := range cases {
		if _, err := normalizeAIRefinement(raw, segments); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	raw.HasSpeech = false
	if _, err := normalizeAIRefinement(raw, []models.EditorAIRefinedSegment{}); err == nil {
		t.Fatal("silent source refined into speech")
	}
}
func TestAIRefinementCacheSeparatesProviderAndASRGenerations(t *testing.T) {
	a := models.EditorAIInput{ASRSignature: "asr1", ModelSignature: "model1"}
	b := a
	b.ASRSignature = "asr2"
	c := a
	c.ModelSignature = "model2"
	if aiRefinementSignature(a) == aiRefinementSignature(b) || aiRefinementSignature(a) == aiRefinementSignature(c) || aiRefinementSignature(a) == a.ASRSignature {
		t.Fatal("raw/refined or generations share namespace")
	}
	a.Brief = "marketing instruction"
	if aiRefinementSignature(a) != aiRefinementSignature(models.EditorAIInput{ASRSignature: "asr1", ModelSignature: "model1"}) {
		t.Fatal("source-only cache depends on brief")
	}
}
func TestAIRefinementOptionalJSONPreservesLegacyShape(t *testing.T) {
	raw := refinementTestTranscript()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "refinement") {
		t.Fatal("legacy field not omitted")
	}
	var copy models.EditorAITranscript
	if err = json.Unmarshal(data, &copy); err != nil || !reflect.DeepEqual(raw, copy) {
		t.Fatalf("legacy roundtrip: %v", err)
	}
}

func TestAIRefinementResponseRequiresExplicitMappingAndUncertainty(t *testing.T) {
	for _, raw := range []string{
		`{"assetId":"source","segments":[{"text":"changed","uncertain":false}]}`,
		`{"assetId":"source","segments":[{"segmentIndex":0,"text":"changed"}]}`,
		`{"assetId":"source","segments":[{"segmentIndex":0,"text":"changed","uncertain":null}]}`,
		`{"assetId":"source","segments":[{"segmentIndex":0,"text":"changed","uncertain":false,"start":1}]}`,
	} {
		var result aiRefinementResult
		if aiDecode([]byte(raw), &result) == nil {
			t.Fatalf("accepted incomplete or invented mapping: %s", raw)
		}
	}
}
