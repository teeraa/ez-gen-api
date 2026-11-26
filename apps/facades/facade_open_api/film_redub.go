package facade_open_api

// Film re-dubbing: a shot clip keeps the provider's undubbed video (RawKey) next to the dubbed one
// (ClipKey). When a character's voice changes, every finished clip in which that character speaks
// is mixed again from its raw clip with the new voice: no new video and only the voice price.
// Clips made before raw clips were kept ("legacy") cannot be re-dubbed; the Web flags them.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

// filmNoVoices is the voice hash of a clip with no dubbed lines.
const filmNoVoices = "none"

// filmRedub is a running or failed re-dub of a finished clip.
type filmRedub struct {
	Status         string  `json:"status"`
	JobID          string  `json:"jobId"`
	VoiceHash      string  `json:"voiceHash"`
	PointsCharged  int     `json:"pointsCharged,omitempty"`
	PointsRefunded int     `json:"pointsRefunded,omitempty"`
	FailureMessage *string `json:"failureMessage,omitempty"`
}

// filmRedubJob is the frozen input of a "shot_redub" job.
type filmRedubJob struct {
	ShotID     string          `json:"shotId"`
	RawKey     string          `json:"rawKey"`
	VoiceHash  string          `json:"voiceHash"`
	Voices     []filmVoiceLine `json:"voices"`
	Seconds    int             `json:"seconds"`
	Ratio      string          `json:"ratio"`
	Resolution string          `json:"resolution"`
	Draft      bool            `json:"draft,omitempty"`
	QueuedAt   time.Time       `json:"queuedAt"`
}

// filmLinesHash identifies the voices dubbed into a clip.
func filmLinesHash(lines []filmVoiceLine) string {
	if len(lines) == 0 {
		return filmNoVoices
	}
	return storyHash(lines)
}

// filmVideoFresh tells whether a finished clip still matches its shot (voices aside). Clips made
// while voices were part of the shot hash may carry that combined hash.
func filmVideoFresh(v *filmVideo, sh *filmShot, byID map[string]*models.FilmElement, style string) bool {
	base := filmShotHash(sh, byID, style)
	return v.Hash == base || v.VoiceHash == "" && v.Hash == filmVoiceHash(base, filmVoiceLines(sh, byID))
}

// filmVoiceOverlap tells whether a clip's dubbed lines play over the provider's own audio (clips
// made before dialogue shots were rendered silent). Only a new video fixes it.
func filmVoiceOverlap(v *filmVideo, sh *filmShot, byID map[string]*models.FilmElement, style string) bool {
	// Silent clips have no provider speech; current mixes mute the clip's own audio under the lines.
	if v.Silent || v.Mix >= filmMixMuted {
		return false
	}
	if v.VoiceHash != "" {
		return v.VoiceHash != filmNoVoices
	}
	lines := filmVoiceLines(sh, byID)
	base := filmShotHash(sh, byID, style)
	return len(lines) > 0 && v.Hash != base && v.Hash == filmVoiceHash(base, lines)
}

// filmDraftDubbable tells whether a draft can become a full clip without its dubbed lines playing
// over provider speech: the full clip inherits the draft's audio.
func filmDraftDubbable(draft filmDraft, sh *filmShot, byID map[string]*models.FilmElement) bool {
	// Drafts that kept their lines are mixed over a muted clip, like every current mix.
	return draft.Native || draft.Silent || len(draft.Lines) > 0 || len(filmVoiceLines(sh, byID)) == 0
}

func filmRedubActive(r *filmRedub) bool {
	return r != nil && (r.Status == "queued" || r.Status == "processing")
}

// filmVoiceStatus is the state of a finished clip's dubbed dialogue (see dto.FilmShotVideo.Voice).
func filmVoiceStatus(v *filmVideo, sh *filmShot, byID map[string]*models.FilmElement, style string) string {
	if v == nil || v.Status != "succeeded" {
		return ""
	}
	if filmVoiceOverlap(v, sh, byID, style) {
		return "overlap"
	}
	if filmRedubActive(v.Redub) {
		return "updating"
	}
	lines := filmVoiceLines(sh, byID)
	want := filmLinesHash(lines)
	if v.VoiceHash == "" {
		// Legacy clip: current only if it was dubbed with exactly these voices (or none are needed).
		if len(lines) == 0 || v.Hash == filmVoiceHash(filmShotHash(sh, byID, style), lines) {
			return "current"
		}
		return "legacy"
	}
	if v.VoiceHash == want {
		return "current"
	}
	// The provider voiced these lines itself (nobody had a voice yet): dubbing over it would double them.
	if v.RawKey == "" || v.VoiceHash == filmNoVoices {
		return "legacy"
	}
	if v.Redub != nil && v.Redub.Status == "failed" && v.Redub.VoiceHash == want {
		return "failed"
	}
	return "outdated"
}

func (f *FilmFacade) redubAvailable() bool {
	return f.voices != nil && f.options.Processor != nil && f.options.VideoFetch != nil && f.storage != nil
}

// RequestRedub dubs the current voices into every finished clip whose voices are out of date.
// Clips whose video is stale are skipped: they need a new video anyway.
func (f *FilmFacade) RequestRedub(c context.Context, projectID, user string, in req.FilmRedubRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if !f.redubAvailable() {
		return dto.FilmEpisode{}, &DirectorError{Status: 503, Code: "VOICE_UNAVAILABLE", Message: "ระบบเสียงพากย์ยังไม่พร้อมใช้งาน"}
	}
	hash := storyHash(struct {
		Action, Project string
		Input           req.FilmRedubRequest
	}{"film-redub", projectID, in})
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, s *models.DirectorSeries, elements []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		byID := filmElementMap(elements)
		project := ep.ProjectID
		jobs, changed := []models.FilmJob{}, false
		for i := range d.Shots {
			sh := &d.Shots[i]
			v := sh.Video
			if v == nil || v.Status != "succeeded" || !filmVideoFresh(v, sh, byID, s.Style) {
				continue
			}
			if st := filmVoiceStatus(v, sh, byID, s.Style); st != "outdated" && st != "failed" {
				continue
			}
			lines := filmVoiceLines(sh, byID)
			want := filmLinesHash(lines)
			if len(lines) == 0 {
				// Nothing to dub any more: the raw clip is the clip.
				v.ClipKey, v.VoiceHash, v.Redub = v.RawKey, want, nil
				changed = true
				continue
			}
			raw, e := json.Marshal(filmRedubJob{ShotID: sh.ID, RawKey: v.RawKey, VoiceHash: want, Voices: lines, Seconds: sh.Seconds, Ratio: s.AspectRatio, Resolution: s.Resolution, Draft: v.Draft, QueuedAt: time.Now().UTC()})
			if e != nil {
				return nil, e
			}
			jobID, cost := uuid.New(), filmVoicePoints(lines)
			v.Redub = &filmRedub{Status: "queued", JobID: jobID.String(), VoiceHash: want, PointsCharged: cost}
			jobs = append(jobs, models.FilmJob{ID: jobID, SeriesID: s.ID, ProjectID: &project, Kind: "shot_redub", TargetID: "redub:" + sh.ID, Snapshot: raw, Cost: cost})
		}
		if len(jobs) == 0 && !changed {
			return nil, &DirectorError{Status: 409, Code: "NOTHING_TO_REDUB", Message: "เสียงของทุกคลิปเป็นปัจจุบันแล้ว"}
		}
		return jobs, filmEncode(ep, &d)
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

// filmOwnsRedub tells whether a re-dub job still applies: same job, same raw clip.
func filmOwnsRedub(sh *filmShot, jobID, rawKey string) bool {
	return sh != nil && sh.Video != nil && sh.Video.Status == "succeeded" && sh.Video.Redub != nil && sh.Video.Redub.JobID == jobID && sh.Video.RawKey == rawKey
}

func filmUpdateRedub(ep *models.FilmEpisode, in *filmRedubJob, jobID string, fn func(*filmVideo)) error {
	d, e := filmDecode(ep)
	if e != nil {
		return e
	}
	_, sh := filmFindShot(&d, in.ShotID)
	if !filmOwnsRedub(sh, jobID, in.RawKey) {
		return nil
	}
	fn(sh.Video)
	return filmEncode(ep, &d)
}

func (f *FilmFacade) redubFail(c context.Context, j *models.FilmJob, in *filmRedubJob, message string, cause error) error {
	if cause != nil {
		log.Printf("film: job=%s shot=%s redub failed: %v", j.ID, in.ShotID, cause)
	}
	return f.store.CompleteEpisode(c, j, "failed", true, func(ep *models.FilmEpisode) error {
		return filmUpdateRedub(ep, in, j.ID.String(), func(v *filmVideo) {
			msg := message
			v.Redub.Status, v.Redub.FailureMessage, v.Redub.PointsRefunded = "failed", &msg, j.Cost
		})
	})
}

// redubRetry waits and tries again while a transient failure is young enough.
func (f *FilmFacade) redubRetry(c context.Context, j *models.FilmJob, in *filmRedubJob, cause error) error {
	if time.Since(in.QueuedAt) > filmDubRetryWindow {
		return f.redubFail(c, j, in, "สร้างเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่", cause)
	}
	log.Printf("film: job=%s shot=%s redub retry: %v", j.ID, in.ShotID, cause)
	return f.store.Wait(c, j, j.Output, time.Now().Add(filmVideoRetry))
}

func (f *FilmFacade) redubJob(c context.Context, j *models.FilmJob) error {
	var in filmRedubJob
	if e := json.Unmarshal(j.Snapshot, &in); e != nil || j.ProjectID == nil {
		return f.store.CompleteEpisode(c, j, "failed", true, func(*models.FilmEpisode) error { return nil })
	}
	_, _, ep, e := f.store.Episode(c, j.ProjectID.String(), j.UserID.String())
	if errors.Is(e, repo.ErrNotFound) {
		return f.store.CompleteEpisode(c, j, "cancelled", true, func(*models.FilmEpisode) error { return nil })
	}
	if e != nil {
		return e
	}
	d, e := filmDecode(ep)
	if e != nil {
		return e
	}
	// A new video for the shot, or the shot removed, retires this job.
	if _, sh := filmFindShot(&d, in.ShotID); !filmOwnsRedub(sh, j.ID.String(), in.RawKey) {
		return f.store.CompleteEpisode(c, j, "cancelled", true, func(*models.FilmEpisode) error { return nil })
	}
	if !f.redubAvailable() {
		return f.redubFail(c, j, &in, "ระบบเสียงพากย์ยังไม่พร้อมใช้งาน", nil)
	}
	if e = f.store.ProgressEpisode(c, j, func(ep *models.FilmEpisode) error {
		return filmUpdateRedub(ep, &in, j.ID.String(), func(v *filmVideo) { v.Redub.Status = "processing" })
	}); e != nil {
		return e
	}
	u, _, e := f.storage.SignedURL(c, in.RawKey, 30*time.Minute)
	var clip []byte
	if e == nil {
		clip, _, e = f.options.VideoFetch(c, u)
	}
	if e != nil || len(clip) == 0 {
		return f.redubRetry(c, j, &in, fmt.Errorf("raw clip: %v", e))
	}
	dubbed, lines, e := f.dubClip(c, j.UserID.String(), &filmVideoJob{ShotID: in.ShotID, Seconds: in.Seconds, Ratio: in.Ratio, Resolution: in.Resolution, Draft: in.Draft, Voices: in.Voices}, clip)
	if errors.Is(e, errFilmDubRetry) {
		return f.redubRetry(c, j, &in, e)
	}
	if e != nil {
		return f.redubFail(c, j, &in, "สร้างเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่ หรือเปลี่ยนเสียงของตัวละคร", e)
	}
	key := fmt.Sprintf("film/%s/%s/%s/%s-dub.mp4", j.UserID, *j.ProjectID, in.ShotID, j.ID)
	stored, e := f.filmStoreLines(c, strings.TrimSuffix(key, ".mp4"), lines)
	if e == nil {
		e = f.storage.Upload(c, key, dubbed, "video/mp4")
	}
	if e != nil {
		return f.redubRetry(c, j, &in, e)
	}
	return f.store.CompleteEpisode(c, j, "succeeded", false, func(ep *models.FilmEpisode) error {
		d, e := filmDecode(ep)
		if e != nil {
			return e
		}
		_, sh := filmFindShot(&d, in.ShotID)
		if !filmOwnsRedub(sh, j.ID.String(), in.RawKey) {
			return nil
		}
		// New voices keep moved line positions when the lines still match one to one.
		if len(sh.LineAt) != len(stored) {
			sh.LineAt = nil
		}
		sh.Video.ClipKey, sh.Video.VoiceHash, sh.Video.Redub, sh.Video.Lines, sh.Video.Mix = key, in.VoiceHash, nil, stored, filmMixVersion
		return filmEncode(ep, &d)
	})
}
