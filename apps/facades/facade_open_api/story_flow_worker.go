package facade_open_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func storyDecodeStrict(text string, out any) error {
	if len(text) > 512<<10 {
		return errors.New("analysis output too large")
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	var extra json.RawMessage
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("trailing analysis output")
	}
	return nil
}
func storyValidContinuity(v models.StoryFlowContinuity) bool {
	return storyBound(v.Wardrobe, 1, 2000) && storyBound(v.Lighting, 1, 2000) && storyBound(v.CharacterState, 1, 2000)
}
func storyValidCharacters(v []string, s *dto.StoryFlowSnapshot) bool {
	if v == nil {
		return false
	}
	seen := map[string]bool{}
	for _, id := range v {
		found := false
		for _, c := range s.Characters {
			if c.ID == id {
				found = true
			}
		}
		if !found || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func storyValidateScenes(s *dto.StoryFlowSnapshot, scenes []models.StoryFlowScene) error {
	if len(scenes) != s.Input.SceneCount {
		return errors.New("scene count mismatch")
	}
	for i, sc := range scenes {
		if sc.SceneIndex != i+1 || !storyValidCharacters(sc.Characters, s) || !storyValidContinuity(sc.Continuity) || len(sc.Shots) > 0 {
			return errors.New("invalid scene structure")
		}
		for _, v := range []string{sc.Title, sc.Narrative, sc.Location, sc.Emotion} {
			if !storyBound(v, 1, 2000) {
				return errors.New("incomplete scene")
			}
		}
	}
	return nil
}

type storyShotPlan struct {
	Scenes []struct {
		SceneIndex int                    `json:"sceneIndex"`
		Shots      []models.StoryFlowShot `json:"shots"`
	} `json:"scenes"`
}

func storyValidateShots(s *dto.StoryFlowSnapshot, plan *storyShotPlan) error {
	if len(plan.Scenes) != len(s.Scenes) {
		return errors.New("shot scene count mismatch")
	}
	total := 0
	for i, sc := range plan.Scenes {
		if sc.SceneIndex != i+1 || len(sc.Shots) != s.SceneShots[i].ShotCount {
			return errors.New("shot count mismatch")
		}
		for j, sh := range sc.Shots {
			if sh.SceneIndex != i+1 || sh.ShotIndex != j+1 || !storyValidCharacters(sh.Characters, s) || !storyValidContinuity(sh.Continuity) || !s.Model.SupportsOutput(s.Input.AspectRatio, sh.DurationSeconds) {
				return errors.New("invalid shot structure")
			}
			// Model output must never supply orchestration state, paths or provider IDs.
			if sh.PrimaryCharacterID != "" || len(sh.ProviderTaskHistory) != 0 || sh.ID != "" || sh.ImageStatus != "" || sh.VideoStatus != "" || sh.ImageKey != "" || sh.VideoKey != "" || sh.ProviderTaskID != "" || sh.FailureCode != "" || sh.FailureMessage != "" || sh.CanRetry {
				return errors.New("invalid model-owned state")
			}
			for _, v := range []string{sh.Action, sh.Location, sh.Atmosphere, sh.Framing, sh.CameraAngle, sh.CameraMovement, sh.ImagePrompt, sh.VideoPrompt} {
				if !storyBound(v, 1, 2000) {
					return errors.New("incomplete shot")
				}
			}
			for _, id := range sh.Characters {
				found := false
				for _, cid := range s.Scenes[i].Characters {
					if cid == id {
						found = true
					}
				}
				if !found {
					return errors.New("shot character absent from scene")
				}
			}
			total++
		}
	}
	if total < 1 || total > 20 {
		return errors.New("total shot count invalid")
	}
	return nil
}

// Tick claims at most one project and performs one provider/media action.
func (f *StoryFlowFacade) Tick(c context.Context) error {
	p, e := f.store.Claim(c)
	if e != nil {
		return e
	}
	s, e := dto.DecodeStoryFlow(p)
	if e != nil {
		return e
	}
	s.Status = "processing"
	c, cancel := context.WithTimeout(c, 18*time.Minute)
	defer cancel()
	save := func(release bool) error {
		if e := storyEncode(p, &s); e != nil {
			return e
		}
		return f.store.Save(c, p, release)
	}
	switch s.Stage {
	case "story":
		e = f.storyStep(c, p, &s, save)
	case "shots":
		e = f.shotsStep(c, &s, save)
	case "storyboard":
		e = f.storyboardStep(c, p, &s, save)
	case "video":
		e = f.videoStep(c, p, &s, save)
	case "export":
		e = f.exportStep(c, p, &s)
	default:
		storyAttention(&s)
	}
	if e != nil {
		return e
	}
	if s.Status == "processing" {
		s.Status = "queued"
	}
	return save(true)
}
func (f *StoryFlowFacade) storyStep(c context.Context, p *models.StoryFlowProject, s *dto.StoryFlowSnapshot, save func(bool) error) error {
	if s.CharacterStatus == "submitting" {
		storyAttention(s)
		return nil
	}
	if s.CharacterStatus == "pending" {
		s.CharacterStatus = "submitting"
		if e := save(false); e != nil {
			return e
		}
		data, e := f.images.GenerateImage(c, modelark.ImageRequest{Model: s.ImageModel, Prompt: s.CharacterPrompt, Size: ImageSize(s.Input.AspectRatio)})
		if e != nil {
			if known, ok := storyDefinitiveImageError(e); ok {
				s.CharacterStatus = "failed"
				storyFailure(s, known.Code, known.Message, true)
			} else {
				s.CharacterStatus = "needs_attention"
				storyAttention(s)
			}
			return nil
		}
		if !storyValidImage(data) {
			s.CharacterStatus = "failed"
			storyFailure(s, "IMAGE_INVALID_OUTPUT", "บริการส่งภาพที่ใช้ไม่ได้ กรุณาลองใหม่", true)
			return nil
		}
		key := storyKey(p, "character", "new-character", storyImageExtension(data))
		if e = f.storage.Upload(c, key, data, storyImageMIME(data)); e != nil {
			s.CharacterStatus = "needs_attention"
			storyAttention(s)
			return nil
		}
		for i := range s.Characters {
			if s.Characters[i].ID == "new-character" {
				s.Characters[i].StorageKey = key
			}
		}
		s.CharacterStatus = "succeeded"
		return nil
	}
	if s.AnalysisPrompt == "" {
		s.AnalysisPrompt = storySnapshotPrompt(s, "story")
		if s.AnalysisPrompts == nil {
			s.AnalysisPrompts = map[string]string{}
		}
		s.AnalysisPrompts["story"] = s.AnalysisPrompt
		if e := save(false); e != nil {
			return e
		}
	}
	if s.AnalysisStatuses == nil {
		s.AnalysisStatuses = map[string]string{}
	}
	if s.AnalysisStatuses[s.Stage] == "submitting" {
		storyAttention(s)
		return nil
	}
	s.AnalysisStatuses[s.Stage] = "submitting"
	if e := save(false); e != nil {
		return e
	}
	text, e := f.analyzer.Analyze(c, s.AnalysisModel, s.AnalysisPrompt)
	if e != nil {
		storyAnalysisFailure(s, e)
		return nil
	}
	var out struct {
		Scenes []models.StoryFlowScene `json:"scenes"`
	}
	if e = storyDecodeStrict(text, &out); e == nil {
		e = storyValidateScenes(s, out.Scenes)
	}
	if e != nil {
		s.AnalysisStatuses[s.Stage] = "failed"
		storyFailure(s, "ANALYSIS_INVALID_OUTPUT", "โมเดลส่งเนื้อเรื่องไม่ครบหรือรูปแบบไม่ถูกต้อง ยังไม่ได้บันทึกเป็นผลสำเร็จ กรุณาลองใหม่", true)
		return nil
	}
	s.AnalysisStatuses[s.Stage] = "succeeded"
	s.Scenes = out.Scenes
	s.Stage = "story_review"
	s.Status = "draft"
	s.AnalysisPrompt = ""
	return nil
}
func (f *StoryFlowFacade) shotsStep(c context.Context, s *dto.StoryFlowSnapshot, save func(bool) error) error {
	if s.AnalysisPrompt == "" {
		s.AnalysisPrompt = storySnapshotPrompt(s, "shots")
		if s.AnalysisPrompts == nil {
			s.AnalysisPrompts = map[string]string{}
		}
		s.AnalysisPrompts["shots"] = s.AnalysisPrompt
		if e := save(false); e != nil {
			return e
		}
	}
	if s.AnalysisStatuses == nil {
		s.AnalysisStatuses = map[string]string{}
	}
	if s.AnalysisStatuses[s.Stage] == "submitting" {
		storyAttention(s)
		return nil
	}
	s.AnalysisStatuses[s.Stage] = "submitting"
	if e := save(false); e != nil {
		return e
	}
	text, e := f.analyzer.Analyze(c, s.AnalysisModel, s.AnalysisPrompt)
	if e != nil {
		storyAnalysisFailure(s, e)
		return nil
	}
	var out storyShotPlan
	if e = storyDecodeStrict(text, &out); e == nil {
		e = storyValidateShots(s, &out)
	}
	if e != nil {
		s.AnalysisStatuses[s.Stage] = "failed"
		storyFailure(s, "ANALYSIS_INVALID_OUTPUT", "จำนวนหรือข้อมูล Shot ไม่ตรงกับที่กำหนด ยังไม่ได้สร้างภาพ กรุณาลองใหม่", true)
		return nil
	}
	for i := range out.Scenes {
		for j := range out.Scenes[i].Shots {
			sh := &out.Scenes[i].Shots[j]
			sh.ID = uuid.NewString()
			if len(sh.Characters) > 0 {
				sh.PrimaryCharacterID = sh.Characters[0]
			}
			sh.ImageStatus = "pending"
			sh.VideoStatus = "pending"
			storyPromptForShot(s, sh)
			if utf8.RuneCountInString(sh.VideoPrompt) > 2000 || utf8.RuneCountInString(sh.ImagePrompt) > 2000 {
				s.AnalysisStatuses[s.Stage] = "failed"
				storyFailure(s, "ANALYSIS_INVALID_OUTPUT", "รายละเอียด Shot ยาวเกินขอบเขตบริการ กรุณาลองวิเคราะห์ใหม่", true)
				return nil
			}
		}
	}
	for i := range out.Scenes {
		s.Scenes[i].Shots = out.Scenes[i].Shots
	}
	s.AnalysisStatuses[s.Stage] = "succeeded"
	s.Stage = "storyboard"
	s.AnalysisPrompt = ""
	return nil
}
func (f *StoryFlowFacade) storyboardStep(c context.Context, p *models.StoryFlowProject, s *dto.StoryFlowSnapshot, save func(bool) error) error {
	for i := range s.Scenes {
		for j := range s.Scenes[i].Shots {
			sh := &s.Scenes[i].Shots[j]
			if sh.ImageStatus == "succeeded" {
				continue
			}
			if sh.ImageStatus == "submitting" || sh.ImageStatus == "needs_attention" {
				storyAttention(s)
				return nil
			}
			if sh.ImageStatus == "failed" {
				storyFailure(s, sh.FailureCode, sh.FailureMessage, false)
				return nil
			}
			reference := ""
			if len(sh.Characters) > 0 {
				for _, ch := range s.Characters {
					if ch.ID == sh.Characters[0] && ch.StorageKey != "" {
						u, _, e := f.storage.SignedURL(c, ch.StorageKey, time.Hour)
						if e != nil {
							storyShotFailure(s, sh, "storyboard", "REFERENCE_UNAVAILABLE", "เปิดภาพตัวละครไม่ได้ กรุณาลองใหม่", true)
							return nil
						}
						reference = u
						break
					}
				}
			}
			sh.ImageStatus = "submitting"
			if e := save(false); e != nil {
				return e
			}
			data, e := f.images.GenerateImage(c, modelark.ImageRequest{Model: s.ImageModel, Prompt: sh.ImagePrompt, Size: ImageSize(s.Input.AspectRatio), ReferenceURL: reference})
			if e != nil {
				if known, ok := storyDefinitiveImageError(e); ok {
					storyShotFailure(s, sh, "storyboard", known.Code, known.Message, true)
				} else {
					sh.ImageStatus = "needs_attention"
					storyAttention(s)
				}
				return nil
			}
			if !storyValidImage(data) {
				storyShotFailure(s, sh, "storyboard", "IMAGE_INVALID_OUTPUT", "บริการส่งภาพที่ใช้ไม่ได้ กรุณาลองใหม่", true)
				return nil
			}
			key := storyKey(p, "images", sh.ID, storyImageExtension(data))
			if e = f.storage.Upload(c, key, data, storyImageMIME(data)); e != nil {
				sh.ImageStatus = "needs_attention"
				storyAttention(s)
				return nil
			}
			sh.ImageKey = key
			sh.ImageStatus = "succeeded"
			return nil
		}
	}
	s.Stage = "storyboard_review"
	s.Status = "draft"
	return nil
}
func (f *StoryFlowFacade) videoStep(c context.Context, p *models.StoryFlowProject, s *dto.StoryFlowSnapshot, save func(bool) error) error {
	for i := range s.Scenes {
		for j := range s.Scenes[i].Shots {
			sh := &s.Scenes[i].Shots[j]
			if sh.VideoStatus == "succeeded" {
				continue
			}
			if sh.ImageStatus != "succeeded" || sh.ImageKey == "" {
				storyFailure(s, "STORYBOARD_INCOMPLETE", "ภาพ Storyboard ยังไม่ครบ", false)
				return nil
			}
			if sh.VideoStatus == "submitting" || sh.VideoStatus == "needs_attention" {
				storyAttention(s)
				return nil
			}
			if sh.VideoStatus == "failed" {
				storyFailure(s, sh.FailureCode, sh.FailureMessage, false)
				return nil
			}
			if sh.VideoStatus == "pending" {
				image, _, e := f.storage.SignedURL(c, sh.ImageKey, time.Hour)
				if e != nil {
					storyShotFailure(s, sh, "video", "REFERENCE_UNAVAILABLE", "เปิดภาพ Storyboard ไม่ได้ กรุณาลองใหม่", true)
					return nil
				}
				sh.VideoStatus = "submitting"
				if e = save(false); e != nil {
					return e
				}
				id, e := f.video.CreateTask(c, modelark.CreateTaskRequest{Model: s.Model.ProviderModelID, Content: []modelark.ContentItem{{Type: modelark.ContentTypeText, Text: sh.VideoPrompt}, {Type: modelark.ContentTypeImageURL, URL: image, Role: modelark.RoleFirstFrame}}, Ratio: s.Input.AspectRatio, DurationSeconds: int64(sh.DurationSeconds), Resolution: modelark.QualityToResolution(s.Input.Quality)})
				if e != nil {
					d := modelark.VideoFailureDiagnostic(e)
					if d.HTTPStatus >= 400 && d.HTTPStatus < 500 {
						var ve *modelark.VideoError
						if errors.As(e, &ve) {
							storyShotFailure(s, sh, "video", ve.Code, ve.Message, true)
						} else {
							storyShotFailure(s, sh, "video", "VIDEO_REJECTED", "บริการวิดีโอปฏิเสธคำขอนี้", true)
						}
					} else {
						sh.VideoStatus = "needs_attention"
						storyAttention(s)
					}
					return nil
				}
				if id == "" {
					sh.VideoStatus = "needs_attention"
					storyAttention(s)
					return nil
				}
				sh.ProviderTaskHistory = append(sh.ProviderTaskHistory, id)
				sh.ProviderTaskID = id
				sh.VideoStatus = "processing"
				return nil
			}
			if sh.ProviderTaskID == "" {
				storyAttention(s)
				return nil
			}
			result, e := f.video.GetTask(c, sh.ProviderTaskID)
			if e != nil {
				storyShotFailure(s, sh, "video", "VIDEO_STATUS_UNAVAILABLE", "ตรวจสถานะวิดีโอไม่ได้ ลองตรวจใหม่โดยไม่ส่งสร้างซ้ำ", true)
				return nil
			}
			if result == nil {
				storyShotFailure(s, sh, "video", "VIDEO_STATUS_UNAVAILABLE", "ตรวจสถานะวิดีโอไม่ได้ กรุณาลองตรวจใหม่", true)
				return nil
			}
			switch result.Status {
			case modelark.ProviderStatusQueued, modelark.ProviderStatusRunning:
				return nil
			case modelark.ProviderStatusFailed, modelark.ProviderStatusCancelled:
				sh.ProviderTaskID = ""
				storyShotFailure(s, sh, "video", "VIDEO_GENERATION_FAILED", "บริการสร้างวิดีโอไม่สำเร็จหรือปฏิเสธเนื้อหาของ Shot นี้", true)
				return nil
			case modelark.ProviderStatusSucceeded:
				data, kind, e := f.fetcher.Fetch(c, result.VideoURL)
				if e != nil {
					storyShotFailure(s, sh, "video", "VIDEO_SAVE_FAILED", "บันทึกวิดีโอไม่ได้ ลองบันทึกใหม่โดยไม่ส่งสร้างซ้ำ", true)
					return nil
				}
				if !strings.HasPrefix(kind, "video/") || len(data) == 0 {
					storyShotFailure(s, sh, "video", "VIDEO_INVALID_OUTPUT", "บริการส่งไฟล์วิดีโอไม่ถูกต้อง", false)
					return nil
				}
				key := storyKey(p, "clips", sh.ID, "mp4")
				if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
					storyShotFailure(s, sh, "video", "VIDEO_SAVE_FAILED", "บันทึกวิดีโอไม่ได้ ลองบันทึกใหม่โดยไม่ส่งสร้างซ้ำ", true)
					return nil
				}
				sh.VideoKey = key
				sh.VideoStatus = "succeeded"
				return nil
			default:
				storyShotFailure(s, sh, "video", "VIDEO_STATUS_UNKNOWN", "บริการส่งสถานะวิดีโอที่ไม่รู้จัก กรุณาติดต่อผู้ดูแลระบบ", false)
				return nil
			}
		}
	}
	s.Stage = "export"
	return nil
}
func (f *StoryFlowFacade) exportStep(c context.Context, p *models.StoryFlowProject, s *dto.StoryFlowSnapshot) error {
	dir, e := os.MkdirTemp("", "violet-story-export-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	clips := []videoprocessing.Clip{}
	for i, sc := range s.Scenes {
		if sc.SceneIndex != i+1 {
			storyFailure(s, "EXPORT_REVISION_MISMATCH", "ลำดับฉากไม่ตรงกับงานนี้", false)
			return nil
		}
		for j, sh := range sc.Shots {
			if sh.SceneIndex != i+1 || sh.ShotIndex != j+1 || sh.VideoStatus != "succeeded" || sh.VideoKey != storyKey(p, "clips", sh.ID, "mp4") {
				storyFailure(s, "EXPORT_INCOMPLETE", "คลิปยังไม่ครบหรือไม่ตรงกับ revision นี้", false)
				return nil
			}
			path := filepath.Join(dir, fmt.Sprintf("clip-%03d.mp4", len(clips)))
			if e = f.downloadOwned(c, sh.VideoKey, path); e != nil {
				storyFailure(s, "EXPORT_FAILED", "โหลดคลิปสำหรับรวมวิดีโอไม่ได้ กรุณาลองใหม่", true)
				return nil
			}
			clips = append(clips, videoprocessing.Clip{Path: path})
		}
	}
	if len(clips) < 1 || len(clips) > 20 {
		storyFailure(s, "EXPORT_INCOMPLETE", "จำนวนคลิปไม่ถูกต้อง", false)
		return nil
	}
	data, e := f.processor.Merge(c, videoprocessing.MergeRequest{Clips: clips, OutputResolution: modelark.QualityToResolution(s.Input.Quality), OutputAspectRatio: s.Input.AspectRatio, AudioVolume: 1})
	if e != nil || len(data) == 0 {
		storyFailure(s, "EXPORT_FAILED", "รวมวิดีโอไม่สำเร็จ กรุณาลองใหม่", true)
		return nil
	}
	key := storyKey(p, "export", "final", "mp4")
	if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
		storyFailure(s, "EXPORT_FAILED", "บันทึกวิดีโอรวมไม่ได้ กรุณาลองใหม่", true)
		return nil
	}
	s.OutputKey = key
	s.ExportStatus = "succeeded"
	s.Status = "succeeded"
	s.Stage = "done"
	return nil
}
func (f *StoryFlowFacade) downloadOwned(c context.Context, key, path string) error {
	u, _, e := f.storage.SignedURL(c, key, 30*time.Minute)
	if e != nil {
		return e
	}
	if f.options.OwnedMediaFetcher != nil {
		data, _, e := f.options.OwnedMediaFetcher.Fetch(c, u)
		if e != nil {
			return e
		}
		return os.WriteFile(path, data, 0600)
	}
	parsed, e := url.Parse(u)
	if e != nil || parsed.Scheme != "https" || parsed.User != nil {
		return errors.New("invalid storage url")
	}
	client := http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Host != parsed.Host || r.URL.Scheme != "https" {
			return errors.New("storage redirect rejected")
		}
		return nil
	}}
	request, e := http.NewRequestWithContext(c, http.MethodGet, u, nil)
	if e != nil {
		return e
	}
	response, e := client.Do(request)
	if e != nil {
		return errors.New("storage fetch failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("storage fetch failed")
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, (512<<20)+1))
	if e != nil || len(data) > 512<<20 {
		return errors.New("storage output exceeds limit")
	}
	return os.WriteFile(path, bytes.Clone(data), 0600)
}

func storyDefinitiveImageError(e error) (*modelark.ImageError, bool) {
	var known *modelark.ImageError
	if !errors.As(e, &known) {
		return nil, false
	}
	d := modelark.ImageFailureDiagnostic(e)
	definitive := d.HTTPStatus >= 400 && d.HTTPStatus < 500
	switch d.ProviderCode {
	case "InputTextSensitiveContentDetected", "InputImageSensitiveContentDetected", "OutputImageSensitiveContentDetected", "InvalidParameter", "MissingParameter", "InvalidRequest", "AuthenticationError", "Unauthorized", "AccessDenied", "ModelNotFound", "QuotaExceeded", "InsufficientBalance", "RateLimit", "RateLimitExceeded":
		definitive = true
	}
	return known, definitive
}

func storyValidImage(data []byte) bool {
	if len(data) == 0 || len(data) > 24<<20 {
		return false
	}
	c, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || (format != "png" && format != "jpeg") || c.Width < 1 || c.Height < 1 || c.Width > 8192 || c.Height > 8192 || int64(c.Width)*int64(c.Height) > 24<<20 {
		return false
	}
	// Bound decoded allocation before reading pixels; headers alone do not prove a usable bitmap.
	decoded, actualFormat, e := image.Decode(bytes.NewReader(data))
	return e == nil && actualFormat == format && decoded.Bounds().Dx() == c.Width && decoded.Bounds().Dy() == c.Height
}

func storyImageMIME(data []byte) string {
	_, format, _ := image.DecodeConfig(bytes.NewReader(data))
	if format == "jpeg" {
		return "image/jpeg"
	}
	return "image/png"
}

func storyAnalysisFailure(s *dto.StoryFlowSnapshot, err error) {
	var known *modelark.StoryAnalysisError
	if errors.As(err, &known) && known.Definitive {
		s.AnalysisStatuses[s.Stage] = "failed"
		storyFailure(s, known.Code, known.Message, true)
		return
	}
	storyAttention(s)
}

func storyImageExtension(data []byte) string {
	if storyImageMIME(data) == "image/jpeg" {
		return "jpg"
	}
	return "png"
}
