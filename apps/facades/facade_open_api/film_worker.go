package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
)

const (
	filmVideoPoll        = 10 * time.Second
	filmVideoRetry       = 30 * time.Second
	filmVideoDeadline    = 2 * time.Hour
	filmVideoFetchWindow = 20 * time.Hour
	// A "continue" shot waits this long for the previous shot's clip before it gives up.
	filmDependencyWait = 3 * time.Hour
)

// Tick runs one film job. It returns repo.ErrNotFound when nothing is runnable.
func (f *FilmFacade) Tick(ctx context.Context) error {
	j, e := f.store.Claim(ctx)
	if e != nil {
		return e
	}
	log.Printf("film: job=%s kind=%s claimed", j.ID, j.Kind)
	c, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch j.Kind {
	case "element_image":
		return f.elementJob(c, j)
	case "shot_video":
		return f.videoJob(c, j)
	case "plan":
		return f.planJob(c, j)
	case "export":
		return f.exportJob(c, j)
	case "shot_redub":
		return f.redubJob(c, j)
	}
	return f.store.CompleteEpisode(c, j, "failed", true, func(*models.FilmEpisode) error { return nil })
}

// ---------- Element images ----------

type filmElementState struct {
	ImageKey string `json:"imageKey,omitempty"`
}

func filmImageExt(data []byte) (string, string) {
	kind := http.DetectContentType(data)
	if kind == "image/jpeg" {
		return ".jpg", kind
	}
	return ".png", "image/png"
}

func (f *FilmFacade) elementFail(c context.Context, j *models.FilmJob, elementID, code, message string, cause error) error {
	if cause != nil {
		log.Printf("film: job=%s element=%s failed code=%s: %v", j.ID, elementID, code, cause)
	}
	return f.store.CompleteElement(c, j, elementID, "failed", true, func(el *models.FilmElement) error {
		el.ImageStatus, el.FailureCode, el.FailureMessage = "failed", &code, &message
		return nil
	})
}

func (f *FilmFacade) generate(c context.Context, request modelark.ImageRequest) ([]byte, string, string, error) {
	data, e := f.images.GenerateImage(c, request)
	if e != nil {
		code, message := "IMAGE_GENERATION_FAILED", "สร้างภาพไม่สำเร็จ กรุณาลองใหม่"
		if known, _ := storyDefinitiveImageError(e); known != nil && known.Message != "" {
			code, message = known.Code, known.Message
		}
		return nil, code, message, e
	}
	if !storyValidImage(data) {
		return nil, "IMAGE_INVALID", "ได้ภาพที่ใช้งานไม่ได้ กรุณาลองใหม่", errors.New("invalid image data")
	}
	return data, "", "", nil
}

func (f *FilmFacade) elementJob(c context.Context, j *models.FilmJob) error {
	var in filmElementJob
	if e := json.Unmarshal(j.Snapshot, &in); e != nil {
		return f.elementFail(c, j, "", "INVALID_JOB", "ข้อมูลงานไม่ถูกต้อง", e)
	}
	if _, e := f.store.Element(c, in.ElementID, j.UserID.String()); errors.Is(e, repo.ErrNotFound) {
		return f.store.CompleteElement(c, j, in.ElementID, "cancelled", true, func(*models.FilmElement) error { return nil })
	} else if e != nil {
		return e
	}
	if f.images == nil || f.storage == nil {
		return f.elementFail(c, j, in.ElementID, "IMAGE_UNAVAILABLE", "ระบบสร้างภาพยังไม่พร้อมใช้งาน", nil)
	}
	if e := f.store.MarkElementProcessing(c, j, in.ElementID); e != nil {
		return e
	}
	var state filmElementState
	if len(j.Output) > 0 {
		_ = json.Unmarshal(j.Output, &state)
	}
	base := fmt.Sprintf("film/%s/%s/elements/%s/%s", j.UserID, j.SeriesID, in.ElementID, j.ID)
	source := in.SourceKey
	if in.Mode == "generate" {
		if state.ImageKey == "" {
			size := ImageSize("3:4")
			if in.Kind == "location" {
				size = directorImageSize(in.Ratio)
			}
			data, code, message, e := f.generate(c, modelark.ImageRequest{Model: in.ImageModel, Prompt: filmElementPrompt(in), Size: size})
			if e != nil {
				return f.elementFail(c, j, in.ElementID, code, message, e)
			}
			ext, kind := filmImageExt(data)
			key := base + ext
			if e = f.storage.Upload(c, key, data, kind); e != nil {
				return f.store.Wait(c, j, j.Output, time.Now().Add(filmVideoRetry))
			}
			state.ImageKey = key
			raw, _ := json.Marshal(state)
			if e = f.store.RecordOutput(c, j, raw); e != nil {
				return e
			}
			j.Output = raw
		}
		source = state.ImageKey
	}
	synthetic := ""
	if in.Filmed && in.Kind == "character" {
		u, _, e := f.storage.SignedURL(c, source, time.Hour)
		if e == nil && f.options.ReferenceCheck != nil {
			e = f.options.ReferenceCheck(c, u)
		}
		if e != nil {
			return f.elementFail(c, j, in.ElementID, "REFERENCE_UNAVAILABLE", "เปิดภาพตัวละครไม่ได้ กรุณาลองใหม่", e)
		}
		data, code, message, e := f.generate(c, modelark.ImageRequest{Model: in.ImageModel, Prompt: filmCastingPrompt(in), Size: ImageSize("3:4"), ReferenceURLs: []string{u}})
		if e != nil {
			return f.elementFail(c, j, in.ElementID, code, message, e)
		}
		ext, kind := filmImageExt(data)
		key := base + "-casting" + ext
		if e = f.storage.Upload(c, key, data, kind); e != nil {
			return f.store.Wait(c, j, j.Output, time.Now().Add(filmVideoRetry))
		}
		synthetic = key
	}
	return f.store.CompleteElement(c, j, in.ElementID, "succeeded", false, func(el *models.FilmElement) error {
		if in.Mode == "generate" {
			key := source
			el.ImageKey = &key
			el.SyntheticKey = nil
		}
		if synthetic != "" {
			el.SyntheticKey = &synthetic
		}
		el.ImageStatus, el.FailureCode, el.FailureMessage = "succeeded", nil, nil
		return nil
	})
}

// ---------- Shot videos ----------

type filmVideoTask struct {
	// Submitting is recorded before the paid request; an expired lease with it set and no task id
	// means the outcome is unknown, so the request is never sent twice.
	Submitting    bool      `json:"submitting,omitempty"`
	TaskID        string    `json:"taskId,omitempty"`
	SubmittedAt   time.Time `json:"submittedAt,omitempty"`
	Mode          string    `json:"mode,omitempty"`
	FirstFrameKey string    `json:"firstFrameKey,omitempty"`
	// Fallback is set when a realistic last frame was rejected and the shot is re-sent from references.
	Fallback      bool   `json:"fallback,omitempty"`
	OwnedVideoKey string `json:"ownedVideoKey,omitempty"`
	// OwnedRawKey is the clip before dubbing, kept so voices can change later without a new video.
	OwnedRawKey string `json:"ownedRawKey,omitempty"`
	// OwnedLines are the generated dialogue lines stored next to the clip.
	OwnedLines []filmDubLine `json:"ownedLines,omitempty"`
}

func (f *FilmFacade) videoFail(c context.Context, j *models.FilmJob, in *filmVideoJob, refund bool, code, message string, cause error) error {
	if cause != nil {
		log.Printf("film: job=%s shot=%s failed code=%s refund=%t: %v", j.ID, in.ShotID, code, refund, cause)
	}
	if message == "" {
		message = "สร้างวิดีโอไม่สำเร็จ กรุณาลองใหม่"
	}
	return f.store.CompleteEpisode(c, j, "failed", refund, func(ep *models.FilmEpisode) error {
		return filmUpdateShot(ep, in.ShotID, j.ID.String(), func(v *filmVideo) {
			v.Status, v.Progress, v.FailureCode, v.FailureMessage, v.CanRetry = "failed", 0, &code, &message, true
			if refund {
				v.PointsRefunded = j.Cost
			}
		})
	})
}

// filmUpdateShot changes the video state of a shot only while the job still owns it.
func filmUpdateShot(ep *models.FilmEpisode, shotID, jobID string, fn func(*filmVideo)) error {
	d, e := filmDecode(ep)
	if e != nil {
		return e
	}
	_, sh := filmFindShot(&d, shotID)
	if sh == nil || sh.Video == nil || sh.Video.JobID != jobID {
		return nil
	}
	fn(sh.Video)
	return filmEncode(ep, &d)
}

func (f *FilmFacade) wait(c context.Context, j *models.FilmJob, task *filmVideoTask, after time.Duration) error {
	raw, e := json.Marshal(task)
	if e != nil {
		return e
	}
	return f.store.Wait(c, j, raw, time.Now().Add(after))
}

func (f *FilmFacade) videoJob(c context.Context, j *models.FilmJob) error {
	var in filmVideoJob
	if e := json.Unmarshal(j.Snapshot, &in); e != nil || j.ProjectID == nil {
		return f.store.CompleteEpisode(c, j, "failed", true, func(*models.FilmEpisode) error { return nil })
	}
	var task filmVideoTask
	if len(j.Output) > 0 {
		_ = json.Unmarshal(j.Output, &task)
	}
	_, _, ep, e := f.store.Episode(c, j.ProjectID.String(), j.UserID.String())
	if errors.Is(e, repo.ErrNotFound) {
		return f.store.CompleteEpisode(c, j, "cancelled", task.TaskID == "" && !task.Submitting, func(*models.FilmEpisode) error { return nil })
	}
	if e != nil {
		return e
	}
	d, e := filmDecode(ep)
	if e != nil {
		return e
	}
	i, sh := filmFindShot(&d, in.ShotID)
	if sh == nil || sh.Video == nil || sh.Video.JobID != j.ID.String() {
		return f.store.CompleteEpisode(c, j, "cancelled", task.TaskID == "" && !task.Submitting, func(*models.FilmEpisode) error { return nil })
	}
	if task.OwnedVideoKey != "" {
		return f.storeVideo(c, j, &in, &task, nil, "")
	}
	if task.TaskID == "" {
		if task.Submitting {
			return f.videoFail(c, j, &in, false, "VIDEO_SUBMISSION_UNCERTAIN", "ไม่แน่ใจว่าคำขอสร้างวิดีโอถูกส่งแล้วหรือยัง กรุณาลองสร้างใหม่", nil)
		}
		return f.submitVideo(c, j, &in, &task, &d, i)
	}
	return f.pollVideo(c, j, &in, &task)
}

func (f *FilmFacade) submitVideo(c context.Context, j *models.FilmJob, in *filmVideoJob, task *filmVideoTask, d *filmDocument, index int) error {
	if !f.videoAvailable() {
		return f.videoFail(c, j, in, true, "VIDEO_UNAVAILABLE", "ระบบสร้างวิดีโอยังไม่พร้อมใช้งาน", nil)
	}
	// Dialogue shots ask for no provider audio explicitly (the provider speaks lines otherwise).
	request := modelark.CreateTaskRequest{Model: in.ProviderModel, Ratio: in.Ratio, DurationSeconds: int64(in.Seconds), Resolution: in.Resolution, GenerateAudio: in.Audio, NoAudio: !in.Audio && len(in.Voices) > 0, ReturnLastFrame: true}
	task.Mode = "reference"
	if in.DraftTaskID != "" {
		// Refining a draft: the provider inherits prompt, frames, ratio and duration and rejects resubmission.
		task.Mode = "finalize"
		request = modelark.CreateTaskRequest{Model: in.ProviderModel, Resolution: in.Resolution, ReturnLastFrame: true, Content: []modelark.ContentItem{{Type: modelark.ContentTypeDraftTask, DraftTaskID: in.DraftTaskID}}}
	} else if in.Transition == "continue" && !task.Fallback {
		task.Mode = "continue"
		if index < 1 || d.Shots[index-1].ID != in.PrevShotID {
			return f.videoFail(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "ลำดับช็อตเปลี่ยน กรุณาสร้างวิดีโอช็อตนี้ใหม่", nil)
		}
		prev := d.Shots[index-1].Video
		switch {
		case prev != nil && filmActive(prev):
			if time.Since(in.QueuedAt) > filmDependencyWait {
				return f.videoFail(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "รอวิดีโอช็อตก่อนหน้านานเกินไป กรุณาลองใหม่", nil)
			}
			return f.wait(c, j, task, filmVideoPoll)
		case prev == nil || prev.Status != "succeeded" || prev.FrameKey == "":
			return f.videoFail(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "กรุณาสร้างวิดีโอช็อตก่อนหน้าให้สำเร็จก่อน หรือเปลี่ยนช็อตนี้เป็นแบบตัดภาพ", nil)
		}
		u, _, e := f.storage.SignedURL(c, prev.FrameKey, time.Hour)
		if e != nil {
			return f.videoFail(c, j, in, true, "REFERENCE_UNAVAILABLE", "เปิดเฟรมสุดท้ายของช็อตก่อนหน้าไม่ได้ กรุณาลองใหม่", e)
		}
		task.FirstFrameKey = prev.FrameKey
		request.Content = []modelark.ContentItem{{Type: modelark.ContentTypeText, Text: in.ContinuePrompt}, {Type: modelark.ContentTypeImageURL, URL: u, Role: modelark.RoleFirstFrame}}
		// Seedance 2.5 locks first-frame tasks to the frame's own ratio (same rule as Director).
		if in.ProviderModel == "dreamina-seedance-2-5-260628" {
			request.Ratio = "adaptive"
		}
	} else {
		request.Content = []modelark.ContentItem{{Type: modelark.ContentTypeText, Text: in.ReferencePrompt}}
		for _, ref := range in.Refs {
			u, _, e := f.storage.SignedURL(c, ref.Key, time.Hour)
			if e == nil && f.options.ReferenceCheck != nil {
				e = f.options.ReferenceCheck(c, u)
			}
			if e != nil {
				return f.videoFail(c, j, in, true, "REFERENCE_UNAVAILABLE", "เปิดภาพอ้างอิงของ "+ref.Label+" ไม่ได้ กรุณาลองใหม่", e)
			}
			request.Content = append(request.Content, modelark.ContentItem{Type: modelark.ContentTypeImageURL, URL: u, Role: modelark.RoleReferenceImage})
		}
	}
	if in.Draft {
		request.Draft, request.Resolution = true, "480p"
	}
	if e := f.store.ProgressEpisode(c, j, func(ep *models.FilmEpisode) error {
		return filmUpdateShot(ep, in.ShotID, j.ID.String(), func(v *filmVideo) { v.Status, v.Progress, v.Mode = "processing", 5, task.Mode })
	}); e != nil {
		return e
	}
	task.Submitting = true
	raw, e := json.Marshal(task)
	if e != nil {
		return e
	}
	if e = f.store.RecordOutput(c, j, raw); e != nil {
		return e
	}
	j.Output = raw
	id, e := f.video.CreateTask(c, request)
	if e != nil {
		diag := modelark.VideoFailureDiagnostic(e)
		code, message := "VIDEO_PROVIDER_ERROR", ""
		var ve *modelark.VideoError
		if errors.As(e, &ve) {
			code, message = ve.Code, ve.Message
		}
		definitive := diag.HTTPStatus >= 400 && diag.HTTPStatus < 500 || errors.Is(e, modelark.ErrNotSubmitted)
		if definitive && task.Mode == "continue" && in.Filmed && code == "PROVIDER_IMAGE_REJECTED" {
			return f.fallback(c, j, in, task)
		}
		return f.videoFail(c, j, in, definitive, code, message, fmt.Errorf("film video submit http=%d model=%s mode=%s draft=%t refs=%d: %w", diag.HTTPStatus, in.ProviderModel, task.Mode, in.Draft, len(request.Content)-1, e))
	}
	if id == "" {
		return f.videoFail(c, j, in, false, "VIDEO_PROVIDER_ERROR", "", errors.New("empty task id"))
	}
	task.Submitting, task.TaskID, task.SubmittedAt = false, id, time.Now().UTC()
	return f.wait(c, j, task, filmVideoPoll)
}

// fallback re-sends a realistic continuation from references after the provider rejected the
// previous clip's last frame (it contains a realistic face). It runs once per job.
func (f *FilmFacade) fallback(c context.Context, j *models.FilmJob, in *filmVideoJob, task *filmVideoTask) error {
	if task.Fallback {
		return f.videoFail(c, j, in, true, "PROVIDER_IMAGE_REJECTED", "บริการปฏิเสธภาพอ้างอิงของช็อตนี้", nil)
	}
	next := filmVideoTask{Fallback: true}
	return f.wait(c, j, &next, time.Second)
}

func (f *FilmFacade) pollVideo(c context.Context, j *models.FilmJob, in *filmVideoJob, task *filmVideoTask) error {
	if f.video == nil || f.fetcher == nil {
		return f.wait(c, j, task, time.Minute)
	}
	age := time.Since(task.SubmittedAt)
	result, e := f.video.GetTask(c, task.TaskID)
	if e != nil || result == nil {
		if age > filmVideoDeadline {
			return f.videoFail(c, j, in, false, "VIDEO_TIMEOUT", "สร้างวิดีโอนานเกินกำหนด", e)
		}
		return f.wait(c, j, task, filmVideoRetry)
	}
	switch result.Status {
	case modelark.ProviderStatusQueued, modelark.ProviderStatusRunning:
		if age > filmVideoDeadline {
			return f.videoFail(c, j, in, false, "VIDEO_TIMEOUT", "สร้างวิดีโอนานเกินกำหนด", nil)
		}
		progress := min(90, 10+int(age.Seconds()*80/120))
		if e = f.store.ProgressEpisode(c, j, func(ep *models.FilmEpisode) error {
			return filmUpdateShot(ep, in.ShotID, j.ID.String(), func(v *filmVideo) { v.Status, v.Progress = "processing", progress })
		}); e != nil {
			return e
		}
		return f.wait(c, j, task, filmVideoPoll)
	case modelark.ProviderStatusFailed, modelark.ProviderStatusCancelled:
		if task.Mode == "continue" && in.Filmed && result.ErrorCode == "PROVIDER_IMAGE_REJECTED" {
			return f.fallback(c, j, in, task)
		}
		code, message := "VIDEO_GENERATION_FAILED", "บริการสร้างวิดีโอไม่สำเร็จหรือปฏิเสธเนื้อหาของช็อตนี้ กรุณาปรับคำบรรยายหรือภาพแล้วลองใหม่"
		switch result.ErrorCode {
		case "PROVIDER_OUTPUT_REJECTED", "PROVIDER_IMAGE_REJECTED", "PROVIDER_TEXT_REJECTED":
			code, message = result.ErrorCode, result.ErrorMessage
		}
		return f.videoFail(c, j, in, true, code, message, fmt.Errorf("film video task status=%s code=%s", result.Status, result.ErrorCode))
	case modelark.ProviderStatusSucceeded:
		data, kind, e := f.fetcher.Fetch(c, result.VideoURL)
		if e != nil {
			if age > filmVideoFetchWindow {
				return f.videoFail(c, j, in, false, "VIDEO_DOWNLOAD_FAILED", "ดาวน์โหลดวิดีโอไม่สำเร็จ", e)
			}
			return f.wait(c, j, task, filmVideoRetry)
		}
		if !strings.HasPrefix(kind, "video/") || len(data) == 0 {
			return f.videoFail(c, j, in, false, "VIDEO_DOWNLOAD_FAILED", "ได้ไฟล์วิดีโอที่ใช้งานไม่ได้", nil)
		}
		// Dialogue of characters with a bound voice is dubbed into the clip before it is stored; the
		// provider's clip is kept as well, so a voice can change later without a new video.
		undubbed := data
		var spoken []filmDubbed
		if dubbed, lines, e := f.dubClip(c, j.UserID.String(), in, data); e != nil {
			if errors.Is(e, errFilmDubRetry) && age <= filmDubRetryWindow {
				log.Printf("film: job=%s shot=%s dubbing retry: %v", j.ID, in.ShotID, e)
				return f.wait(c, j, task, filmVideoRetry)
			}
			return f.videoFail(c, j, in, true, "VOICE_FAILED", "สร้างเสียงพากย์ของตัวละครไม่สำเร็จ กรุณาลองใหม่ หรือเปลี่ยนเสียงของตัวละคร", e)
		} else {
			data, spoken = dubbed, lines
		}
		key := fmt.Sprintf("film/%s/%s/%s/%s.mp4", j.UserID, *j.ProjectID, in.ShotID, j.ID)
		stored, e := f.filmStoreLines(c, strings.TrimSuffix(key, ".mp4"), spoken)
		if e != nil {
			return f.wait(c, j, task, filmVideoRetry)
		}
		task.OwnedLines = stored
		rawKey := key
		if len(in.Voices) > 0 {
			rawKey = strings.TrimSuffix(key, ".mp4") + "-raw.mp4"
			if e = f.storage.Upload(c, rawKey, undubbed, "video/mp4"); e != nil {
				return f.wait(c, j, task, filmVideoRetry)
			}
		}
		if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
			return f.wait(c, j, task, filmVideoRetry)
		}
		task.OwnedVideoKey, task.OwnedRawKey = key, rawKey
		raw, e := json.Marshal(task)
		if e != nil {
			return e
		}
		if e = f.store.RecordOutput(c, j, raw); e != nil {
			return e
		}
		j.Output = raw
		return f.storeVideo(c, j, in, task, data, result.LastFrameURL)
	}
	return f.videoFail(c, j, in, false, "VIDEO_PROVIDER_ERROR", "", fmt.Errorf("unknown status %s", result.Status))
}

// storeVideo saves the clip's last frame (used by the next "continue" shot) and completes the job.
func (f *FilmFacade) storeVideo(c context.Context, j *models.FilmJob, in *filmVideoJob, task *filmVideoTask, data []byte, frameURL string) error {
	var frame []byte
	if frameURL != "" && f.fetcher != nil {
		frame, _, _ = f.fetcher.Fetch(c, frameURL)
	}
	if !storyValidImage(frame) {
		extractor, ok := f.options.Processor.(videoprocessing.LastFrameExtractor)
		if ok && len(data) == 0 && f.fetcher != nil {
			if u, _, e := f.storage.SignedURL(c, task.OwnedVideoKey, time.Hour); e == nil {
				data, _, _ = f.fetcher.Fetch(c, u)
			}
		}
		if ok && len(data) > 0 {
			frame, _ = extractor.ExtractLastFrame(c, data)
		}
	}
	frameKey := ""
	if storyValidImage(frame) {
		ext, kind := filmImageExt(frame)
		frameKey = strings.TrimSuffix(task.OwnedVideoKey, ".mp4") + "-last" + ext
		if e := f.storage.Upload(c, frameKey, frame, kind); e != nil {
			frameKey = ""
		}
	}
	// A clip without a stored last frame is still usable; only a following "continue" shot needs it.
	return f.store.CompleteEpisode(c, j, "succeeded", false, func(ep *models.FilmEpisode) error {
		d, e := filmDecode(ep)
		if e != nil {
			return e
		}
		_, sh := filmFindShot(&d, in.ShotID)
		if sh == nil || sh.Video == nil || sh.Video.JobID != j.ID.String() {
			return nil
		}
		v := sh.Video
		v.Status, v.Progress, v.ClipKey, v.FrameKey, v.Mode, v.Draft = "succeeded", 100, task.OwnedVideoKey, frameKey, task.Mode, in.Draft
		if task.Mode == "continue" {
			v.SourceFrameKey = task.FirstFrameKey
		}
		v.FailureCode, v.FailureMessage, v.CanRetry = nil, nil, false
		v.RawKey, v.VoiceHash, v.Redub, v.Silent, v.Lines, v.Mix = task.OwnedRawKey, filmLinesHash(in.Voices), nil, !in.Audio, task.OwnedLines, filmMixVersion
		// Moved line positions belonged to the previous clip's lines.
		sh.LineAt = nil
		if v.RawKey == "" && len(in.Voices) == 0 {
			// A job queued before raw clips were kept: an undubbed clip is its own raw clip.
			v.RawKey = task.OwnedVideoKey
		}
		if v.RawKey == "" {
			v.VoiceHash = ""
		}
		// Only the newest draft can be finalized; a full clip retires it.
		if in.Draft && task.TaskID != "" {
			if d.Drafts == nil {
				d.Drafts = map[string]filmDraft{}
			}
			d.Drafts[in.ShotID] = filmDraft{TaskID: task.TaskID, Provider: in.ProviderModel, ModelID: in.ModelID, Hash: in.Hash, Seconds: in.Seconds, CreatedAt: task.SubmittedAt, SourceFrameKey: v.SourceFrameKey, Silent: !in.Audio}
		} else {
			delete(d.Drafts, in.ShotID)
		}
		return filmEncode(ep, &d)
	})
}
