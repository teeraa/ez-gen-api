package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/datatypes"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type EditorAIOptions struct {
	Enabled bool
	Fixture bool
}
type VideoEditorAIFacade struct {
	repo     *repo.VideoEditorAIRepository
	editor   *VideoEditorFacade
	provider editorai.Provider
	asr      editorai.Transcriber
	options  EditorAIOptions
	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewVideoEditorAIFacade(r *repo.VideoEditorAIRepository, editor *VideoEditorFacade, p editorai.Provider, a editorai.Transcriber, options EditorAIOptions) *VideoEditorAIFacade {
	return &VideoEditorAIFacade{repo: r, editor: editor, provider: p, asr: a, options: options, cancels: map[string]context.CancelFunc{}}
}
func aiJSON(v any) datatypes.JSON { b, _ := json.Marshal(v); return datatypes.JSON(b) }
func aiInput(j *models.VideoEditorAIJob) models.EditorAIInput {
	var in models.EditorAIInput
	_ = json.Unmarshal(j.Input, &in)
	return in
}
func aiFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (f *VideoEditorAIFacade) tablesReady() bool {
	if !f.options.Enabled {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return f.repo.Ready(ctx)
}
func (f *VideoEditorAIFacade) Capabilities() dto.VideoEditorAICapabilitiesDTO {
	a := f.asr != nil && f.asr.Available()
	p := f.provider != nil && f.provider.Available()
	available := f.tablesReady() && a && p && f.editor.available
	reason := ""
	if !available {
		reason = "ระบบวิเคราะห์ยังไม่พร้อมใช้งาน กรุณาตั้งค่าบริการวิเคราะห์และถอดเสียง"
	}
	mode := "configured"
	if f.options.Fixture {
		mode = "fixture"
	}
	return dto.VideoEditorAICapabilitiesDTO{AutoCompose: true, Available: available, UnavailableReason: reason, TranscriptionAvailable: a, AnalysisAvailable: p, MaxAssets: 8, MaxSourceDuration: 600, MaxFrames: 24, MaxTargetDuration: 120, MaxSegments: 20, MaxJobsPerDay: 20, MaxCloudCalls: 6, CloudNotice: "การวิเคราะห์จะส่งภาพตัวอย่างและข้อความถอดเสียงไปยัง Google บริการฟรีอาจใช้ข้อมูลเพื่อปรับปรุงผลิตภัณฑ์ กรุณาใช้เฉพาะข้อมูลที่เปิดเผยได้", ConnectionMode: mode, SupportedOptions: dto.EditorAISupportedOptions{RemoveSilence: true, Subtitles: true, Music: true}}
}
func aiDTO(j *models.VideoEditorAIJob) dto.VideoEditorAIJobDTO {
	in := aiInput(j)
	d := dto.VideoEditorAIJobDTO{AutoCompose: in.AutoCompose, ID: j.ID.String(), ProjectID: j.ProjectID.String(), SourceRevision: j.SourceRevision, Phase: j.Phase, Status: j.Status, Stage: j.Stage, AssetIDs: []string{}, Brief: in.Brief, TargetDuration: in.TargetDuration, Style: in.Style, AudioMode: in.AudioMode, RemoveSilence: in.RemoveSilence, Progress: dto.EditorAIProgressDTO{CompletedAssets: j.CompletedAssets, TotalAssets: len(in.Sources)}, Transcripts: []models.EditorAITranscript{}, Visuals: []models.EditorAIVisual{}, Concepts: []models.EditorAIConcept{}, CloudCallsUsed: j.CloudCallsUsed, CloudCallsLimit: 6, FailureCode: j.FailureCode, FailureMessage: j.FailureMessage, CreatedAt: j.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: j.UpdatedAt.UTC().Format(time.RFC3339)}
	for _, s := range in.Sources {
		d.AssetIDs = append(d.AssetIDs, s.ID)
	}
	_ = json.Unmarshal(j.Transcripts, &d.Transcripts)
	_ = json.Unmarshal(j.Visuals, &d.Visuals)
	_ = json.Unmarshal(j.Concepts, &d.Concepts)
	if j.SelectedConceptID != nil {
		s := j.SelectedConceptID.String()
		d.SelectedConceptID = &s
	}
	if len(j.Draft) > 0 && string(j.Draft) != "null" {
		var draft models.EditorAIDraft
		if json.Unmarshal(j.Draft, &draft) == nil {
			if draft.TextLayers == nil {
				draft.TextLayers = []models.VideoEditorTextLayer{}
			}
			if draft.AudioClips == nil {
				draft.AudioClips = []models.VideoEditorAudioClip{}
			}
			if draft.CompositionNotes == nil {
				draft.CompositionNotes = []string{}
			}
			d.Draft = &draft
		}
	}
	attempts := j.AnalysisAttempts
	if j.Phase == "planning" {
		attempts = j.PlanningAttempts
	}
	d.CanRetry = j.Status == "failed" && j.CloudCallsUsed < 6 && attempts < 3 && (j.RetryAt == nil || !j.RetryAt.After(time.Now())) && j.FailureCode != "AI_INVALID_RESULT" && j.FailureCode != "AI_INPUT_TOO_LARGE"
	if j.RetryAt != nil {
		s := j.RetryAt.UTC().Format(time.RFC3339)
		d.RetryAt = &s
	}
	if j.Status == "applied" {
		var p models.VideoEditorProject
		if json.Unmarshal(j.AppliedProject, &p) == nil {
			d.AppliedRevision = &p.Revision
		}
	}
	return d
}
func (f *VideoEditorAIFacade) Create(ctx context.Context, project, user string, in req.CreateVideoEditorAIJob) (dto.VideoEditorAIJobDTO, bool, error) {
	if !f.tablesReady() {
		return dto.VideoEditorAIJobDTO{}, false, editorai.ErrUnavailable
	}

	if !veID(project) || !veID(user) {
		return dto.VideoEditorAIJobDTO{}, false, repo.ErrNotFound
	}
	if !veID(in.RequestID) || in.Revision < 1 || len(in.AssetIDs) < 1 || len(in.AssetIDs) > 8 || strings.TrimSpace(in.Brief) == "" || utf8.RuneCountInString(in.Brief) > 4000 || !aiFinite(in.TargetDuration) || in.TargetDuration < 1 || in.TargetDuration > 120 || !in.CloudConsent {
		return dto.VideoEditorAIJobDTO{}, false, ErrVEInvalid
	}
	if in.Style != "warm" && in.Style != "fun" && in.Style != "minimal" && in.Style != "professional" || in.AudioMode != "keep" && in.AudioMode != "low" && in.AudioMode != "mute" {
		return dto.VideoEditorAIJobDTO{}, false, ErrVEInvalid
	}
	hash := veHash(struct {
		Project string
		Input   req.CreateVideoEditorAIJob
	}{project, in})
	old, e := f.repo.Existing(ctx, user, in.RequestID)
	if e == nil {
		if old.ProjectID.String() != project || old.RequestHash != hash {
			return dto.VideoEditorAIJobDTO{}, false, repo.ErrVEIdempotency
		}
		return aiDTO(old), false, nil
	}
	if !errors.Is(e, repo.ErrNotFound) {
		return dto.VideoEditorAIJobDTO{}, false, e
	}
	if !f.Capabilities().Available {
		return dto.VideoEditorAIJobDTO{}, false, editorai.ErrUnavailable
	}
	p, e := f.editor.repo.Project(ctx, project, user)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, false, e
	}
	assets, e := f.editor.repo.Assets(ctx, project, user)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, false, e
	}
	byID := map[string]models.VideoEditorAsset{}
	for _, a := range assets {
		byID[a.ID.String()] = a
	}
	seen := map[string]bool{}
	sources := []models.EditorAISource{}
	duration := 0.0
	for _, id := range in.AssetIDs {
		if !veID(id) || seen[id] {
			return dto.VideoEditorAIJobDTO{}, false, ErrVEInvalid
		}
		seen[id] = true
		a, ok := byID[id]
		if !ok {
			return dto.VideoEditorAIJobDTO{}, false, repo.ErrNotFound
		}
		if a.Status != "ready" || veKind(&a) != "video" || a.SourceKey == "" || !aiFinite(a.Duration) || a.Duration <= 0 {
			return dto.VideoEditorAIJobDTO{}, false, ErrVEInvalid
		}
		duration += a.Duration
		sources = append(sources, models.EditorAISource{ID: id, Hash: a.ContentHash, Key: a.SourceKey, Duration: a.Duration, HasAudio: a.HasAudio})
	}
	if duration > 600 {
		return dto.VideoEditorAIJobDTO{}, false, editorai.ErrTooLarge
	}
	snapshot := models.EditorAIInput{Sources: sources, Brief: in.Brief, TargetDuration: in.TargetDuration, Style: in.Style, AudioMode: in.AudioMode, RemoveSilence: in.RemoveSilence, ModelSignature: f.provider.Signature(), ASRSignature: f.asr.Signature()}
	if in.AutoCompose {
		snapshot.AutoCompose = true
		for _, clip := range veAudioClips(p) {
			if !veID(clip.OriginJobID) {
				snapshot.ManualAudio = true
			}
		}
		for _, layer := range veTextLayers(p) {
			if !veID(layer.OriginJobID) {
				snapshot.ManualTextCount++
			}
		}
		for _, asset := range assets {
			if asset.Status == "ready" && veKind(&asset) == "audio" && asset.SourceKey != "" && aiFinite(asset.Duration) && asset.Duration > 0 {
				snapshot.AudioSources = append(snapshot.AudioSources, models.EditorAISource{ID: asset.ID.String(), Hash: asset.ContentHash, Key: asset.SourceKey, Duration: asset.Duration, HasAudio: true})
			}
		}
	}
	j := models.VideoEditorAIJob{UserID: p.UserID, ProjectID: p.ID, RequestID: uuid.MustParse(in.RequestID), RequestHash: hash, SourceRevision: in.Revision, Phase: "analysis", Status: "queued", Stage: "queued", Input: aiJSON(snapshot), Transcripts: aiJSON([]models.EditorAITranscript{}), Visuals: aiJSON([]models.EditorAIVisual{}), Concepts: aiJSON([]models.EditorAIConcept{}), Evidence: aiJSON([]models.EditorAIEvidence{}), Draft: datatypes.JSON("null"), RetryRequests: datatypes.JSON("[]"), AppliedProject: datatypes.JSON("null")}
	created, e := f.repo.Create(ctx, &j)
	return aiDTO(&j), created, e
}

var ErrAIStatusUnavailable = errors.New("AI status temporarily unavailable")

func aiStatusReadError(err error) error {
	if err == nil || errors.Is(err, repo.ErrNotFound) {
		return err
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "42P01" {
		return editorai.ErrUnavailable
	}
	return ErrAIStatusUnavailable
}

func (f *VideoEditorAIFacade) Get(ctx context.Context, id, user string) (dto.VideoEditorAIJobDTO, error) {
	if !f.options.Enabled {
		return dto.VideoEditorAIJobDTO{}, editorai.ErrUnavailable
	}

	if !veID(id) || !veID(user) {
		return dto.VideoEditorAIJobDTO{}, repo.ErrNotFound
	}
	if e := f.repo.Expire(ctx); e != nil {
		return dto.VideoEditorAIJobDTO{}, aiStatusReadError(e)
	}
	j, e := f.repo.Get(ctx, id, user)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, aiStatusReadError(e)
	}
	return aiDTO(j), nil
}
func (f *VideoEditorAIFacade) List(ctx context.Context, project, user string) (dto.VideoEditorAIJobsDTO, error) {
	out := dto.VideoEditorAIJobsDTO{Items: []dto.VideoEditorAIJobDTO{}}
	if !veID(project) || !veID(user) {
		return out, repo.ErrNotFound
	}
	if !f.options.Enabled {
		if _, e := f.editor.repo.Project(ctx, project, user); e != nil {
			return out, e
		}
		return out, nil
	}
	if !f.tablesReady() {
		return out, editorai.ErrUnavailable
	}
	if !veID(project) || !veID(user) {
		return out, repo.ErrNotFound
	}
	if _, e := f.editor.repo.Project(ctx, project, user); e != nil {
		return out, e
	}
	if e := f.repo.Expire(ctx); e != nil {
		return out, e
	}
	jobs, e := f.repo.List(ctx, project, user)
	for i := range jobs {
		out.Items = append(out.Items, aiDTO(&jobs[i]))
	}
	return out, e
}
func (f *VideoEditorAIFacade) Select(ctx context.Context, id, user, concept string) (dto.VideoEditorAIJobDTO, error) {
	if !f.tablesReady() {
		return dto.VideoEditorAIJobDTO{}, editorai.ErrUnavailable
	}

	if !veID(id) || !veID(user) {
		return dto.VideoEditorAIJobDTO{}, repo.ErrNotFound
	}
	if !veID(concept) {
		return dto.VideoEditorAIJobDTO{}, ErrVEInvalid
	}
	j, e := f.repo.Select(ctx, id, user, concept)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, e
	}
	return aiDTO(j), nil
}
func validateAIDraft(d *models.EditorAIDraft, in models.EditorAIInput, known map[string]bool) error {
	if len(d.Segments) < 1 || len(d.Segments) > 20 {
		return editorai.ErrInvalid
	}
	sources := map[string]models.EditorAISource{}
	for _, s := range in.Sources {
		sources[s.ID] = s
	}
	seen := map[string]bool{}
	duration := 0.0
	for _, s := range d.Segments {
		a, ok := sources[s.AssetID]
		if !ok || !veID(s.ID) || seen[s.ID] || known != nil && !known[s.ID] || !aiFinite(s.Start) || !aiFinite(s.End) || !aiFinite(s.Volume) || s.Start < 0 || s.End <= s.Start || s.End > a.Duration+.001 || s.Volume < 0 || s.Volume > 1 || strings.TrimSpace(s.Title) == "" || utf8.RuneCountInString(s.Title) > 200 || utf8.RuneCountInString(s.Reason) > 2000 {
			return editorai.ErrInvalid
		}
		seen[s.ID] = true
		duration += s.End - s.Start
	}
	if duration > 1200 || known == nil && duration > in.TargetDuration+.05 {
		return editorai.ErrInvalid
	}
	d.Duration = duration
	if duration >= in.TargetDuration*.95-.001 {
		d.DurationReason = ""
	}
	return nil
}
func (f *VideoEditorAIFacade) UpdateDraft(ctx context.Context, id, user string, in req.UpdateVideoEditorAIDraft) (dto.VideoEditorAIJobDTO, error) {
	if !f.tablesReady() {
		return dto.VideoEditorAIJobDTO{}, editorai.ErrUnavailable
	}

	if !veID(id) || !veID(user) {
		return dto.VideoEditorAIJobDTO{}, repo.ErrNotFound
	}
	j, e := f.repo.Get(ctx, id, user)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, e
	}
	var old models.EditorAIDraft
	if json.Unmarshal(j.Draft, &old) != nil {
		return dto.VideoEditorAIJobDTO{}, repo.ErrAIDraft
	}
	known := map[string]bool{}
	for _, s := range old.Segments {
		known[s.ID] = true
	}
	d := models.EditorAIDraft{Revision: in.DraftRevision, Segments: in.Segments}
	if e = validateAIDraft(&d, aiInput(j), known); e != nil {
		return dto.VideoEditorAIJobDTO{}, e
	}
	d.DurationReason = aiDurationReason(d.Duration, "ฉบับร่างที่แก้ไขมีความยาวสั้นกว่าเป้า คุณยังนำไปใช้หรือปรับช่วงตัดต่อได้", aiInput(j), nil)
	if aiInput(j).AutoCompose {
		var transcripts []models.EditorAITranscript
		if json.Unmarshal(j.Transcripts, &transcripts) != nil {
			return dto.VideoEditorAIJobDTO{}, editorai.ErrInvalid
		}
		composeAIDraft(&d, aiInput(j), j.ID.String(), transcripts)
		if err := validateAIComposition(d, aiInput(j), j.ID.String()); err != nil {
			return dto.VideoEditorAIJobDTO{}, err
		}
	}
	j, e = f.repo.UpdateDraft(ctx, id, user, in.DraftRevision, d)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, e
	}
	return aiDTO(j), nil
}
func (f *VideoEditorAIFacade) Apply(ctx context.Context, id, user string, in req.ApplyVideoEditorAIDraft) (dto.VideoEditorProjectDTO, error) {
	if !f.tablesReady() {
		return dto.VideoEditorProjectDTO{}, editorai.ErrUnavailable
	}

	if !veID(id) || !veID(user) {
		return dto.VideoEditorProjectDTO{}, repo.ErrNotFound
	}
	if !veID(in.RequestID) || in.Revision < 1 || in.DraftRevision < 1 {
		return dto.VideoEditorProjectDTO{}, ErrVEInvalid
	}
	j, e := f.repo.Get(ctx, id, user)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	// Applied replays retain their original response.
	if j.Status == "review" {
		var draft models.EditorAIDraft
		if json.Unmarshal(j.Draft, &draft) != nil {
			return dto.VideoEditorProjectDTO{}, repo.ErrAIDraft
		}
		known := map[string]bool{}
		for _, segment := range draft.Segments {
			known[segment.ID] = true
		}
		if draft.Revision == 1 {
			known = nil
		}
		if e = validateAIDraft(&draft, aiInput(j), known); e != nil {
			return dto.VideoEditorProjectDTO{}, e
		}
		if aiInput(j).AutoCompose {
			if e = validateAIComposition(draft, aiInput(j), j.ID.String()); e != nil {
				return dto.VideoEditorProjectDTO{}, e
			}
			current, e := f.editor.repo.Project(ctx, j.ProjectID.String(), user)
			if e != nil {
				return dto.VideoEditorProjectDTO{}, e
			}
			assets, e := f.editor.repo.Assets(ctx, j.ProjectID.String(), user)
			if e != nil {
				return dto.VideoEditorProjectDTO{}, e
			}
			if e = validateAIApplyTracks(current, draft, assets); e != nil {
				return dto.VideoEditorProjectDTO{}, e
			}
		}
	}
	p, e := f.repo.Apply(ctx, id, user, in.RequestID, in.Revision, in.DraftRevision, in.ApprovedSegmentIDs)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	d := veProjectDTO(p)
	d.Assets, e = f.editor.Assets(ctx, p.ID.String(), user)
	return d, e
}
func (f *VideoEditorAIFacade) Cancel(ctx context.Context, id, user string) (dto.VideoEditorAIJobDTO, error) {
	if !f.tablesReady() {
		return dto.VideoEditorAIJobDTO{}, editorai.ErrUnavailable
	}

	if !veID(id) || !veID(user) {
		return dto.VideoEditorAIJobDTO{}, repo.ErrNotFound
	}
	j, e := f.repo.Cancel(ctx, id, user)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, e
	}
	f.mu.Lock()
	cancel := f.cancels[id]
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return aiDTO(j), nil
}
func (f *VideoEditorAIFacade) Retry(ctx context.Context, id, user, request string) (dto.VideoEditorAIJobDTO, error) {
	if !f.tablesReady() {
		return dto.VideoEditorAIJobDTO{}, editorai.ErrUnavailable
	}

	if !veID(id) || !veID(user) {
		return dto.VideoEditorAIJobDTO{}, repo.ErrNotFound
	}
	if !veID(request) {
		return dto.VideoEditorAIJobDTO{}, ErrVEInvalid
	}
	existing, err := f.repo.Get(ctx, id, user)
	if err != nil {
		return dto.VideoEditorAIJobDTO{}, err
	}
	var seen []string
	if json.Unmarshal(existing.RetryRequests, &seen) == nil {
		for _, prior := range seen {
			if prior == request {
				return aiDTO(existing), nil
			}
		}
	}
	if !f.Capabilities().Available {
		return dto.VideoEditorAIJobDTO{}, editorai.ErrUnavailable
	}
	j, e := f.repo.Retry(ctx, id, user, request)
	if e != nil {
		return dto.VideoEditorAIJobDTO{}, e
	}
	return aiDTO(j), nil
}
