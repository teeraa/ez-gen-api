package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// The AI chat editor (experimental) is an agent loop: the model calls tools that read the
// project and its clip analysis, edit a private copy of the timeline, or start the
// existing story job. Edits become one proposal per reply that the user applies; the
// model never saves the project itself.

const (
	aiChatMaxMessage      = 2000
	aiChatMaxSteps        = 8
	aiChatMaxCallsPerStep = 6
	aiChatTimeout         = 140 * time.Second
	aiChatCallsPerDay     = 300
	aiChatHistory         = 16
	aiChatThreadLimit     = 100
	aiChatMaxLookAt       = 2
	aiChatOutputTokens    = 8192
)

var (
	ErrAIChatInput    = fmt.Errorf("AI chat request invalid: %w", ErrVEInvalid)
	ErrAIChatConsent  = errors.New("AI chat needs cloud consent")
	ErrAIChatProposal = errors.New("AI chat proposal is not pending")
	ErrAIChatBusy     = errors.New("AI chat reply already running")
	ErrAIChatModel    = errors.New("AI chat model not offered or not configured")
)

type VideoEditorAIChatFacade struct {
	ai      *VideoEditorAIFacade
	repo    *repo.VideoEditorAIChatRepository
	callers []editorai.ToolCaller // the models the user may pick, in display order
	caller  editorai.ToolCaller   // the default model
	enabled bool
	limits  *aiChatLimiter
}

// NewVideoEditorAIChatFacade takes the selectable models (may be empty) and the
// configured provider. The configured provider is the default when it is listed and
// available; otherwise the first available listed model is.
func NewVideoEditorAIChatFacade(ai *VideoEditorAIFacade, r *repo.VideoEditorAIChatRepository, callers []editorai.ToolCaller, configured editorai.ToolCaller, enabled bool) *VideoEditorAIChatFacade {
	if len(callers) == 0 && configured != nil {
		callers = []editorai.ToolCaller{configured}
	}
	f := &VideoEditorAIChatFacade{ai: ai, repo: r, callers: callers, enabled: enabled, limits: &aiChatLimiter{counts: map[string]int{}, active: map[string]bool{}, now: time.Now}}
	for _, c := range callers {
		if configured != nil && configured.Available() && editorai.ChatModelID(c) == editorai.ChatModelID(configured) && c.Available() {
			f.caller = c
			break
		}
	}
	for _, c := range callers {
		if f.caller == nil && c.Available() {
			f.caller = c
		}
	}
	if f.caller == nil && len(callers) > 0 {
		f.caller = callers[0]
	}
	return f
}

// pick returns the caller for a model id from the request; empty means the default.
func (f *VideoEditorAIChatFacade) pick(id string) (editorai.ToolCaller, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if f.caller == nil || !f.caller.Available() {
			return nil, ErrAIChatModel
		}
		return f.caller, nil
	}
	for _, c := range f.callers {
		if editorai.ChatModelID(c) == id {
			if !c.Available() {
				return nil, ErrAIChatModel
			}
			return c, nil
		}
	}
	return nil, ErrAIChatModel
}

var aiChatModelLabels = map[string]string{
	"gemini-3.5-flash-lite": "Gemini 3.5 Flash-Lite",
	"gemini-3.5-flash":      "Gemini 3.5 Flash",
	"claude-haiku-5-5":      "Claude Haiku 5.5",
	"claude-sonnet-5-5":     "Claude Sonnet 5.5",
	"claude-opus-5-5":       "Claude Opus 5.5",
}

func (f *VideoEditorAIChatFacade) models() []dto.VideoEditorAIChatModelDTO {
	out := []dto.VideoEditorAIChatModelDTO{}
	for _, c := range f.callers {
		m := dto.VideoEditorAIChatModelDTO{ID: editorai.ChatModelID(c), Provider: c.Name(), Model: c.Model(), Label: aiChatModelLabels[c.Model()], Available: c.Available()}
		if m.Label == "" {
			m.Label = c.Model()
		}
		if price, ok := aiChatPriceFor(c.Model()); ok {
			in, o := price.Input, price.Output
			m.InputPrice, m.OutputPrice = &in, &o
		}
		out = append(out, m)
	}
	return out
}

// aiChatLimiter admits one reply per user at a time and caps provider calls per UTC day.
type aiChatLimiter struct {
	mu     sync.Mutex
	day    string
	counts map[string]int
	active map[string]bool
	now    func() time.Time
}

func (l *aiChatLimiter) roll() {
	if day := l.now().UTC().Format("2006-01-02"); day != l.day {
		l.day, l.counts = day, map[string]int{}
	}
}
func (l *aiChatLimiter) enter(user string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	if l.active[user] {
		return nil, ErrAIChatBusy
	}
	if l.counts[user] >= aiChatCallsPerDay {
		return nil, repo.ErrAIDaily
	}
	l.active[user] = true
	return func() { l.mu.Lock(); delete(l.active, user); l.mu.Unlock() }, nil
}
func (l *aiChatLimiter) charge(user string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	if l.counts[user] >= aiChatCallsPerDay {
		return repo.ErrAIDaily
	}
	l.counts[user]++
	return nil
}

type aiChatMeta struct {
	Tools    []dto.VideoEditorAIChatToolDTO    `json:"tools,omitempty"`
	Proposal *dto.VideoEditorAIChatProposalDTO `json:"proposal,omitempty"`
	JobID    string                            `json:"jobId,omitempty"`
	Error    string                            `json:"error,omitempty"`
	Provider string                            `json:"provider,omitempty"`
	Model    string                            `json:"model,omitempty"`
	Steps    int                               `json:"steps,omitempty"`
	Usage    editorai.ChatUsage                `json:"usage"`
}

func aiChatReadMeta(m *models.VideoEditorAIChatMessage) aiChatMeta {
	var meta aiChatMeta
	_ = json.Unmarshal(m.Meta, &meta)
	return meta
}
func aiChatMessageDTO(m *models.VideoEditorAIChatMessage) dto.VideoEditorAIChatMessageDTO {
	meta := aiChatReadMeta(m)
	d := dto.VideoEditorAIChatMessageDTO{ID: m.ID.String(), Role: m.Role, Content: m.Content, CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339), Tools: meta.Tools, Proposal: meta.Proposal, JobID: meta.JobID, Error: meta.Error}
	if d.Tools == nil {
		d.Tools = []dto.VideoEditorAIChatToolDTO{}
	}
	return d
}

func (f *VideoEditorAIChatFacade) unavailableReason(ctx context.Context) string {
	switch {
	case f == nil || !f.enabled:
		return "แชทตัดต่อด้วย AI ยังไม่เปิดใช้งาน (EDITOR_AI_CHAT_ENABLED)"
	case f.ai == nil || !f.ai.options.Enabled || f.ai.editor == nil || !f.ai.editor.available:
		return "ระบบตัดต่อหรือ AI ยังไม่พร้อมใช้งาน"
	case f.caller == nil || !f.caller.Available():
		return "ยังไม่ได้ตั้งค่าผู้ให้บริการ AI ของแชท"
	case !f.repo.Ready(ctx):
		return "ฐานข้อมูลยังไม่มีตารางแชท (migration 031)"
	}
	return ""
}

const aiChatCloudNotice = "ข้อความของคุณ ชื่อไฟล์ timeline ผลถอดเสียง คำอธิบายภาพ และภาพตัวอย่างที่ AI ขอดู จะถูกส่งไปยังผู้ให้บริการ AI ภายนอก กรุณาใช้เฉพาะข้อมูลที่เปิดเผยได้"

// Thread returns the chat of a project plus whether sending is possible.
func (f *VideoEditorAIChatFacade) Thread(ctx context.Context, project, user string) (dto.VideoEditorAIChatDTO, error) {
	out := dto.VideoEditorAIChatDTO{Models: f.models(), CloudNotice: aiChatCloudNotice, MaxMessageLength: aiChatMaxMessage, Messages: []dto.VideoEditorAIChatMessageDTO{}}
	if !veID(project) || !veID(user) {
		return out, repo.ErrNotFound
	}
	if f.caller != nil {
		out.Provider, out.Model, out.DefaultModel = f.caller.Name(), f.caller.Model(), editorai.ChatModelID(f.caller)
	}
	if _, e := f.ai.editor.repo.Project(ctx, project, user); e != nil {
		return out, e
	}
	out.UnavailableReason = f.unavailableReason(ctx)
	out.Available = out.UnavailableReason == ""
	if !f.repo.Ready(ctx) {
		return out, nil
	}
	rows, e := f.repo.Recent(ctx, project, user, aiChatThreadLimit)
	if e != nil {
		return out, e
	}
	for i := range rows {
		out.Messages = append(out.Messages, aiChatMessageDTO(&rows[i]))
	}
	return out, nil
}

// Clear starts a new conversation: earlier messages are soft-deleted and are neither
// shown nor replayed to the model. Story jobs they started keep running.
func (f *VideoEditorAIChatFacade) Clear(ctx context.Context, project, user string) (dto.VideoEditorAIChatDTO, error) {
	if !veID(project) || !veID(user) {
		return dto.VideoEditorAIChatDTO{}, repo.ErrNotFound
	}
	if _, e := f.ai.editor.repo.Project(ctx, project, user); e != nil {
		return dto.VideoEditorAIChatDTO{}, e
	}
	if f.repo.Ready(ctx) {
		if e := f.repo.Clear(ctx, project, user); e != nil {
			return dto.VideoEditorAIChatDTO{}, e
		}
	}
	return f.Thread(ctx, project, user)
}

// Precheck validates a send before the response turns into an event stream.
func (f *VideoEditorAIChatFacade) Precheck(ctx context.Context, project, user string, in req.SendVideoEditorAIChat) error {
	if !veID(project) || !veID(user) {
		return repo.ErrNotFound
	}
	text := strings.TrimSpace(in.Message)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > aiChatMaxMessage {
		return ErrAIChatInput
	}
	if !in.CloudConsent {
		return ErrAIChatConsent
	}
	if f.unavailableReason(ctx) != "" {
		return editorai.ErrUnavailable
	}
	if _, e := f.pick(in.Model); e != nil {
		return e
	}
	if e := f.ai.editor.schemaReady(ctx); e != nil {
		return e
	}
	_, e := f.ai.editor.repo.Project(ctx, project, user)
	return e
}

// Send runs one user message through the agent loop, reporting progress through emit,
// and stores both messages. The returned error is for logging; emit has told the client.
func (f *VideoEditorAIChatFacade) Send(ctx context.Context, project, user string, in req.SendVideoEditorAIChat, emit func(dto.VideoEditorAIChatEventDTO)) error {
	if e := f.Precheck(ctx, project, user, in); e != nil {
		return e
	}
	release, e := f.limits.enter(user)
	if e != nil {
		return e
	}
	defer release()
	text := strings.TrimSpace(in.Message)
	p, e := f.ai.editor.repo.Project(ctx, project, user)
	if e != nil {
		return e
	}
	assets, e := f.ai.editor.repo.Assets(ctx, project, user)
	if e != nil {
		return e
	}
	history, e := f.repo.Recent(ctx, project, user, aiChatHistory)
	if e != nil {
		return e
	}
	// Each message costs a flat price, charged before the model is called.
	pointRef := uuid.NewString()
	if e = chargePoints(ctx, user, pointFeatureChat, pointRef, pointPrices.ChatMessage); e != nil {
		return e
	}
	userID, projectID := uuid.MustParse(user), uuid.MustParse(project)
	if e = f.repo.Create(ctx, &models.VideoEditorAIChatMessage{UserID: userID, ProjectID: projectID, Role: "user", Content: text, Meta: datatypes.JSON("{}")}); e != nil {
		refundPoints(ctx, user, pointFeatureChat, pointRef)
		return e
	}
	s := newAIChatSession(ctx, f, p, user, assets)
	defer s.cleanup()
	turns := aiChatHistoryTurns(history)
	state, _ := json.Marshal(s.projectState())
	turns = append(turns, editorai.ChatTurn{Role: "user", Text: text + "\n\n<project_state>\n" + string(state) + "\n</project_state>"})
	caller, e := f.pick(in.Model)
	if e != nil {
		return e
	}
	s.caller, s.charge = caller, func() error { return f.limits.charge(user) }
	meta, final, failure := runAIChatLoop(ctx, caller, s.charge, turns, s, emit)
	// Calls made inside tools (caption proofreading) are billed too.
	meta.Steps += s.extraCalls
	meta.Usage.InputTokens += s.extra.InputTokens
	meta.Usage.OutputTokens += s.extra.OutputTokens
	if failure != nil {
		log.Printf("video_editor_ai_chat: reply stopped steps=%d err=%v", meta.Steps, failure)
		meta.Error = aiChatErrorCode(failure)
	}
	meta.JobID = s.jobID
	if proposal := s.proposal(final); proposal != nil {
		meta.Proposal = proposal
	}
	if failure != nil && final == "" && meta.Proposal == nil && s.jobID == "" {
		// The model gave nothing the user can use; the message is not charged.
		refundPoints(ctx, user, pointFeatureChat, pointRef)
	}
	if final == "" {
		final = aiChatFallbackText(failure, meta.Proposal != nil, s.jobID != "")
	} else if meta.Proposal == nil && s.jobID == "" && aiChatTriedEdit(meta.Tools) {
		// The model may still claim a change; the card is what the user acts on, so say so.
		final += "\n\n(ข้อความนี้ไม่มีการแก้ timeline ให้นำไปใช้ เพราะผลลัพธ์เหมือนเดิม)"
	}
	if runes := []rune(final); len(runes) > 4000 {
		final = string(runes[:4000])
	}
	answer := &models.VideoEditorAIChatMessage{UserID: userID, ProjectID: projectID, Role: "assistant", Content: final, Meta: aiJSON(meta)}
	// The reply is stored even if the client went away, so it shows on the next load.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if e = f.repo.Create(saveCtx, answer); e != nil {
		return e
	}
	d := aiChatMessageDTO(answer)
	// A provider failure is part of the stored reply (meta.error), not a stream error.
	emit(dto.VideoEditorAIChatEventDTO{Type: "message", Message: &d})
	return nil
}

// aiChatTriedEdit reports whether the reply called a timeline-editing tool.
func aiChatTriedEdit(tools []dto.VideoEditorAIChatToolDTO) bool {
	for _, t := range tools {
		switch t.Name {
		case "set_story", "add_segment", "add_audio", "add_subtitles", "edit_timeline":
			return true
		}
	}
	return false
}

func aiChatErrorCode(e error) string {
	var rate *editorai.RateError
	switch {
	case errors.As(e, &rate):
		return "AI_RATE_LIMITED"
	case errors.Is(e, repo.ErrAIDaily):
		return "AI_DAILY_LIMIT"
	case errors.Is(e, context.DeadlineExceeded), errors.Is(e, context.Canceled):
		return "AI_TIMEOUT"
	case errors.Is(e, editorai.ErrInvalid):
		return "AI_INVALID_RESULT"
	case errors.Is(e, editorai.ErrTooLarge):
		return "AI_INPUT_TOO_LARGE"
	}
	return "AI_PROVIDER_UNAVAILABLE"
}

func aiChatFallbackText(failure error, proposal, job bool) string {
	switch {
	case failure == nil && proposal:
		return "เตรียมการแก้ไว้แล้ว ตรวจรายการแล้วกด “นำไปใช้” ได้เลย"
	case failure == nil && job:
		return "เริ่มวิเคราะห์คลิปและร้อยเรื่องให้แล้ว รอการ์ดความคืบหน้าด้านล่างสักครู่"
	case failure == nil:
		return "ทำงานครบจำนวนขั้นที่กำหนดแล้ว ลองบอกสิ่งที่ต้องการให้เจาะจงขึ้นอีกนิด"
	}
	prefix := ""
	if proposal {
		prefix = "เตรียมการแก้ได้บางส่วนแล้ว (ดูรายการด้านล่าง) แต่ "
	}
	switch aiChatErrorCode(failure) {
	case "AI_DAILY_LIMIT":
		return prefix + "ใช้ AI ครบโควตาวันนี้แล้ว"
	case "AI_RATE_LIMITED":
		return prefix + "ผู้ให้บริการ AI จำกัดคำขอชั่วคราว กรุณารอสักครู่แล้วลองใหม่"
	case "AI_TIMEOUT":
		return prefix + "ใช้เวลานานเกินกำหนด ลองแบ่งคำสั่งให้สั้นลง"
	}
	return prefix + "เชื่อมต่อผู้ให้บริการ AI ไม่สำเร็จ กรุณาลองใหม่"
}

// aiChatHistoryTurns replays earlier messages as plain text; tool traffic is not
// replayed (the current project state is sent fresh), which keeps requests small.
func aiChatHistoryTurns(rows []models.VideoEditorAIChatMessage) []editorai.ChatTurn {
	turns := []editorai.ChatTurn{}
	for i := range rows {
		m := &rows[i]
		if m.Role == "user" {
			turns = append(turns, editorai.ChatTurn{Role: "user", Text: m.Content})
			continue
		}
		if len(turns) == 0 {
			continue // a thread replayed from the middle must start with the user
		}
		meta := aiChatReadMeta(m)
		text := m.Content
		if meta.Proposal != nil {
			text += fmt.Sprintf("\n[ระบบ: ข้อเสนอแก้ timeline %d รายการ สถานะ %s]", len(meta.Proposal.Operations), meta.Proposal.Status)
		}
		if meta.JobID != "" {
			text += "\n[ระบบ: เริ่มงานวิเคราะห์และร้อยเรื่อง " + meta.JobID + "]"
		}
		if strings.TrimSpace(text) == "" {
			text = "(ไม่มีข้อความ)"
		}
		turns = append(turns, editorai.ChatTurn{Role: "assistant", Text: text})
	}
	return turns
}

// Apply saves a pending proposal as the project at its base revision.
func (f *VideoEditorAIChatFacade) Apply(ctx context.Context, project, id, user string) (dto.VideoEditorAIChatApplyDTO, error) {
	if !veID(project) || !veID(id) || !veID(user) {
		return dto.VideoEditorAIChatApplyDTO{}, repo.ErrNotFound
	}
	if !f.repo.Ready(ctx) {
		return dto.VideoEditorAIChatApplyDTO{}, editorai.ErrUnavailable
	}
	m, e := f.repo.Get(ctx, project, id, user)
	if e != nil {
		return dto.VideoEditorAIChatApplyDTO{}, e
	}
	previous := m.Meta
	meta := aiChatReadMeta(m)
	if meta.Proposal == nil || meta.Proposal.Status != "pending" {
		return dto.VideoEditorAIChatApplyDTO{}, ErrAIChatProposal
	}
	proposal := meta.Proposal.Proposal
	audio, text := proposal.AudioClips, proposal.TextLayers
	saved, e := f.ai.editor.SaveProject(ctx, project, user, req.SaveVideoEditorProject{Revision: meta.Proposal.BaseRevision, Title: proposal.Title, AspectRatio: proposal.AspectRatio, Segments: proposal.Segments, AudioClips: &audio, TextLayers: &text, ImageLayers: proposal.ImageLayers})
	if e != nil {
		if errors.Is(e, repo.ErrVERevision) {
			meta.Proposal.Status = "stale"
			_, _ = f.repo.UpdateMeta(ctx, m, previous, aiJSON(meta))
		}
		return dto.VideoEditorAIChatApplyDTO{}, e
	}
	meta.Proposal.Status, meta.Proposal.AppliedRevision = "applied", saved.Revision
	ok, e := f.repo.UpdateMeta(ctx, m, previous, aiJSON(meta))
	if e != nil {
		return dto.VideoEditorAIChatApplyDTO{}, e
	}
	if !ok {
		// Another tab discarded the proposal while this save ran. The project is saved, so
		// record what really happened on top of the message's current meta.
		if m, e = f.repo.Get(ctx, project, id, user); e != nil {
			return dto.VideoEditorAIChatApplyDTO{}, e
		}
		current := aiChatReadMeta(m)
		if current.Proposal != nil {
			current.Proposal.Status, current.Proposal.AppliedRevision = "applied", saved.Revision
			if _, e = f.repo.UpdateMeta(ctx, m, m.Meta, aiJSON(current)); e != nil {
				return dto.VideoEditorAIChatApplyDTO{}, e
			}
		}
	}
	return dto.VideoEditorAIChatApplyDTO{Message: aiChatMessageDTO(m), Project: saved}, nil
}

// Discard marks a pending proposal as not wanted.
func (f *VideoEditorAIChatFacade) Discard(ctx context.Context, project, id, user string) (dto.VideoEditorAIChatMessageDTO, error) {
	if !veID(project) || !veID(id) || !veID(user) {
		return dto.VideoEditorAIChatMessageDTO{}, repo.ErrNotFound
	}
	if !f.repo.Ready(ctx) {
		return dto.VideoEditorAIChatMessageDTO{}, editorai.ErrUnavailable
	}
	m, e := f.repo.Get(ctx, project, id, user)
	if e != nil {
		return dto.VideoEditorAIChatMessageDTO{}, e
	}
	previous := m.Meta
	meta := aiChatReadMeta(m)
	if meta.Proposal == nil || meta.Proposal.Status != "pending" {
		return dto.VideoEditorAIChatMessageDTO{}, ErrAIChatProposal
	}
	meta.Proposal.Status = "discarded"
	ok, e := f.repo.UpdateMeta(ctx, m, previous, aiJSON(meta))
	if e != nil {
		return dto.VideoEditorAIChatMessageDTO{}, e
	}
	if !ok {
		return dto.VideoEditorAIChatMessageDTO{}, ErrAIChatProposal
	}
	return aiChatMessageDTO(m), nil
}

// AIChatTimeout bounds one streamed reply; it stays under the server's write timeout.
func AIChatTimeout() time.Duration { return aiChatTimeout }

// runAIChatLoop alternates model calls and tool runs until the model answers in text,
// a step budget is spent or a call fails. Tool effects accumulate in s.
func runAIChatLoop(ctx context.Context, caller editorai.ToolCaller, charge func() error, turns []editorai.ChatTurn, s *aiChatSession, emit func(dto.VideoEditorAIChatEventDTO)) (aiChatMeta, string, error) {
	meta := aiChatMeta{Tools: []dto.VideoEditorAIChatToolDTO{}, Provider: caller.Name(), Model: caller.Model()}
	final := ""
	var failure error
	for step := 0; step < aiChatMaxSteps; step++ {
		if failure = charge(); failure != nil {
			break
		}
		reply, err := caller.Chat(ctx, editorai.ChatRequest{System: aiChatSystem, Turns: turns, Tools: aiChatTools(), MaxOutputTokens: aiChatOutputTokens})
		if err != nil {
			failure = err
			if ctx.Err() != nil {
				// Providers report any failed request as unavailable; a spent reply budget
				// is a timeout (AI_TIMEOUT tells the user to split the request).
				failure = ctx.Err()
			}
			break
		}
		meta.Steps++
		meta.Usage.InputTokens += reply.Usage.InputTokens
		meta.Usage.OutputTokens += reply.Usage.OutputTokens
		turns = append(turns, reply.Turn())
		if len(reply.ToolCalls) == 0 {
			final = strings.TrimSpace(reply.Text)
			break
		}
		results := []editorai.ToolResult{}
		for i, call := range reply.ToolCalls {
			if i >= aiChatMaxCallsPerStep {
				results = append(results, editorai.ToolResult{CallID: call.ID, Name: call.Name, Content: aiChatError("เรียกเครื่องมือได้ไม่เกิน 6 ครั้งต่อรอบ")})
				continue
			}
			label := aiChatToolLabels[call.Name]
			if label == "" {
				label = "ใช้เครื่องมือ"
			}
			emit(dto.VideoEditorAIChatEventDTO{Type: "status", Tool: call.Name, Label: label})
			content, images, ok := s.run(call)
			meta.Tools = append(meta.Tools, dto.VideoEditorAIChatToolDTO{Name: call.Name, Label: label, OK: ok})
			results = append(results, editorai.ToolResult{CallID: call.ID, Name: call.Name, Content: content, Images: images})
		}
		turns = append(turns, editorai.ChatTurn{Role: "user", ToolResults: results})
		if ctx.Err() != nil {
			failure = ctx.Err()
			break
		}
	}
	return meta, final, failure
}
