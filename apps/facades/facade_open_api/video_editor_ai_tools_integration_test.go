package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
)

// aiToolsProvider adds editing tools to the shared fake planner and answers timeline edits.
type aiToolsProvider struct {
	*aiTestProvider
	mu         sync.Mutex
	editOut    func(prompt aiEditPrompt) any
	prompts    []aiEditPrompt
	planTokens int
}

func (p *aiToolsProvider) Generate(ctx context.Context, system, prompt string, images []editorai.Image, schema json.RawMessage) ([]byte, error) {
	var task struct {
		Task string `json:"task"`
	}
	_ = json.Unmarshal([]byte(prompt), &task)
	switch task.Task {
	case "timeline_edit":
		var in aiEditPrompt
		if err := json.Unmarshal([]byte(prompt), &in); err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.prompts = append(p.prompts, in)
		out := p.editOut
		p.mu.Unlock()
		return aiJSON(out(in)), nil
	case "planning":
		p.mu.Lock()
		p.planTokens = editorai.MaxOutputTokens(ctx)
		p.mu.Unlock()
		raw, err := p.aiTestProvider.Generate(ctx, system, prompt, images, schema)
		if err != nil {
			return nil, err
		}
		var plan map[string]any
		if err = json.Unmarshal(raw, &plan); err != nil {
			return nil, err
		}
		first := plan["segments"].([]any)[0].(map[string]any)
		// speed/fade/brightness are valid; a first-segment transition and zoom 9 are not.
		for k, v := range map[string]any{"speed": 2, "fadeIn": 0.1, "brightness": 0.2, "transition": "fade", "transitionDuration": 0.2, "zoom": 9} {
			first[k] = v
		}
		plan["captionStyle"] = map[string]any{"color": "#FFCC00", "outlineColor": "#000000", "outlineWidth": 2, "animation": "spin"}
		return aiJSON(plan), nil
	}
	return p.aiTestProvider.Generate(ctx, system, prompt, images, schema)
}

func TestEditorAIDraftToolsApplyAndInstructionPreview(t *testing.T) {
	ctx := context.Background()
	provider := &aiToolsProvider{aiTestProvider: &aiTestProvider{}}
	db, editor, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider.aiTestProvider)
	f.provider = provider
	input := aiRequest(asset)
	input.Brief = "ทำคลิปกาแฟ"
	j, _, err := f.Create(ctx, project, user, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if j, err = f.Get(ctx, j.ID, user); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Select(ctx, j.ID, user, j.Concepts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if j, err = f.Get(ctx, j.ID, user); err != nil || j.Status != "review" {
		t.Fatal(j, err)
	}
	if provider.planTokens != aiStructuredOutputTokens {
		t.Fatalf("planning maxOutputTokens %d", provider.planTokens)
	}
	first := j.Draft.Segments[0]
	if first.Speed != 2 || first.FadeIn != .1 || first.Brightness != .2 || first.Transition != "" || first.TransitionDuration != 0 || first.Zoom != 0 {
		t.Fatalf("generated effects not validated: %+v", first)
	}
	if math.Abs(j.Draft.Duration-aiDraftDuration(j.Draft.Segments)) > 1e-9 || j.Draft.Duration > input.TargetDuration+.05 {
		t.Fatalf("draft duration must be timeline length: %v", j.Draft.Duration)
	}
	if j.Draft.TextStyles == nil || j.Draft.TextStyles.Caption == nil || j.Draft.TextStyles.Caption.Color != "#FFCC00" || j.Draft.TextStyles.Caption.OutlineWidth != 2 || j.Draft.TextStyles.Caption.Animation != "" {
		t.Fatalf("caption style %+v", j.Draft.TextStyles)
	}

	edited := append([]models.EditorAIDraftSegment{}, j.Draft.Segments...)
	originalEvidence := edited[0].EvidenceID
	edited[0].Speed = .5
	edited[0].Contrast = 5
	edited[0].EvidenceID = "forged-evidence"
	j, err = f.UpdateDraft(ctx, j.ID, user, req.UpdateVideoEditorAIDraft{DraftRevision: 1, Segments: edited})
	if err != nil {
		t.Fatal(err)
	}
	if j.Draft.Segments[0].EvidenceID != originalEvidence {
		t.Fatalf("server-owned evidenceId overwritten by client: %q", j.Draft.Segments[0].EvidenceID)
	}
	if j.Draft.Segments[0].Speed != .5 || j.Draft.Segments[0].Contrast != 0 || j.Draft.Segments[0].Brightness != .2 {
		t.Fatalf("draft PUT effects %+v", j.Draft.Segments[0])
	}
	apply := req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 1, DraftRevision: j.Draft.Revision}
	for _, s := range j.Draft.Segments {
		apply.ApprovedSegmentIDs = append(apply.ApprovedSegmentIDs, s.ID)
	}
	p, err := f.Apply(ctx, j.ID, user, apply)
	if err != nil {
		t.Fatal(err)
	}
	if p.Segments[0].Speed != .5 || p.Segments[0].Brightness != .2 || p.Segments[0].FadeIn != .1 {
		t.Fatalf("apply dropped effects %+v", p.Segments[0])
	}

	segmentID := p.Segments[0].ID
	provider.editOut = func(in aiEditPrompt) any {
		return map[string]any{"summary": "ปรับความเร็วและเพิ่มข้อความ", "operations": []map[string]any{
			{"tool": "set_segment", "target": segmentID, "description": "เร่งความเร็ว", "params": `{"speed":1.5}`},
			{"tool": "add_text", "target": "", "description": "เพิ่มชื่อเรื่อง", "params": `{"text":"สวัสดี","y":0.2}`},
			{"tool": "set_segment", "target": uuid.NewString(), "description": "x", "params": `{"speed":2}`},
			{"tool": "explode", "target": segmentID, "description": "x", "params": "{}"},
			{"tool": "set_segment", "target": segmentID, "description": "x", "params": `{"text":"no"}`},
			{"tool": "set_segment", "target": segmentID, "description": "x", "params": `{"transition":"fade"}`},
		}}
	}
	if _, err = f.PreviewEdit(ctx, project, user, req.PreviewVideoEditorAIEdit{Instruction: "เร่งคลิปแรก", Revision: p.Revision - 1}); !errors.Is(err, repo.ErrVERevision) {
		t.Fatal("stale revision", err)
	}
	other := veTestUser(t, db)
	if _, err = f.PreviewEdit(ctx, project, other, req.PreviewVideoEditorAIEdit{Instruction: "เร่งคลิปแรก", Revision: p.Revision}); !errors.Is(err, repo.ErrNotFound) {
		t.Fatal("other user", err)
	}
	preview, err := f.PreviewEdit(ctx, project, user, req.PreviewVideoEditorAIEdit{Instruction: "  เร่งคลิปแรก  ", Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if preview.BaseRevision != p.Revision || len(preview.Operations) != 2 || preview.Operations[0].Target != segmentID || preview.Operations[1].Tool != "add_text" || !strings.Contains(preview.Summary, "4") {
		t.Fatalf("preview %+v", preview)
	}
	if provider.prompts[len(provider.prompts)-1].Instruction != "เร่งคลิปแรก" {
		t.Fatal("instruction not trimmed")
	}
	if preview.Proposal.Segments[0].Speed != 1.5 || len(preview.Proposal.TextLayers) != len(p.TextLayers)+1 {
		t.Fatalf("proposal %+v", preview.Proposal)
	}
	stored, err := editor.Project(ctx, project, user)
	if err != nil || stored.Revision != p.Revision || stored.Segments[0].Speed != .5 {
		t.Fatal("preview must not save", stored.Revision, err)
	}
	body := req.SaveVideoEditorProject{Revision: preview.BaseRevision, Title: preview.Proposal.Title, AspectRatio: preview.Proposal.AspectRatio, Segments: preview.Proposal.Segments, AudioClips: &preview.Proposal.AudioClips, TextLayers: &preview.Proposal.TextLayers}
	saved, err := editor.SaveProject(ctx, project, user, body)
	if err != nil || saved.Segments[0].Speed != 1.5 {
		t.Fatal("proposal must be a valid PUT body", err)
	}

	provider.editOut = func(aiEditPrompt) any {
		return map[string]any{"summary": "ทำไม่ได้", "operations": []map[string]any{{"tool": "delete_text", "target": uuid.NewString(), "description": "x", "params": "{}"}}}
	}
	if _, err = f.PreviewEdit(ctx, project, user, req.PreviewVideoEditorAIEdit{Instruction: "ลบข้อความที่ไม่มี", Revision: saved.Revision}); !errors.Is(err, ErrAIEditNoChanges) {
		t.Fatal("no changes", err)
	}
}
