package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
)

type directorExtensionState struct {
	Phase          string                     `json:"extensionPhase"`
	SourceClipKey  string                     `json:"extensionSourceClipKey"`
	SourceFrameKey string                     `json:"extensionSourceFrameKey"`
	TailKey        string                     `json:"extensionTailKey"`
	Attempts       int                        `json:"extensionAttempts"`
	Request        modelark.CreateTaskRequest `json:"extensionRequest"`
}

func (f *DirectorFacade) extensionCheckpoint(c context.Context, j *models.DirectorJob, state directorExtensionState) error {
	raw, e := json.Marshal(state)
	if e != nil {
		return e
	}
	if e = f.store.RecordOutput(c, j, raw); e != nil {
		return e
	}
	j.Output = raw
	return nil
}
func directorExtensionSourceCurrent(d *directorDocument, in directorJobInput, state directorExtensionState) bool {
	sh := directorFindShot(&d.Project, in.ShotID)
	if sh == nil {
		return false
	}
	prev := directorPreviousShot(&d.Project, sh)
	if prev == nil || prev.Video == nil || prev.Video.Status != "succeeded" || prev.Video.Stale {
		return false
	}
	frame := d.VideoFrames[prev.ID]
	return d.Keys[directorVideoTarget(prev.ID)] == state.SourceClipKey && frame.ClipKey == state.SourceClipKey && frame.Key == state.SourceFrameKey
}
func (f *DirectorFacade) directorVideoExtension(c context.Context, j *models.DirectorJob, in directorJobInput, state directorExtensionState) error {
	if state.Phase == "submitting" {
		return f.failWith(c, j, in, false, "VIDEO_EXTENSION_SUBMISSION_UNCERTAIN", "Cannot confirm video submission; contact support before retrying", nil)
	}
	if !f.videoAvailable() {
		return f.store.Wait(c, j, j.Output, time.Now().UTC().Add(time.Minute))
	}
	processor, ok := f.options.Processor.(videoprocessing.VideoContinuationProcessor)
	if !ok {
		return f.failWith(c, j, in, true, "VIDEO_EXTENSION_UNAVAILABLE", "Video continuation processing is unavailable", nil)
	}
	row, e := f.store.Get(c, j.ProjectID.String(), j.UserID.String())
	if e != nil {
		return e
	}
	doc, e := directorDecode(row)
	if e != nil {
		return e
	}
	if state.Phase == "" {
		sh := directorFindShot(&doc.Project, in.ShotID)
		if sh == nil {
			return f.fail(c, j, in, true)
		}
		prev := directorPreviousShot(&doc.Project, sh)
		if prev == nil {
			return f.fail(c, j, in, true)
		}
		if directorVideoBusy(prev) {
			return f.store.Wait(c, j, nil, time.Now().UTC().Add(directorVideoPoll))
		}
		frame := doc.VideoFrames[prev.ID]
		state = directorExtensionState{Phase: "preparing", SourceClipKey: doc.Keys[directorVideoTarget(prev.ID)], SourceFrameKey: frame.Key, TailKey: fmt.Sprintf("director/%s/%s/%s/%s-tail.mp4", j.UserID, j.ProjectID, in.ShotID, j.ID)}
		if !directorExtensionSourceCurrent(&doc, in, state) {
			return f.failWith(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "Previous video must finish before continuation", nil)
		}
		cast := directorFeaturedCast(directorShotCharacters(in.Characters, sh.CharacterAssetIDs), directorShotAction(&doc, sh))
		ratio := in.Ratio
		if in.VideoModel == "dreamina-seedance-2-5-260628" {
			ratio = "adaptive"
		}
		resolution := in.Resolution
		if in.Draft {
			resolution = "480p"
		}
		state.Request = modelark.CreateTaskRequest{Model: in.VideoModel, Content: []modelark.ContentItem{{Type: modelark.ContentTypeText, Text: directorExtensionPrompt(&doc, &doc.Project, sh, cast, in.Seconds)}, {Type: modelark.ContentTypeVideoURL, URL: state.TailKey, Role: modelark.RoleReferenceVideo}}, Ratio: ratio, DurationSeconds: int64(in.Seconds), Resolution: resolution, Draft: in.Draft, ReturnLastFrame: true}
		if err := promptcompiler.ValidateVideoPrompt(state.Request.Content[0].Text); err != nil {
			return f.failWith(c, j, in, true, "VIDEO_PROMPT_TOO_LONG", "รายละเอียดวิดีโอรวมยาวเกินขีดจำกัด กรุณาย่อข้อความแล้วลองใหม่", nil)
		}
		if e = f.extensionCheckpoint(c, j, state); e != nil {
			return e
		}
	}
	expected := fmt.Sprintf("director/%s/%s/%s/%s-tail.mp4", j.UserID, j.ProjectID, in.ShotID, j.ID)
	if state.TailKey != expected || state.SourceClipKey == "" || len(state.Request.Content) != 2 || state.Request.Content[1].Type != modelark.ContentTypeVideoURL || state.Request.Content[1].Role != modelark.RoleReferenceVideo {
		return f.fail(c, j, in, false)
	}
	if !directorExtensionSourceCurrent(&doc, in, state) {
		return f.failWith(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "Previous video changed; retry after it completes", nil)
	}
	if state.Phase == "preparing" {
		if state.Attempts >= 3 {
			return f.failWith(c, j, in, true, "VIDEO_EXTENSION_PREPARATION_FAILED", "Unable to prepare the previous video tail", nil)
		}
		state.Attempts++
		if e = f.extensionCheckpoint(c, j, state); e != nil {
			return e
		}
		retry := func() error {
			if state.Attempts >= 3 {
				return f.failWith(c, j, in, true, "VIDEO_EXTENSION_PREPARATION_FAILED", "Unable to prepare the previous video tail", nil)
			}
			return f.store.Wait(c, j, j.Output, time.Now().UTC().Add(directorVideoRetry))
		}
		u, _, err := f.storage.SignedURL(c, state.SourceClipKey, time.Hour)
		if err != nil {
			return retry()
		}
		if f.options.VideoFetch == nil {
			return retry()
		}
		data, kind, err := f.options.VideoFetch(c, u)
		if err != nil || kind != "video/mp4" && kind != "video/quicktime" || len(data) == 0 || len(data) > 300<<20 {
			return retry()
		}
		tail, err := processor.PrepareContinuation(c, data)
		if err != nil {
			return retry()
		}
		if e = f.storage.Upload(c, state.TailKey, tail, "video/mp4"); e != nil {
			return retry()
		}
		state.Phase = "ready"
		if e = f.extensionCheckpoint(c, j, state); e != nil {
			return e
		}
	}
	if state.Phase != "ready" {
		return f.fail(c, j, in, false)
	}
	u, _, e := f.storage.SignedURL(c, state.TailKey, time.Hour)
	if e != nil {
		return f.store.Wait(c, j, j.Output, time.Now().UTC().Add(directorVideoRetry))
	}
	changed := errors.New("extension source changed")
	if e = f.progress(c, j, in, func(d *directorDocument) error {
		if !directorExtensionSourceCurrent(d, in, state) {
			return changed
		}
		sh := directorFindShot(&d.Project, in.ShotID)
		sh.Video.Status = "processing"
		sh.Video.Progress = 5
		return nil
	}); e != nil {
		if errors.Is(e, changed) {
			return f.failWith(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "Previous video changed before continuation", nil)
		}
		return e
	}
	request := state.Request
	request.Content = append([]modelark.ContentItem(nil), state.Request.Content...)
	request.Content[1].URL = u
	state.Phase = "submitting"
	if e = f.extensionCheckpoint(c, j, state); e != nil {
		return e
	}
	id, e := f.video.CreateTask(c, request)
	if e != nil {
		diag := modelark.VideoFailureDiagnostic(e)
		definitive := diag.HTTPStatus >= 400 && diag.HTTPStatus < 500 || errors.Is(e, modelark.ErrNotSubmitted)
		code, message := modelark.VideoFailure(e)
		return f.failWith(c, j, in, definitive, code, message, fmt.Errorf("extension submit http=%d provider=%s param=%s model=%s ratio=%s resolution=%s draft=%t: %w", diag.HTTPStatus, diag.ProviderCode, diag.Param, request.Model, request.Ratio, request.Resolution, request.Draft, e))
	}
	if id == "" {
		return f.fail(c, j, in, false)
	}
	raw, e := json.Marshal(directorVideoTask{TaskID: id, SubmittedAt: time.Now().UTC(), SourceClipKey: state.SourceClipKey, FirstImageKey: state.SourceFrameKey, Extension: true})
	if e != nil {
		return e
	}
	if e = f.store.RecordOutput(c, j, raw); e != nil {
		return e
	}
	j.Output = raw
	return f.store.Wait(c, j, raw, time.Now().UTC().Add(directorVideoPoll))
}
