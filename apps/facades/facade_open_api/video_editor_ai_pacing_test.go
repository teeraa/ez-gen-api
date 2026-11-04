package facade_open_api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

func TestAIPacingOffersSpeechUnitsAndVariableRuns(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 60, Sources: []models.EditorAISource{{ID: "source", Duration: 70}}}
	transcript := models.EditorAITranscript{AssetID: "source", HasSpeech: true}
	for i, bounds := range [][2]float64{{0, 9}, {9.4, 22}, {22.4, 35}, {35.4, 48}, {48.4, 62}, {62.4, 70}} {
		transcript.Segments = append(transcript.Segments, models.EditorAISpeech{Start: bounds[0], End: bounds[1], Text: fmt.Sprintf("เรื่องส่วนที่ %d", i)})
	}
	evidence := buildAIEvidence(in, []models.EditorAITranscript{transcript}, nil)
	foundSingle, foundPair, foundMiddle, foundLong := false, false, false, false
	for _, e := range evidence {
		foundSingle = foundSingle || e.Start == 9.4 && e.End == 22
		foundPair = foundPair || e.Start == 0 && e.End == 22
		foundMiddle = foundMiddle || e.Start == 0 && e.End == 35
		foundLong = foundLong || e.Start == 0 && e.End == 48
	}
	if !foundSingle || !foundPair || !foundMiddle || !foundLong {
		t.Fatalf("missing pacing opportunities: %+v", evidence)
	}
	duration, reason := aiConceptDuration(30, "", in, evidence)
	if duration != 60 || reason != "" {
		t.Fatalf("short model default overrode feasible target: %v %q", duration, reason)
	}
}

func TestAIPacingRetainsAdjacentNarrativeRoles(t *testing.T) {
	segments := []models.EditorAIDraftSegment{{AssetID: "s", Start: 0, End: 12, Title: "เปิดเรื่อง", Reason: "ให้บริบท"}, {AssetID: "s", Start: 12, End: 30, Title: "คำตอบ", Reason: "ขยายประเด็น"}}
	if len(aiMergeAdjacent(segments)) != 2 {
		t.Fatal("different story roles collapsed into one long shot")
	}
	segments[1].Title, segments[1].Reason = segments[0].Title, segments[0].Reason
	if len(aiMergeAdjacent(segments)) != 1 {
		t.Fatal("identical continuity metadata no longer coalesces")
	}
}

func TestAIPacingSpeechPromptBudget(t *testing.T) {
	in := models.EditorAIInput{TargetDuration: 60}
	transcripts := []models.EditorAITranscript{}
	visuals := []models.EditorAIVisual{}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
		in.Sources = append(in.Sources, models.EditorAISource{ID: id, Duration: 75})
		row := models.EditorAITranscript{AssetID: id, HasSpeech: true}
		for j := 0; j < 15; j++ {
			row.Segments = append(row.Segments, models.EditorAISpeech{Start: float64(j * 5), End: float64((j + 1) * 5), Text: strings.Repeat("ก", 200)})
		}
		transcripts = append(transcripts, row)
		visuals = append(visuals, models.EditorAIVisual{AssetID: id, Summary: strings.Repeat("ก", 2000)})
	}
	evidence := buildAIEvidence(in, transcripts, nil)
	prompt := aiPlanningPrompt{Task: "planning", Brief: strings.Repeat("ก", 4000), Concept: models.EditorAIConcept{Title: strings.Repeat("ก", 200), Summary: strings.Repeat("ก", 2000)}, Target: 60, Evidence: evidence, Visuals: visuals, Instruction: strings.Repeat("x", 5000)}
	raw, err := json.Marshal(prompt)
	if err != nil || len(raw) > 96<<10 {
		t.Fatalf("pacing evidence exceeds prompt budget: %d, %v", len(raw), err)
	}
	if len(evidence) > 64 {
		t.Fatalf("unbounded speech choices: %d", len(evidence))
	}
}

type aiVersionedTestASR struct {
	*aiTestASR
	generation string
}

func (a *aiVersionedTestASR) CacheSignature() string { return a.Signature() + ":" + a.generation }

func TestAIPacingCacheIdentityPreservesStoredProviderSignature(t *testing.T) {
	asr := &aiVersionedTestASR{aiTestASR: &aiTestASR{}, generation: "pause-segments-v1"}
	f := &VideoEditorAIFacade{asr: asr}
	in := models.EditorAIInput{ASRSignature: asr.Signature(), ModelSignature: "model"}
	generation := f.asrCacheSignature(in)
	if generation == in.ASRSignature || in.ASRSignature != asr.Signature() {
		t.Fatal("cache upgrade changed compatibility signature")
	}
	cacheInput := in
	cacheInput.ASRSignature = generation
	if aiRefinementSignature(cacheInput) == aiRefinementSignature(in) {
		t.Fatal("old coarse refinement reused for pause segments")
	}
	in.ASRSignature = "historic-other-provider"
	if f.asrCacheSignature(in) != in.ASRSignature {
		t.Fatal("rewrote unrelated historic identity")
	}
}

func TestEditorAIPacingKeepsDistinctBeatsAndApplies(t *testing.T) {
	ctx := context.Background()
	provider := &aiTestProvider{planAll: true, distinctPlanRoles: true}
	db, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider)
	job, _, err := f.Create(ctx, project, user, aiRequest(asset))
	if err != nil {
		t.Fatal(err)
	}
	var stored models.VideoEditorAIJob
	if err = db.First(&stored, "id=?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := aiInput(&stored)
	snapshot.TargetDuration = 1.8
	concept := models.EditorAIConcept{ID: uuid.NewString(), Title: "สามบทบาท", Summary: "เปิด ขยาย จบ", Duration: 1.8}
	evidence := []models.EditorAIEvidence{{ID: uuid.NewString(), AssetID: asset, Start: 0, End: .6}, {ID: uuid.NewString(), AssetID: asset, Start: .6, End: 1.2}, {ID: uuid.NewString(), AssetID: asset, Start: 1.2, End: 1.8}}
	if err = db.Model(&stored).Updates(map[string]any{"input": aiJSON(snapshot), "phase": "planning", "concepts": aiJSON([]models.EditorAIConcept{concept}), "selected_concept_id": concept.ID, "evidence": aiJSON(evidence), "visuals": aiJSON([]models.EditorAIVisual{}), "transcripts": aiJSON([]models.EditorAITranscript{})}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(ctx, job.ID, user)
	if err != nil || got.Status != "review" || got.Draft == nil || len(got.Draft.Segments) != 3 || mathAbs(got.Draft.Duration-1.8) > .000001 {
		t.Fatalf("beats collapsed: %+v %v", got, err)
	}
	approved := []string{}
	for _, segment := range got.Draft.Segments {
		approved = append(approved, segment.ID)
	}
	if _, err = f.Apply(ctx, job.ID, user, req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 1, DraftRevision: got.Draft.Revision, ApprovedSegmentIDs: approved}); err != nil {
		t.Fatal(err)
	}
}

func TestEditorAIPacingCacheUpgradeDoesNotReuseCoarseASR(t *testing.T) {
	ctx := context.Background()
	asr := &aiTestASR{}
	db, _, f, user, project, asset := aiSetup(t, asr, &aiTestProvider{})
	analyze := func() models.VideoEditorAIJob {
		input := aiRequest(asset)
		job, _, err := f.Create(ctx, project, user, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.ProcessNext(ctx); err != nil {
			t.Fatal(err)
		}
		var stored models.VideoEditorAIJob
		if err = db.First(&stored, "id=?", job.ID).Error; err != nil {
			t.Fatal(err)
		}
		return stored
	}
	first := analyze()
	if first.Status != "awaiting_concept" || asr.calls.Load() != 1 {
		t.Fatalf("legacy analysis: %+v", first)
	}
	f.asr = &aiVersionedTestASR{aiTestASR: asr, generation: "pause-segments-v1"}
	second := analyze()
	if second.Status != "awaiting_concept" || asr.calls.Load() != 2 || second.CloudCallsUsed != 2 {
		t.Fatalf("coarse raw/refinement reused: calls=%d cloud=%d status=%s", asr.calls.Load(), second.CloudCallsUsed, second.Status)
	}
	if aiInput(&second).ASRSignature != asr.Signature() {
		t.Fatal("cache upgrade mutated stored compatibility identity")
	}
	third := analyze()
	if third.Status != "awaiting_concept" || asr.calls.Load() != 2 || third.CloudCallsUsed != 1 {
		t.Fatalf("pause cache not reused: calls=%d cloud=%d", asr.calls.Load(), third.CloudCallsUsed)
	}
}
