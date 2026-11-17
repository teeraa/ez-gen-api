package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
)

// chatTestCaller replays scripted replies and records every request.
type chatTestCaller struct {
	mu       sync.Mutex
	replies  []editorai.ChatReply
	requests []editorai.ChatRequest
	repeat   *editorai.ChatReply
}

func (c *chatTestCaller) Available() bool { return true }
func (c *chatTestCaller) Name() string    { return "fake" }
func (c *chatTestCaller) Model() string   { return "fake-1" }
func (c *chatTestCaller) Chat(_ context.Context, in editorai.ChatRequest) (editorai.ChatReply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, in)
	if c.repeat != nil {
		return *c.repeat, nil
	}
	if len(c.replies) == 0 {
		return editorai.ChatReply{}, editorai.ErrInvalid
	}
	r := c.replies[0]
	c.replies = c.replies[1:]
	return r, nil
}

func chatCall(id, name string, args any) editorai.ToolCall {
	raw, _ := json.Marshal(args)
	return editorai.ToolCall{ID: id, Name: name, Args: raw}
}

func chatTestSession(t *testing.T, assets []models.VideoEditorAsset, segments []models.VideoEditorSegment, analysis map[string]aiChatAnalysis) *aiChatSession {
	t.Helper()
	base := aiEditState{Segments: segments, AudioClips: []models.VideoEditorAudioClip{}, TextLayers: []models.VideoEditorTextLayer{}, ImageLayers: []models.VideoEditorImageLayer{}}
	validated, err := base.validate(assets, false)
	if err != nil {
		t.Fatal(err)
	}
	return &aiChatSession{ctx: context.Background(), f: &VideoEditorAIChatFacade{ai: &VideoEditorAIFacade{}}, project: &models.VideoEditorProject{Base: models.Base{ID: uuid.New()}, Title: "ทดสอบ", AspectRatio: "9:16", Revision: 3}, user: uuid.NewString(), assets: assets, base: validated, state: validated.clone(), loaded: true, analysis: analysis}
}

func chatTranscript(asset string) *models.EditorAITranscript {
	return &models.EditorAITranscript{AssetID: asset, Language: "th", HasSpeech: true, Segments: []models.EditorAISpeech{
		{Start: 1, End: 3, Text: "ราคาแค่ 59 บาท", Words: []models.EditorAIWord{{Start: 1, End: 2, Text: "ราคาแค่"}, {Start: 2, End: 3, Text: "59 บาท"}}},
		{Start: 6, End: 8, Text: "ร้านเปิดทุกวัน", Words: []models.EditorAIWord{{Start: 6, End: 8, Text: "ร้านเปิดทุกวัน"}}},
	}}
}

func TestAIChatLoopBuildsOneProposal(t *testing.T) {
	video := aiToolAsset("video", 10)
	id := video.ID.String()
	s := chatTestSession(t, []models.VideoEditorAsset{video}, nil, map[string]aiChatAnalysis{id: {transcript: chatTranscript(id), visual: &models.EditorAIVisual{AssetID: id, Summary: "หน้าร้านกาแฟ"}}})
	caller := &chatTestCaller{replies: []editorai.ChatReply{
		{ToolCalls: []editorai.ToolCall{chatCall("1", "get_project", map[string]any{}), chatCall("2", "find_moments", map[string]any{"query": "ราคา"})}},
		{ToolCalls: []editorai.ToolCall{chatCall("3", "set_story", map[string]any{"segments": []map[string]any{{"assetId": id, "start": 0, "end": 4}, {"assetId": id, "start": 6, "end": 9}}})}},
		{ToolCalls: []editorai.ToolCall{chatCall("4", "edit_timeline", map[string]any{"operations": []map[string]any{{"tool": "add_text", "target": "", "description": "ข้อความเปิด", "params": `{"text":"โปรวันนี้","start":0,"end":2}`}}})}},
		{Text: "เรียงเรื่องและใส่ข้อความเปิดให้แล้ว กด “นำไปใช้” ได้เลย"},
	}}
	var events []dto.VideoEditorAIChatEventDTO
	meta, final, err := runAIChatLoop(context.Background(), caller, func() error { return nil }, []editorai.ChatTurn{{Role: "user", Text: "ตัดคลิปโปรโมต"}}, s, func(e dto.VideoEditorAIChatEventDTO) { events = append(events, e) })
	if err != nil || !strings.Contains(final, "นำไปใช้") || meta.Steps != 4 {
		t.Fatalf("final=%q steps=%d err=%v", final, meta.Steps, err)
	}
	if len(meta.Tools) != 4 || len(events) != 4 {
		t.Fatalf("tools %+v events %d", meta.Tools, len(events))
	}
	for _, tool := range meta.Tools {
		if !tool.OK {
			t.Fatalf("tool failed %+v", tool)
		}
	}
	// The second request carries both tool results, and find_moments found the price line.
	results := caller.requests[1].Turns[len(caller.requests[1].Turns)-1].ToolResults
	if len(results) != 2 || results[1].Name != "find_moments" || !strings.Contains(string(results[1].Content), "59") {
		t.Fatalf("results %+v", results)
	}
	p := s.proposal(final)
	if p == nil || p.Status != "pending" || p.BaseRevision != 3 || len(p.Proposal.Segments) != 2 || len(p.Proposal.TextLayers) != 1 || len(p.Operations) != 2 {
		t.Fatalf("proposal %+v", p)
	}
}

func TestAIChatToolsRejectInvalidEditsAndKeepState(t *testing.T) {
	video := aiToolAsset("video", 10)
	id := video.ID.String()
	seg := models.VideoEditorSegment{ID: uuid.NewString(), AssetID: id, Start: 0, End: 5, Volume: 1}
	s := chatTestSession(t, []models.VideoEditorAsset{video}, []models.VideoEditorSegment{seg}, map[string]aiChatAnalysis{})
	if _, _, ok := s.run(chatCall("1", "set_story", map[string]any{"segments": []map[string]any{{"assetId": id, "start": 0, "end": 30}}})); ok {
		t.Fatal("window past the asset end must be rejected")
	}
	if _, _, ok := s.run(chatCall("2", "add_segment", map[string]any{"assetId": uuid.NewString(), "start": 0, "end": 1})); ok {
		t.Fatal("unknown asset must be rejected")
	}
	if s.proposal("") != nil {
		t.Fatal("rejected edits must not create a proposal")
	}
	if _, _, ok := s.run(chatCall("3", "add_segment", map[string]any{"assetId": id, "start": 6, "end": 8, "position": 0})); !ok || s.state.Segments[0].Start != 6 {
		t.Fatalf("insert at 0: %+v", s.state.Segments)
	}
	s.jobID = uuid.NewString()
	if content, _, ok := s.run(chatCall("4", "add_segment", map[string]any{"assetId": id, "start": 1, "end": 2})); ok || !strings.Contains(string(content), "error") {
		t.Fatal("edits after start_story in the same reply are refused")
	}
	if _, _, ok := s.run(chatCall("5", "nope", map[string]any{})); ok {
		t.Fatal("unknown tool")
	}
}

func TestAIChatSubtitlesFollowTimelineAndReplace(t *testing.T) {
	video := aiToolAsset("video", 10)
	id := video.ID.String()
	segments := []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: id, Start: 5, End: 9, Volume: 1}, {ID: uuid.NewString(), AssetID: id, Start: 0, End: 4, Volume: 1, Speed: 2}}
	s := chatTestSession(t, []models.VideoEditorAsset{video}, segments, map[string]aiChatAnalysis{id: {transcript: chatTranscript(id)}})
	if _, _, ok := s.run(chatCall("1", "add_subtitles", map[string]any{})); !ok {
		t.Fatal("subtitles")
	}
	if len(s.state.TextLayers) != 2 {
		t.Fatalf("captions %+v", s.state.TextLayers)
	}
	// First cut 5-9 holds "ร้านเปิดทุกวัน" at source 6-8 → timeline 1-3; the 2× cut 0-4
	// starts at 4 and holds the price line at source 1-3 → timeline 4.5-5.5.
	first, second := s.state.TextLayers[0], s.state.TextLayers[1]
	if first.Start != 1 || first.End != 3 || second.Start != 4.5 || second.End != 5.5 || first.OriginJobID != aiChatSubtitleOrigin {
		t.Fatalf("timing %+v %+v", first, second)
	}
	if _, _, ok := s.run(chatCall("2", "add_subtitles", map[string]any{"position": "top"})); !ok || len(s.state.TextLayers) != 2 || s.state.TextLayers[0].Y != .15 {
		t.Fatalf("rerun must replace: %+v", s.state.TextLayers)
	}
	// Regression 2026-11-09: rerunning with nothing new must not report success, and a
	// timeline that already holds these captions must not yield a proposal.
	if content, _, ok := s.run(chatCall("2b", "add_subtitles", map[string]any{"position": "top"})); ok || !strings.Contains(string(content), "update_text") {
		t.Fatalf("identical rerun must report no change: ok=%v %s", ok, content)
	}
	applied := chatTestSession(t, []models.VideoEditorAsset{video}, segments, map[string]aiChatAnalysis{id: {transcript: chatTranscript(id)}})
	applied.base, applied.state = s.state.clone(), s.state.clone()
	applied.ops = nil
	if _, _, ok := applied.run(chatCall("2c", "add_subtitles", map[string]any{"position": "top"})); ok || applied.proposal("x") != nil {
		t.Fatal("captions already on the saved timeline: no change, no proposal")
	}
	none := chatTestSession(t, []models.VideoEditorAsset{video}, segments, map[string]aiChatAnalysis{})
	if _, _, ok := none.run(chatCall("3", "add_subtitles", map[string]any{})); ok {
		t.Fatal("no transcript, no captions")
	}
}

func TestAIChatLoopBudgetAndFailures(t *testing.T) {
	video := aiToolAsset("video", 10)
	s := chatTestSession(t, []models.VideoEditorAsset{video}, nil, map[string]aiChatAnalysis{})
	loop := &editorai.ChatReply{ToolCalls: []editorai.ToolCall{chatCall("1", "get_project", map[string]any{})}}
	caller := &chatTestCaller{repeat: loop}
	meta, final, err := runAIChatLoop(context.Background(), caller, func() error { return nil }, []editorai.ChatTurn{{Role: "user", Text: "x"}}, s, func(dto.VideoEditorAIChatEventDTO) {})
	if err != nil || final != "" || meta.Steps != aiChatMaxSteps || len(caller.requests) != aiChatMaxSteps {
		t.Fatalf("budget steps=%d final=%q err=%v", meta.Steps, final, err)
	}
	if text := aiChatFallbackText(nil, false, false); !strings.Contains(text, "ครบจำนวนขั้น") {
		t.Fatal(text)
	}
	calls := 0
	_, _, err = runAIChatLoop(context.Background(), &chatTestCaller{repeat: loop}, func() error {
		if calls++; calls > 2 {
			return repo.ErrAIDaily
		}
		return nil
	}, []editorai.ChatTurn{{Role: "user", Text: "x"}}, s, func(dto.VideoEditorAIChatEventDTO) {})
	if !errors.Is(err, repo.ErrAIDaily) || aiChatErrorCode(err) != "AI_DAILY_LIMIT" {
		t.Fatalf("quota %v", err)
	}
	if _, _, err = runAIChatLoop(context.Background(), &chatTestCaller{}, func() error { return nil }, nil, s, func(dto.VideoEditorAIChatEventDTO) {}); !errors.Is(err, editorai.ErrInvalid) {
		t.Fatalf("provider failure %v", err)
	}
	if !strings.Contains(aiChatFallbackText(&editorai.RateError{}, true, false), "บางส่วน") {
		t.Fatal("partial proposal must be mentioned")
	}
}

func TestAIChatHistoryTurns(t *testing.T) {
	meta := aiJSON(aiChatMeta{Proposal: &dto.VideoEditorAIChatProposalDTO{Status: "applied", Operations: []dto.VideoEditorAIEditOperationDTO{{Tool: "add_text"}}}})
	turns := aiChatHistoryTurns([]models.VideoEditorAIChatMessage{
		{Role: "assistant", Content: "ตอบกลางเธรด", Meta: aiJSON(aiChatMeta{})},
		{Role: "user", Content: "ใส่ข้อความ"},
		{Role: "assistant", Content: "", Meta: meta},
	})
	if len(turns) != 2 || turns[0].Role != "user" || turns[1].Role != "assistant" || !strings.Contains(turns[1].Text, "applied") {
		t.Fatalf("turns %+v", turns)
	}
}

func TestAIChatToolSchemasAreObjects(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range aiChatTools() {
		if seen[tool.Name] || aiChatToolLabels[tool.Name] == "" {
			t.Fatalf("tool %q duplicated or unlabelled", tool.Name)
		}
		seen[tool.Name] = true
		if len(tool.Parameters) > 0 {
			var schema struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(tool.Parameters, &schema); err != nil || schema.Type != "object" || len(schema.Properties) == 0 {
				t.Fatalf("schema %s: %v", tool.Name, err)
			}
		}
	}
}
