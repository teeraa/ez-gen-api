package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type directorSceneOutput struct {
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	Mood              string   `json:"mood"`
	CharacterAssetIDs []string `json:"characterAssetIds"`
	Setting           string   `json:"setting,omitempty"`
}
type directorShotOutput struct {
	ID                string   `json:"id,omitempty"`
	Description       string   `json:"description"`
	Camera            string   `json:"camera"`
	CharacterAssetIDs []string `json:"characterAssetIds"`
	Prompt            string   `json:"prompt,omitempty"`
}

// directorPlannerInput is what the text model sees: no storage keys or job metadata.
type directorPlannerInput struct {
	Project     dto.DirectorProject `json:"project"`
	Instruction string              `json:"instruction,omitempty"`
	Characters  []directorCastBrief `json:"characters"`
	VisualBible string              `json:"visualBible,omitempty"`
	Settings    map[string]string   `json:"sceneSettings,omitempty"`
}
type directorCastBrief struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

const directorPlannerGuide = "Write for a storyboard whose images are generated one shot at a time, so every shot must be self-contained and consistent. " +
	"Every shot description and prompt must name the scene's location explicitly and name every visible character; a close-up lists only the character in frame. " +
	"Use each character's supplied name and look exactly; never invent new main characters. " +
	"Keep each scene in one coherent location unless the story requires a move."
const directorShotGuide = ` "prompt" is an English image-generation prompt of 60-120 words: subject and action, every visible character by name with expression and pose, ` +
	`foreground/background composition, the scene's location and lighting from sceneSettings, and the visual bible's era and palette. ` +
	`Never describe a named character's clothing, colors, hair or accessories: refer to them by name only, their fixed look is applied separately. ` +
	`"description" is a Thai summary of the same shot for the user (1-2 sentences, names the characters). characterAssetIds lists every character visible in the shot.`

func directorSubset(ids, allowed []string) bool {
	if ids == nil {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !directorContains(allowed, id) {
			return false
		}
		seen[id] = true
	}
	return true
}
func (f *DirectorFacade) validScene(s directorSceneOutput, in directorJobInput) bool {
	return directorBound(s.Title, 1, 80) && directorBound(s.Summary, 1, 1200) && directorContains(f.Capabilities().Moods, s.Mood) && directorSubset(s.CharacterAssetIDs, in.Project.Idea.CharacterAssetIDs)
}
func (f *DirectorFacade) validShot(s directorShotOutput, allowed []string) bool {
	return directorBound(s.Description, 1, 600) && directorContains(f.Capabilities().Cameras, s.Camera) && directorSubset(s.CharacterAssetIDs, allowed)
}
func (f *DirectorFacade) progress(c context.Context, j *models.DirectorJob, in directorJobInput, fn func(*directorDocument) error) error {
	return f.store.Progress(c, j, func(m *models.DirectorProject) error {
		d, e := directorDecode(m)
		if e != nil {
			return e
		}
		if d.Generations[j.TargetID] != in.Generation {
			return repo.ErrConflict
		}
		if e = fn(&d); e != nil {
			return e
		}
		return directorEncode(m, &d)
	})
}
func (f *DirectorFacade) finish(c context.Context, j *models.DirectorJob, in directorJobInput, status string, refund bool, fn func(*directorDocument) error) error {
	return f.store.Complete(c, j, status, refund, func(m *models.DirectorProject) error {
		d, e := directorDecode(m)
		if e != nil {
			return e
		}
		if d.Generations[j.TargetID] != in.Generation {
			return nil
		}
		if e = fn(&d); e != nil {
			return e
		}
		return directorEncode(m, &d)
	})
}
func (f *DirectorFacade) fail(c context.Context, j *models.DirectorJob, in directorJobInput, definitive bool) error {
	return f.failWith(c, j, in, definitive, "", "", nil)
}

// failWith records a user-safe reason and logs the internal cause; provider payloads and prompts are never logged.
func (f *DirectorFacade) failWith(c context.Context, j *models.DirectorJob, in directorJobInput, definitive bool, code, message string, cause error) error {
	status := "needs_attention"
	if definitive {
		status = "failed"
	}
	log.Printf("director: job=%s kind=%s status=%s code=%s cause=%v", j.ID, j.Kind, status, code, directorCause(cause))
	failure := dto.DirectorFailure{FailureCode: directorPointer("GENERATION_UNCERTAIN"), FailureMessage: directorPointer("ยังยืนยันผลการสร้างไม่ได้ กรุณาติดต่อผู้ดูแลระบบก่อนส่งซ้ำ"), CanRetry: false}
	if definitive {
		failure = dto.DirectorFailure{FailureCode: directorPointer("GENERATION_FAILED"), FailureMessage: directorPointer("สร้างไม่สำเร็จ กรุณาตรวจคำอธิบายแล้วลองใหม่"), CanRetry: true}
	}
	if code != "" {
		failure.FailureCode = directorPointer(code)
	}
	if message != "" {
		failure.FailureMessage = directorPointer(message)
	}
	return f.finish(c, j, in, status, definitive, func(d *directorDocument) error {
		p := &d.Project
		switch j.Kind {
		case "story":
			p.Story.Status = status
			p.Story.Phase = nil
			p.Story.DirectorFailure = failure
			if definitive {
				p.Story.PointsRefunded += j.Cost
			}
			for i := range p.Scenes {
				if p.Scenes[i].Status != "ready" {
					p.Scenes[i].Status = "failed"
					p.Scenes[i].DirectorFailure = failure
				}
			}
		case "rewrite":
			if sc := directorFindScene(p, j.TargetID); sc != nil {
				sc.Status = "failed"
				sc.DirectorFailure = failure
				if definitive {
					sc.PointsRefunded += j.Cost
				}
			}
		case "cover":
			if p.Cover != nil && p.Cover.ID == j.TargetID {
				p.Cover.Status = status
				p.Cover.DirectorFailure = failure
				if definitive {
					p.Cover.PointsRefunded += j.Cost
				}
			}
		case "plan":
			// Images can still be generated from the scene text when shot planning fails.
			for i := range p.Scenes {
				sc := &p.Scenes[i]
				for k := range sc.Shots {
					if strings.TrimSpace(sc.Shots[k].Description) == "" {
						sc.Shots[k].Description = directorTruncate(sc.Title+": "+sc.Summary, 600)
					}
				}
			}
			directorRecount(p)
		case "export":
			if p.Export != nil {
				p.Export.Status = status
				p.Export.Progress = 0
				p.Export.DirectorFailure = failure
			}
		case "video":
			if sh := directorFindShot(p, in.ShotID); sh != nil && sh.Video != nil {
				sh.Video.Status = status
				sh.Video.Progress = 0
				sh.Video.DirectorFailure = failure
				if definitive {
					sh.Video.PointsRefunded += j.Cost
				}
			}
		default:
			if sh := directorFindShot(p, j.TargetID); sh != nil {
				sh.Image.Status = status
				sh.Image.DirectorFailure = failure
				if definitive {
					sh.PointsRefunded += j.Cost
				}
			}
			directorRecount(p)
		}
		return nil
	})
}
func directorAnalysisDefinitive(e error) bool {
	var a *modelark.StoryAnalysisError
	return errors.As(e, &a) && a.Definitive
}
func (f *DirectorFacade) Tick(ctx context.Context) error {
	j, e := f.store.Claim(ctx)
	if e != nil {
		return e
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var in directorJobInput
	if e = json.Unmarshal(j.Snapshot, &in); e != nil {
		return e
	}
	// A removed target is cancelled before any external side effect.
	current, lookupErr := f.store.Get(c, j.ProjectID.String(), j.UserID.String())
	if lookupErr != nil && !errors.Is(lookupErr, repo.ErrNotFound) {
		return lookupErr
	}
	valid := false
	var doc directorDocument
	if lookupErr == nil {
		var decodeErr error
		doc, decodeErr = directorDecode(current)
		if decodeErr != nil {
			return decodeErr
		}
		valid = doc.Generations[j.TargetID] == in.Generation
	}
	if !valid {
		return f.finish(c, j, in, "cancelled", j.Status != "submitting" && len(j.Output) == 0, func(*directorDocument) error { return nil })
	}
	// A persisted response can be stored again, but an uncertain external call cannot be repeated.
	if j.Status == "submitting" && len(j.Output) == 0 {
		return f.fail(c, j, in, false)
	}
	if len(j.Output) == 0 && len(in.Characters) > 0 {
		in.Characters = f.characterLooks(c, j, in, &doc)
	}
	if j.Kind == "video" {
		return f.directorVideo(c, j, in)
	}
	if j.Kind == "export" {
		return f.directorExport(c, j, in)
	}
	if j.Kind == "image" || j.Kind == "cover" || (j.Kind == "add" && len(j.Output) > 0) {
		return f.directorImage(c, j, in)
	}
	if e = f.progress(c, j, in, func(d *directorDocument) error {
		switch j.Kind {
		case "story":
			d.Project.Story.Status = "processing"
			d.Project.Story.Phase = directorPointer("analyzing")
		case "plan":
			d.Project.Storyboard.Status = "processing"
		}
		return nil
	}); e != nil {
		return e
	}
	operation := "DIRECTOR_" + strings.ToUpper(j.Kind)
	schema := ""
	switch j.Kind {
	case "story":
		schema = `{"logline":"Thai logline","visualBible":"English, <=500 chars: era/world, architecture, costume rules for background extras only (never restyle named characters), color palette, lighting language","scenes":[{"title":"Thai title <=80 chars","summary":"Thai summary <=1200 chars","mood":"one supplied Thai mood","characterAssetIds":["ids of characters in this scene"],"setting":"English, <=400 chars: exact location, time of day, weather, light sources, key props and background details shared by every shot of the scene"}]}. Exactly one scene per supplied placeholder, same order. ` + directorPlannerGuide
	case "rewrite":
		schema = `{"title":"Thai title <=80 chars","summary":"Thai summary <=1200 chars","mood":"one supplied Thai mood","characterAssetIds":["ids of characters in this scene"],"setting":"English, <=400 chars: exact location, time of day, weather, light sources, key props"}. Rewrite target scene only using neighboring scenes for continuity; preserve intended shot count and the visual bible. ` + directorPlannerGuide
	case "plan":
		schema = `{"shots":[{"id":"existing placeholder shot UUID","description":"Thai shot description <=600 chars","camera":"one supplied camera","characterAssetIds":["subset of that scene's characters"],"prompt":"English image prompt"}]}. Return exactly all placeholder IDs once in scene/shot order. Vary cameras across a scene.` + directorShotGuide + " " + directorPlannerGuide
	case "add":
		schema = `{"description":"Thai shot description <=600 chars","camera":"one supplied camera","characterAssetIds":["subset of that scene's characters"],"prompt":"English image prompt"}. Write only the target new shot, using neighboring shots and scene for continuity. Do not return id.` + directorShotGuide + " " + directorPlannerGuide
	default:
		return f.fail(c, j, in, true)
	}
	brief := directorPlannerInput{Project: in.Project, Instruction: in.Instruction, Characters: []directorCastBrief{}, VisualBible: doc.Bible, Settings: doc.Settings}
	// Scene settings live on the current scenes (users can edit them); the snapshot may predate an edit.
	for i := range brief.Project.Scenes {
		if cur := directorFindScene(&doc.Project, brief.Project.Scenes[i].ID); cur != nil && cur.Setting != "" {
			brief.Project.Scenes[i].Setting = cur.Setting
		}
	}
	for _, ch := range in.Characters {
		brief.Characters = append(brief.Characters, directorCastBrief{ch.ID, ch.Name, ch.Description})
	}
	b, _ := json.Marshal(brief)
	cap := f.Capabilities()
	constraints, _ := json.Marshal(struct{ Moods, Cameras []string }{cap.Moods, cap.Cameras})
	text, e := f.analyzer.Analyze(c, in.Model, operation+"\nReturn exactly JSON schema: "+schema+"\nTarget ID: "+j.TargetID+"\nAllowed values: "+string(constraints)+"\nProject and instruction data: "+string(b))
	if e != nil {
		code, message := "", ""
		var a *modelark.StoryAnalysisError
		if errors.As(e, &a) {
			code, message = a.Code, directorAnalysisMessages[a.Code]
		}
		return f.failWith(c, j, in, directorAnalysisDefinitive(e), code, message, e)
	}
	invalid := func(reason error) error {
		return f.failWith(c, j, in, true, "ANALYSIS_INVALID_OUTPUT", "AI ตอบกลับในรูปแบบที่ใช้ไม่ได้ กรุณาลองใหม่อีกครั้ง", reason)
	}
	switch j.Kind {
	case "story":
		var result struct {
			Logline     string                `json:"logline"`
			VisualBible string                `json:"visualBible"`
			Scenes      []directorSceneOutput `json:"scenes"`
		}
		if e = directorDecodeJSON(text, &result); e != nil {
			return invalid(e)
		}
		result.Logline = directorTruncate(result.Logline, 1200)
		if result.Logline == "" || len(result.Scenes) < len(in.Project.Scenes) {
			return invalid(fmt.Errorf("story scenes %d want %d", len(result.Scenes), len(in.Project.Scenes)))
		}
		result.Scenes = result.Scenes[:len(in.Project.Scenes)]
		for i := range result.Scenes {
			result.Scenes[i] = f.normalizeScene(result.Scenes[i], in)
			if !f.validScene(result.Scenes[i], in) {
				return invalid(fmt.Errorf("story scene %d empty", i+1))
			}
		}
		if e = f.progress(c, j, in, func(d *directorDocument) error {
			d.Project.Story.Logline = &result.Logline
			d.Bible = directorTruncate(result.VisualBible, 500)
			d.Project.Story.Phase = directorPointer("writing")
			if len(d.Project.Scenes) > 0 {
				d.Project.Scenes[0].Status = "writing"
			}
			return nil
		}); e != nil {
			return e
		}
		for i, sc := range result.Scenes {
			if e = f.progress(c, j, in, func(d *directorDocument) error {
				s := directorFindScene(&d.Project, in.Project.Scenes[i].ID)
				if s == nil {
					return repo.ErrConflict
				}
				s.Title = sc.Title
				s.Summary = sc.Summary
				s.Mood = &sc.Mood
				s.CharacterAssetIDs = sc.CharacterAssetIDs
				s.Status = "ready"
				if setting := directorTruncate(sc.Setting, 400); setting != "" {
					s.Setting = setting
				}
				d.Project.Story.CompletedScenes = i + 1
				if i+1 < len(d.Project.Scenes) {
					d.Project.Scenes[i+1].Status = "writing"
				} else {
					d.Project.Story.Phase = directorPointer("reviewing")
				}
				return nil
			}); e != nil {
				return e
			}
		}
		return f.finish(c, j, in, "succeeded", false, func(d *directorDocument) error {
			d.Project.Story.Status = "succeeded"
			d.Project.Story.Phase = nil
			return nil
		})
	case "rewrite":
		var sc directorSceneOutput
		if e = directorDecodeJSON(text, &sc); e != nil {
			return invalid(e)
		}
		if sc = f.normalizeScene(sc, in); !f.validScene(sc, in) {
			return invalid(errors.New("rewrite scene empty"))
		}
		return f.finish(c, j, in, "succeeded", false, func(d *directorDocument) error {
			s := directorFindScene(&d.Project, j.TargetID)
			if s == nil {
				return nil
			}
			s.Title = sc.Title
			s.Summary = sc.Summary
			s.Mood = &sc.Mood
			s.CharacterAssetIDs = sc.CharacterAssetIDs
			s.Status = "ready"
			if setting := directorTruncate(sc.Setting, 400); setting != "" {
				s.Setting = setting
			}
			for i := range s.Shots {
				s.Shots[i].Image.Stale = true
			}
			return nil
		})
	case "plan":
		var result struct {
			Shots []directorShotOutput `json:"shots"`
		}
		if e = directorDecodeJSON(text, &result); e != nil {
			return invalid(e)
		}
		result.Shots = f.alignPlannedShots(result.Shots, &in.Project)
		if len(result.Shots) == 0 {
			return invalid(errors.New("plan returned no usable shots"))
		}
		return f.finish(c, j, in, "succeeded", false, func(d *directorDocument) error {
			for _, s := range result.Shots {
				sh := directorFindShot(&d.Project, s.ID)
				if sh == nil {
					continue
				}
				original := directorFindShot(&in.Project, s.ID)
				if sh.Description == original.Description {
					sh.Description = s.Description
				}
				if sh.Camera == original.Camera {
					sh.Camera = s.Camera
				}
				sh.CharacterAssetIDs = s.CharacterAssetIDs
				if s.Prompt != "" {
					d.Prompts[s.ID] = directorPlannedPrompt{Prompt: s.Prompt, Hash: directorShotHash(s.Description, s.Camera)}
				}
			}
			for i := range d.Project.Scenes {
				sc := &d.Project.Scenes[i]
				for k := range sc.Shots {
					if strings.TrimSpace(sc.Shots[k].Description) == "" {
						sc.Shots[k].Description = directorTruncate(sc.Title+": "+sc.Summary, 600)
					}
				}
			}
			return nil
		})
	case "add":
		var result directorShotOutput
		if e = directorDecodeJSON(text, &result); e != nil {
			return invalid(e)
		}
		sh := directorFindShot(&in.Project, j.TargetID)
		if sh == nil {
			return f.fail(c, j, in, true)
		}
		sc := directorFindScene(&in.Project, sh.SceneID)
		result.ID = ""
		if result = f.normalizeShot(result, sc.CharacterAssetIDs); !f.validShot(result, sc.CharacterAssetIDs) {
			return invalid(errors.New("added shot empty"))
		}
		if e = f.progress(c, j, in, func(d *directorDocument) error {
			s := directorFindShot(&d.Project, j.TargetID)
			if s == nil {
				return repo.ErrConflict
			}
			if s.Description == "" {
				s.Description = result.Description
			}
			if s.Camera == sh.Camera {
				s.Camera = result.Camera
			}
			s.CharacterAssetIDs = result.CharacterAssetIDs
			if result.Prompt != "" {
				d.Prompts[s.ID] = directorPlannedPrompt{Prompt: result.Prompt, Hash: directorShotHash(result.Description, result.Camera)}
			}
			return nil
		}); e != nil {
			return e
		}
		return f.directorImage(c, j, in)
	}
	return nil
}
func (f *DirectorFacade) directorImage(c context.Context, j *models.DirectorJob, in directorJobInput) error {
	contentHash := ""
	prompt := ""
	characters := in.Project.Idea.CharacterAssetIDs
	var doc directorDocument
	var shot *dto.DirectorShot
	if j.Kind != "cover" {
		m, e := f.store.Get(c, j.ProjectID.String(), j.UserID.String())
		if e != nil {
			return f.fail(c, j, in, len(j.Output) == 0 && j.Status != "submitting")
		}
		d, e := directorDecode(m)
		if e != nil {
			return e
		}
		doc = d
		sh := directorFindShot(&doc.Project, j.TargetID)
		shot = sh
		if sh == nil || d.Generations[j.TargetID] != in.Generation {
			return f.finish(c, j, in, "cancelled", len(j.Output) == 0 && j.Status != "submitting", func(*directorDocument) error { return nil })
		}
		if !directorBound(sh.Description, 1, 600) {
			return f.failWith(c, j, in, true, "SHOT_NOT_PLANNED", "shot นี้ยังไม่มีคำบรรยายภาพ กรุณาพิมพ์คำบรรยายแล้วกดลองใหม่", nil)
		}
		contentHash = storyHash(struct{ Description, Camera string }{sh.Description, sh.Camera})
		characters = sh.CharacterAssetIDs
		// Persist the exact submitted prompt hash; recovery after upload failure must not recalculate it from edited text.
		if len(j.Output) > 0 {
			contentHash = in.Description
		} else {
			in.Description = contentHash
			raw, _ := json.Marshal(in)
			j.Snapshot = raw
		}
	}
	if len(j.Output) == 0 {
		if e := f.progress(c, j, in, func(d *directorDocument) error {
			if j.Kind == "cover" {
				if d.Project.Cover == nil {
					return repo.ErrConflict
				}
				d.Project.Cover.Status = "processing"
			} else {
				sh := directorFindShot(&d.Project, j.TargetID)
				if sh == nil {
					return repo.ErrConflict
				}
				sh.Image.Status = "processing"
				directorRecount(&d.Project)
			}
			return nil
		}); e != nil {
			return e
		}
		cast := directorShotCharacters(in.Characters, characters)
		if shot != nil {
			cast = directorFeaturedCast(cast, directorShotAction(&doc, shot))
		}
		if len(cast) > directorMaxImageReferences {
			cast = cast[:directorMaxImageReferences]
		}
		// Referenced characters go first so "reference Image N" matches the request order.
		referenced, unreferenced, refs := []directorCharacter{}, []directorCharacter{}, []string{}
		for _, ch := range cast {
			if len(refs) >= f.options.MaxImageReferences {
				unreferenced = append(unreferenced, ch)
				continue
			}
			u, _, e := f.storage.SignedURL(c, ch.Key, time.Hour)
			if e == nil && f.options.ReferenceCheck != nil {
				e = f.options.ReferenceCheck(c, u)
			}
			if e != nil {
				log.Printf("director: job=%s reference for character %s unavailable, generating without it: %v", j.ID, ch.ID, directorCause(e))
				unreferenced = append(unreferenced, ch)
				continue
			}
			referenced = append(referenced, ch)
			refs = append(refs, u)
		}
		cast = append(referenced, unreferenced...)
		build := func(n int) string {
			if j.Kind == "cover" {
				return directorCoverPrompt(&in.Project, cast, n)
			}
			return directorImagePrompt(&doc, &doc.Project, shot, cast, n)
		}
		styleRef := ""
		if len(refs) == 0 && in.CoverKey != "" && j.Kind != "cover" && in.Project.Storyboard.StyleReference == "cover" {
			u, _, e := f.storage.SignedURL(c, in.CoverKey, time.Hour)
			if e != nil {
				return f.fail(c, j, in, true)
			}
			styleRef = u
		}
		stopProgress := f.imageProgress(c, j, in)
		var data []byte
		var e error
		// A rejected request is retried with fewer references so one unusable reference does not
		// block the shot; content-policy and account errors are never retried.
		for n := len(refs); ; {
			prompt = build(n)
			attempt := refs[:n]
			if styleRef != "" {
				attempt = []string{styleRef}
				prompt += "\nUse reference Image 1 only for visual style and color tone; keep the described scene content."
			}
			data, e = f.images.GenerateImage(c, modelark.ImageRequest{Model: in.ImageModel, Prompt: prompt, Size: directorImageSize(in.Project.AspectRatio), ReferenceURLs: attempt})
			if e == nil || n == 0 || !directorRetryWithFewerReferences(e) {
				break
			}
			log.Printf("director: job=%s image rejected with %d references, retrying with fewer: %s", j.ID, n, modelark.ImageFailureDetail(e))
			if n > 1 {
				n = 1
			} else {
				n = 0
			}
		}
		stopProgress()
		if e != nil {
			known, definitive := storyDefinitiveImageError(e)
			code, message := "IMAGE_PROVIDER_ERROR", ""
			d := modelark.ImageFailureDiagnostic(e)
			if known != nil {
				code, message = known.Code, known.Message+directorProviderHint(d.HTTPStatus, d.ProviderCode)
			}
			return f.failWith(c, j, in, definitive, code, message, fmt.Errorf("image provider http=%d code=%s model=%s size=%s refs=%d detail=%q: %w", d.HTTPStatus, d.ProviderCode, in.ImageModel, directorImageSize(in.Project.AspectRatio), len(refs), modelark.ImageFailureDetail(e), e))
		}
		if !storyValidImage(data) {
			return f.failWith(c, j, in, true, "IMAGE_INVALID_OUTPUT", "บริการสร้างภาพส่งไฟล์ที่ใช้ไม่ได้ กรุณาลองใหม่", fmt.Errorf("invalid image bytes len=%d type=%s", len(data), http.DetectContentType(data)))
		}
		// Persist returned bytes before any fallible storage operation. Recovery performs storage only.
		if e = f.store.RecordOutput(c, j, data); e != nil {
			return e
		}
		j.Output = data
	}
	kind := http.DetectContentType(j.Output)
	ext := "png"
	if kind == "image/jpeg" {
		ext = "jpg"
	}
	key := fmt.Sprintf("director/%s/%s/%s/%s.%s", j.UserID, j.ProjectID, j.TargetID, j.ID, ext)
	if e := f.storage.Upload(c, key, j.Output, kind); e != nil {
		return e
	}
	return f.finish(c, j, in, "succeeded", false, func(d *directorDocument) error {
		d.Keys[j.TargetID] = key
		if j.Kind == "cover" {
			if d.Project.Cover != nil && d.Project.Cover.ID == j.TargetID {
				d.Project.Cover.Status = "succeeded"
				d.Project.Cover.Progress = 100
				d.Project.Cover.DirectorFailure = dto.DirectorFailure{}
			}
		} else {
			if sh := directorFindShot(&d.Project, j.TargetID); sh != nil {
				sh.Image.Status = "succeeded"
				sh.Image.Progress = 100
				sh.Image.Stale = contentHash != storyHash(struct{ Description, Camera string }{sh.Description, sh.Camera})
				sh.Image.DirectorFailure = dto.DirectorFailure{}
				directorMarkVideoStale(sh)
				// The previous shot's clip ends on this image as its last frame.
				if prev := directorPreviousShot(&d.Project, sh); prev != nil && prev.Video != nil && prev.Video.UsesLastFrame {
					directorMarkVideoStale(prev)
				}
				if d.Project.Cover != nil && d.Project.Cover.ShotID != nil && *d.Project.Cover.ShotID == sh.ID {
					d.Keys[d.Project.Cover.ID] = key
				}
			}
			directorRecount(&d.Project)
		}
		return nil
	})
}

func directorImageSize(ratio string) string {
	if ratio == "4:5" {
		return "1792x2240"
	}
	return ImageSize(ratio)
}

const (
	directorVideoPoll     = 10 * time.Second
	directorVideoRetry    = 30 * time.Second
	directorVideoDeadline = 2 * time.Hour
	// Provider result URLs expire after about a day; stop retrying the download well before that.
	directorVideoFetchWindow = 20 * time.Hour
)

type directorVideoTask struct {
	TaskID      string    `json:"taskId"`
	SubmittedAt time.Time `json:"submittedAt"`
}

func (f *DirectorFacade) directorVideo(c context.Context, j *models.DirectorJob, in directorJobInput) error {
	now := time.Now().UTC()
	if len(j.Output) == 0 {
		if !f.videoAvailable() || in.ImageKey == "" || in.VideoModel == "" {
			return f.fail(c, j, in, true)
		}
		m, e := f.store.Get(c, j.ProjectID.String(), j.UserID.String())
		if e != nil {
			return f.fail(c, j, in, true)
		}
		doc, e := directorDecode(m)
		if e != nil {
			return e
		}
		request := modelark.CreateTaskRequest{Model: in.VideoModel, Resolution: "1080p"}
		if in.DraftTaskID != "" {
			// The provider inherits prompt, frames, ratio and duration from the draft and rejects resubmission.
			request.Content = []modelark.ContentItem{{Type: modelark.ContentTypeDraftTask, DraftTaskID: in.DraftTaskID}}
		} else {
			first, _, e := f.storage.SignedURL(c, in.ImageKey, time.Hour)
			if e != nil {
				return f.failWith(c, j, in, true, "REFERENCE_UNAVAILABLE", "เปิดภาพ shot ไม่ได้ กรุณาลองใหม่", e)
			}
			sh := directorFindShot(&doc.Project, in.ShotID)
			if sh == nil {
				sh = directorFindShot(&in.Project, in.ShotID)
			}
			if sh == nil {
				return f.fail(c, j, in, true)
			}
			// Seedance reference mode: storyboard frames first (hard-wired as start/end in the
			// prompt), then character identity references.
			frames := []string{first}
			if in.NextImageKey != "" {
				last, _, e := f.storage.SignedURL(c, in.NextImageKey, time.Hour)
				if e != nil {
					return f.failWith(c, j, in, true, "REFERENCE_UNAVAILABLE", "เปิดภาพ shot ถัดไปไม่ได้ กรุณาลองใหม่", e)
				}
				frames = append(frames, last)
			}
			cast := directorFeaturedCast(directorShotCharacters(in.Characters, sh.CharacterAssetIDs), directorShotAction(&doc, sh))
			refs := []string{}
			used := []directorCharacter{}
			for _, ch := range cast {
				if len(frames)+len(refs) >= directorMaxVideoReferences || ch.Key == "" {
					break
				}
				u, _, e := f.storage.SignedURL(c, ch.Key, time.Hour)
				if e == nil && f.options.ReferenceCheck != nil {
					e = f.options.ReferenceCheck(c, u)
				}
				if e != nil {
					log.Printf("director: job=%s video reference for character %s unavailable: %s", j.ID, ch.ID, directorCause(e))
					continue
				}
				refs = append(refs, u)
				used = append(used, ch)
			}
			prompt := directorVideoPrompt(&doc, &doc.Project, sh, used, in.NextImageKey != "")
			request.Content = []modelark.ContentItem{{Type: modelark.ContentTypeText, Text: prompt}}
			for _, u := range append(frames, refs...) {
				request.Content = append(request.Content, modelark.ContentItem{Type: modelark.ContentTypeImageURL, URL: u, Role: modelark.RoleReferenceImage})
			}
			request.Ratio, request.DurationSeconds, request.Resolution, request.Draft = in.Ratio, int64(in.Seconds), in.Resolution, in.Draft
			if in.Draft {
				request.Resolution = "480p"
			}
		}
		if e = f.progress(c, j, in, func(d *directorDocument) error {
			sh := directorFindShot(&d.Project, in.ShotID)
			if sh == nil || sh.Video == nil {
				return repo.ErrConflict
			}
			sh.Video.Status = "processing"
			sh.Video.Progress = 5
			return nil
		}); e != nil {
			return e
		}
		id, e := f.video.CreateTask(c, request)
		if e != nil {
			d := modelark.VideoFailureDiagnostic(e)
			code, message := "VIDEO_PROVIDER_ERROR", ""
			var ve *modelark.VideoError
			if errors.As(e, &ve) {
				code, message = ve.Code, ve.Message
			}
			// Nothing reached the provider when the request failed before submission: refund and allow retry.
			definitive := d.HTTPStatus >= 400 && d.HTTPStatus < 500 || errors.Is(e, modelark.ErrNotSubmitted)
			return f.failWith(c, j, in, definitive, code, message, fmt.Errorf("video submit http=%d model=%s ratio=%s seconds=%d resolution=%s draft=%t refine=%t last_frame=%t refs=%d: %w", d.HTTPStatus, in.VideoModel, request.Ratio, request.DurationSeconds, request.Resolution, request.Draft, in.DraftTaskID != "", in.NextImageKey != "", len(request.Content)-1, e))
		}
		if id == "" {
			return f.fail(c, j, in, false)
		}
		raw, e := json.Marshal(directorVideoTask{TaskID: id, SubmittedAt: now})
		if e != nil {
			return e
		}
		return f.store.Wait(c, j, raw, now.Add(directorVideoPoll))
	}
	var task directorVideoTask
	if json.Unmarshal(j.Output, &task) != nil || task.TaskID == "" {
		return f.fail(c, j, in, false)
	}
	if f.video == nil || f.fetcher == nil {
		return f.store.Wait(c, j, j.Output, now.Add(time.Minute))
	}
	age := now.Sub(task.SubmittedAt)
	result, e := f.video.GetTask(c, task.TaskID)
	if e != nil || result == nil {
		if age > directorVideoDeadline {
			return f.fail(c, j, in, false)
		}
		return f.store.Wait(c, j, j.Output, now.Add(directorVideoRetry))
	}
	switch result.Status {
	case modelark.ProviderStatusQueued, modelark.ProviderStatusRunning:
		if age > directorVideoDeadline {
			return f.fail(c, j, in, false)
		}
		progress := min(90, 10+int(age.Seconds()*80/120))
		if e = f.progress(c, j, in, func(d *directorDocument) error {
			if sh := directorFindShot(&d.Project, in.ShotID); sh != nil && sh.Video != nil {
				sh.Video.Status = "processing"
				sh.Video.Progress = progress
			}
			return nil
		}); e != nil {
			return e
		}
		return f.store.Wait(c, j, j.Output, now.Add(directorVideoPoll))
	case modelark.ProviderStatusFailed, modelark.ProviderStatusCancelled:
		return f.failWith(c, j, in, true, "VIDEO_GENERATION_FAILED", "บริการสร้างวิดีโอไม่สำเร็จหรือปฏิเสธเนื้อหาของ shot นี้ กรุณาปรับคำบรรยายหรือภาพแล้วลองใหม่", fmt.Errorf("video task status=%s code=%s", result.Status, result.ErrorCode))
	case modelark.ProviderStatusSucceeded:
		data, kind, e := f.fetcher.Fetch(c, result.VideoURL)
		if e != nil {
			if age > directorVideoFetchWindow {
				return f.fail(c, j, in, false)
			}
			return f.store.Wait(c, j, j.Output, now.Add(directorVideoRetry))
		}
		if !strings.HasPrefix(kind, "video/") || len(data) == 0 {
			return f.fail(c, j, in, false)
		}
		key := fmt.Sprintf("director/%s/%s/%s/%s.mp4", j.UserID, j.ProjectID, in.ShotID, j.ID)
		if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
			return f.store.Wait(c, j, j.Output, now.Add(directorVideoRetry))
		}
		return f.finish(c, j, in, "succeeded", false, func(d *directorDocument) error {
			d.Keys[j.TargetID] = key
			if in.Draft {
				d.VideoDrafts[in.ShotID] = directorVideoDraft{TaskID: task.TaskID, Provider: in.VideoModel, CreatedAt: task.SubmittedAt}
			} else {
				delete(d.VideoDrafts, in.ShotID)
			}
			if sh := directorFindShot(&d.Project, in.ShotID); sh != nil && sh.Video != nil {
				sh.Video.Status = "succeeded"
				sh.Video.Progress = 100
				sh.Video.Draft = in.Draft
				nextChanged := in.NextShotID != "" && d.Keys[in.NextShotID] != in.NextImageKey
				sh.Video.Stale = in.DraftTaskID == "" && (d.Keys[sh.ID] != in.ImageKey || sh.Description != in.Description || sh.Camera != in.Camera || nextChanged)
				sh.Video.DirectorFailure = dto.DirectorFailure{}
			}
			return nil
		})
	default:
		return f.fail(c, j, in, false)
	}
}

func directorCause(e error) string {
	if e == nil {
		return "-"
	}
	return directorTruncate(e.Error(), 300)
}
func directorTruncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max]))
}

// Language models sometimes wrap JSON in fences or add explanatory fields; the
// shape is still validated after decoding, so unknown fields are tolerated here.
func directorDecodeJSON(text string, out any) error {
	text = strings.TrimSpace(text)
	if len(text) > 512<<10 {
		return errors.New("analysis output too large")
	}
	start, end := strings.IndexAny(text, "{["), strings.LastIndexAny(text, "}]")
	if start < 0 || end < start {
		return errors.New("analysis output has no JSON object")
	}
	return json.Unmarshal([]byte(text[start:end+1]), out)
}
func directorFilterIDs(ids, allowed []string) []string {
	out := []string{}
	for _, id := range ids {
		if directorContains(allowed, id) && !directorContains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
func directorPick(value string, allowed []string, fallback string) string {
	value = strings.TrimSpace(value)
	if directorContains(allowed, value) {
		return value
	}
	for _, a := range allowed {
		if value != "" && (strings.Contains(value, a) || strings.Contains(a, value)) {
			return a
		}
	}
	return fallback
}
func (f *DirectorFacade) normalizeScene(s directorSceneOutput, in directorJobInput) directorSceneOutput {
	moods := f.Capabilities().Moods
	fallback := moods[0]
	if len(in.Project.Idea.Moods) > 0 {
		fallback = in.Project.Idea.Moods[0]
	}
	s.Title = directorTruncate(s.Title, 80)
	s.Summary = directorTruncate(s.Summary, 1200)
	s.Mood = directorPick(s.Mood, moods, fallback)
	s.CharacterAssetIDs = directorFilterIDs(s.CharacterAssetIDs, in.Project.Idea.CharacterAssetIDs)
	return s
}
func (f *DirectorFacade) normalizeShot(s directorShotOutput, allowed []string) directorShotOutput {
	cameras := f.Capabilities().Cameras
	s.Description = directorTruncate(s.Description, 600)
	s.Prompt = directorTruncate(s.Prompt, 1200)
	s.Camera = directorPick(s.Camera, cameras, cameras[0])
	s.CharacterAssetIDs = directorFilterIDs(s.CharacterAssetIDs, allowed)
	return s
}

// alignPlannedShots keeps outputs whose IDs match placeholders, then assigns the rest to the
// remaining placeholders in storyboard order, because models often renumber or drop IDs.
func (f *DirectorFacade) alignPlannedShots(shots []directorShotOutput, p *dto.DirectorProject) []directorShotOutput {
	order := []*dto.DirectorShot{}
	for i := range p.Scenes {
		for k := range p.Scenes[i].Shots {
			order = append(order, &p.Scenes[i].Shots[k])
		}
	}
	used := map[string]bool{}
	out := []directorShotOutput{}
	rest := []directorShotOutput{}
	for _, s := range shots {
		if old := directorFindShot(p, s.ID); old != nil && !used[s.ID] {
			used[s.ID] = true
			out = append(out, s)
		} else {
			rest = append(rest, s)
		}
	}
	for _, sh := range order {
		if len(rest) == 0 {
			break
		}
		if !used[sh.ID] {
			s := rest[0]
			rest = rest[1:]
			s.ID = sh.ID
			used[sh.ID] = true
			out = append(out, s)
		}
	}
	valid := []directorShotOutput{}
	for _, s := range out {
		sc := directorFindScene(p, directorFindShot(p, s.ID).SceneID)
		if s = f.normalizeShot(s, sc.CharacterAssetIDs); f.validShot(s, sc.CharacterAssetIDs) {
			valid = append(valid, s)
		}
	}
	return valid
}

// Analyzer messages may carry provider text, so users see only these fixed messages.
var directorAnalysisMessages = map[string]string{
	"ANALYSIS_REJECTED":       "AI ปฏิเสธคำขอนี้ กรุณาปรับไอเดียหรือตรวจสิทธิ์โมเดลแล้วลองใหม่",
	"ANALYSIS_UNAVAILABLE":    "บริการ AI ไม่ตอบสนอง กรุณาลองใหม่อีกครั้ง",
	"ANALYSIS_NOT_CONFIGURED": "ยังไม่ได้ตั้งค่าโมเดลเขียนเนื้อเรื่อง กรุณาติดต่อผู้ดูแลระบบ",
	"ANALYSIS_INVALID_OUTPUT": "AI ตอบกลับในรูปแบบที่ใช้ไม่ได้ กรุณาลองใหม่อีกครั้ง",
}

// The provider code is allowlisted by the modelark adapter, so it is safe to show and lets
// testers report the exact provider reason without server log access.
func directorProviderHint(status int, code string) string {
	switch {
	case status > 0 && code != "":
		return fmt.Sprintf(" (HTTP %d · %s)", status, code)
	case status > 0:
		return fmt.Sprintf(" (HTTP %d)", status)
	case code != "":
		return " (" + code + ")"
	}
	return ""
}

func directorRetryWithFewerReferences(e error) bool {
	known, _ := storyDefinitiveImageError(e)
	if known == nil {
		return false
	}
	d := modelark.ImageFailureDiagnostic(e)
	if strings.Contains(d.ProviderCode, "Sensitive") {
		return false
	}
	switch known.Code {
	case "PROVIDER_INVALID_INPUT", "MODEL_UNAVAILABLE", "PROVIDER_ERROR":
		return true
	}
	return false
}

// imageProgress publishes an estimated percentage while the synchronous image call runs,
// because the provider reports no intermediate progress. It approaches but never reaches 95.
func (f *DirectorFacade) imageProgress(c context.Context, j *models.DirectorJob, in directorJobInput) func() {
	ctx, cancel := context.WithCancel(c)
	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		ticker := time.NewTicker(directorImageProgressTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p := directorEstimatedProgress(time.Since(started))
				_ = f.progress(ctx, j, in, func(d *directorDocument) error {
					if j.Kind == "cover" {
						if d.Project.Cover != nil && d.Project.Cover.Status == "processing" {
							d.Project.Cover.Progress = p
						}
						return nil
					}
					if sh := directorFindShot(&d.Project, j.TargetID); sh != nil && sh.Image.Status == "processing" {
						sh.Image.Progress = p
					}
					return nil
				})
			}
		}
	}()
	return func() { cancel(); <-done }
}

const directorImageProgressTick = 2 * time.Second

func directorEstimatedProgress(elapsed time.Duration) int {
	return min(95, 5+int(90*(1-math.Exp(-elapsed.Seconds()/25))))
}

// characterLooks attaches each character's cached appearance text, describing any reference
// image not described yet. Failures fall back to the library description and are logged.
func (f *DirectorFacade) characterLooks(c context.Context, j *models.DirectorJob, in directorJobInput, doc *directorDocument) []directorCharacter {
	out := append([]directorCharacter(nil), in.Characters...)
	fresh := map[string]directorLook{}
	for i := range out {
		ch := &out[i]
		look, ok := doc.Looks[ch.ID]
		if (!ok || look.Key != ch.Key || look.Text == "") && f.options.LookDescriber != nil && f.options.ImageFetch != nil && ch.Key != "" {
			text, e := f.describeLook(c, ch.Key)
			if e != nil {
				log.Printf("director: job=%s describe look for character %s failed, using library text: %s", j.ID, ch.ID, directorCause(e))
				continue
			}
			look, ok = directorLook{Text: text, Key: ch.Key}, true
			fresh[ch.ID] = look
		}
		if ok && look.Key == ch.Key && look.Text != "" {
			ch.Description = directorMergeLook(look.Text, ch.Description)
		}
	}
	if len(fresh) > 0 {
		if e := f.progress(c, j, in, func(d *directorDocument) error {
			for id, l := range fresh {
				d.Looks[id] = l
			}
			return nil
		}); e != nil {
			log.Printf("director: job=%s caching character looks failed: %v", j.ID, e)
		}
	}
	return out
}

func (f *DirectorFacade) describeLook(c context.Context, key string) (string, error) {
	u, _, e := f.storage.SignedURL(c, key, 10*time.Minute)
	if e != nil {
		return "", fmt.Errorf("sign reference: %w", e)
	}
	data, mime, e := f.options.ImageFetch(c, u)
	if e != nil {
		return "", fmt.Errorf("fetch reference: %w", e)
	}
	text, e := f.options.LookDescriber.DescribeImage(c, f.options.AnalysisModel, data, mime)
	if e != nil {
		return "", e
	}
	text = directorTruncate(text, 600)
	if text == "" {
		return "", errors.New("empty look description")
	}
	return text, nil
}

func directorMergeLook(look, description string) string {
	description = strings.TrimSpace(description)
	if description == "" || strings.Contains(look, description) {
		return look
	}
	return look + " (" + description + ")"
}

// directorExport merges the clip keys frozen at request time, so edits made while it runs
// cannot mix old and new clips; the result is marked stale afterwards instead.
func (f *DirectorFacade) directorExport(c context.Context, j *models.DirectorJob, in directorJobInput) error {
	if f.options.Processor == nil || f.options.VideoFetch == nil {
		return f.failWith(c, j, in, true, "EXPORT_UNAVAILABLE", "ยังไม่ได้ตั้งค่าระบบรวมวิดีโอ กรุณาติดต่อผู้ดูแลระบบ", nil)
	}
	if len(in.ClipKeys) == 0 || len(in.ClipKeys) > 40 {
		return f.failWith(c, j, in, true, "VIDEO_INCOMPLETE", "ยังมี shot ที่ไม่มีวิดีโอ กรุณาสร้างวิดีโอให้ครบก่อน export", nil)
	}
	setProgress := func(p int) error {
		return f.progress(c, j, in, func(d *directorDocument) error {
			if d.Project.Export != nil {
				d.Project.Export.Status = "processing"
				d.Project.Export.Progress = p
			}
			return nil
		})
	}
	if e := setProgress(5); e != nil {
		return e
	}
	dir, e := os.MkdirTemp("", "director-export-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	clips := make([]videoprocessing.Clip, 0, len(in.ClipKeys))
	for i, key := range in.ClipKeys {
		u, _, e := f.storage.SignedURL(c, key, 30*time.Minute)
		if e == nil {
			var data []byte
			if data, _, e = f.options.VideoFetch(c, u); e == nil {
				path := filepath.Join(dir, fmt.Sprintf("clip-%03d.mp4", i))
				if e = os.WriteFile(path, data, 0o600); e == nil {
					clips = append(clips, videoprocessing.Clip{Path: path})
				}
			}
		}
		if e != nil {
			return f.failWith(c, j, in, true, "EXPORT_FAILED", "โหลดคลิปสำหรับรวมวิดีโอไม่ได้ กรุณาลองใหม่", fmt.Errorf("clip %d: %w", i+1, e))
		}
		if e = setProgress(5 + (i+1)*55/len(in.ClipKeys)); e != nil {
			return e
		}
	}
	data, e := f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: clips, OutputResolution: in.Project.Resolution, OutputAspectRatio: in.Project.AspectRatio, AudioVolume: 1})
	if e != nil || len(data) == 0 {
		return f.failWith(c, j, in, true, "EXPORT_FAILED", "รวมวิดีโอไม่สำเร็จ กรุณาลองใหม่", fmt.Errorf("merge %d clips: %v", len(clips), e))
	}
	if e = setProgress(90); e != nil {
		return e
	}
	key := fmt.Sprintf("director/%s/%s/export/%s.mp4", j.UserID, j.ProjectID, j.ID)
	if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
		return f.failWith(c, j, in, true, "EXPORT_FAILED", "บันทึกวิดีโอรวมไม่ได้ กรุณาลองใหม่", e)
	}
	return f.finish(c, j, in, "succeeded", false, func(d *directorDocument) error {
		d.Keys["export"] = key
		d.ExportSource = storyHash(in.ClipKeys)
		if d.Project.Export != nil {
			d.Project.Export.Status = "succeeded"
			d.Project.Export.Progress = 100
			d.Project.Export.ClipCount = len(in.ClipKeys)
			d.Project.Export.DirectorFailure = dto.DirectorFailure{}
		}
		return nil
	})
}
