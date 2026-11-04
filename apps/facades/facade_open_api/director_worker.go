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
	"net/http"
	"strings"
	"time"
)

type directorSceneOutput struct {
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	Mood              string   `json:"mood"`
	CharacterAssetIDs []string `json:"characterAssetIds"`
}
type directorShotOutput struct {
	ID                string   `json:"id,omitempty"`
	Description       string   `json:"description"`
	Camera            string   `json:"camera"`
	CharacterAssetIDs []string `json:"characterAssetIds"`
}

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
	status := "needs_attention"
	if definitive {
		status = "failed"
	}
	failure := dto.DirectorFailure{FailureCode: directorPointer("GENERATION_UNCERTAIN"), FailureMessage: directorPointer("ยังยืนยันผลการสร้างไม่ได้ กรุณาติดต่อผู้ดูแลระบบก่อนส่งซ้ำ"), CanRetry: false}
	if definitive {
		failure = dto.DirectorFailure{FailureCode: directorPointer("GENERATION_FAILED"), FailureMessage: directorPointer("สร้างไม่สำเร็จ กรุณาตรวจคำอธิบายแล้วลองใหม่"), CanRetry: true}
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
			directorRecount(p)
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
	if lookupErr == nil {
		d, decodeErr := directorDecode(current)
		if decodeErr != nil {
			return decodeErr
		}
		valid = d.Generations[j.TargetID] == in.Generation
	}
	if !valid {
		return f.finish(c, j, in, "cancelled", j.Status != "submitting" && len(j.Output) == 0, func(*directorDocument) error { return nil })
	}
	// A persisted response can be stored again, but an uncertain external call cannot be repeated.
	if j.Status == "submitting" && len(j.Output) == 0 {
		return f.fail(c, j, in, false)
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
		schema = `{"logline":"Thai logline","scenes":[{"title":"Thai title <=80 chars","summary":"Thai summary <=1200 chars","mood":"one supplied Thai mood","characterAssetIds":[]}]}. Exactly one scene per supplied placeholder, same order.`
	case "rewrite":
		schema = `{"title":"Thai title <=80 chars","summary":"Thai summary <=1200 chars","mood":"one supplied Thai mood","characterAssetIds":[]}. Rewrite target scene only using neighboring scenes for continuity; preserve intended shot count.`
	case "plan":
		schema = `{"shots":[{"id":"existing placeholder shot UUID","description":"Thai shot description <=600 chars","camera":"one supplied camera","characterAssetIds":[]}]}. Return exactly all placeholder IDs once in scene/shot order; character IDs must be subset of that scene.`
	case "add":
		schema = `{"description":"Thai shot description <=600 chars","camera":"one supplied camera","characterAssetIds":[]}. Write only the target new shot, using neighboring shots and scene for continuity. Do not return id.`
	default:
		return f.fail(c, j, in, true)
	}
	b, _ := json.Marshal(in)
	cap := f.Capabilities()
	constraints, _ := json.Marshal(struct{ Moods, Cameras []string }{cap.Moods, cap.Cameras})
	text, e := f.analyzer.Analyze(c, in.Model, operation+"\nReturn exactly JSON schema: "+schema+"\nTarget ID: "+j.TargetID+"\nAllowed values: "+string(constraints)+"\nProject and instruction data: "+string(b))
	if e != nil {
		return f.fail(c, j, in, directorAnalysisDefinitive(e))
	}
	switch j.Kind {
	case "story":
		var result struct {
			Logline string                `json:"logline"`
			Scenes  []directorSceneOutput `json:"scenes"`
		}
		if e = storyDecodeStrict(text, &result); e != nil || !directorBound(result.Logline, 1, 1200) || len(result.Scenes) != len(in.Project.Scenes) {
			return f.fail(c, j, in, true)
		}
		for _, sc := range result.Scenes {
			if !f.validScene(sc, in) {
				return f.fail(c, j, in, true)
			}
		}
		if e = f.progress(c, j, in, func(d *directorDocument) error {
			d.Project.Story.Logline = &result.Logline
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
		if e = storyDecodeStrict(text, &sc); e != nil || !f.validScene(sc, in) {
			return f.fail(c, j, in, true)
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
			for i := range s.Shots {
				s.Shots[i].Image.Stale = true
			}
			return nil
		})
	case "plan":
		var result struct {
			Shots []directorShotOutput `json:"shots"`
		}
		if e = storyDecodeStrict(text, &result); e != nil || len(result.Shots) != directorTotal(&in.Project) {
			return f.fail(c, j, in, true)
		}
		seen := map[string]bool{}
		for _, s := range result.Shots {
			old := directorFindShot(&in.Project, s.ID)
			if old == nil || seen[s.ID] {
				return f.fail(c, j, in, true)
			}
			seen[s.ID] = true
			sc := directorFindScene(&in.Project, old.SceneID)
			if !f.validShot(s, sc.CharacterAssetIDs) {
				return f.fail(c, j, in, true)
			}
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
			}
			return nil
		})
	case "add":
		var result directorShotOutput
		if e = storyDecodeStrict(text, &result); e != nil || result.ID != "" {
			return f.fail(c, j, in, true)
		}
		sh := directorFindShot(&in.Project, j.TargetID)
		if sh == nil {
			return f.fail(c, j, in, true)
		}
		sc := directorFindScene(&in.Project, sh.SceneID)
		if !f.validShot(result, sc.CharacterAssetIDs) {
			return f.fail(c, j, in, true)
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
	prompt := in.Project.Idea.Prompt
	characters := in.Project.Idea.CharacterAssetIDs
	if j.Kind != "cover" {
		m, e := f.store.Get(c, j.ProjectID.String(), j.UserID.String())
		if e != nil {
			return f.fail(c, j, in, len(j.Output) == 0 && j.Status != "submitting")
		}
		d, e := directorDecode(m)
		if e != nil {
			return e
		}
		sh := directorFindShot(&d.Project, j.TargetID)
		if sh == nil || d.Generations[j.TargetID] != in.Generation {
			return f.finish(c, j, in, "cancelled", len(j.Output) == 0 && j.Status != "submitting", func(*directorDocument) error { return nil })
		}
		if !directorBound(sh.Description, 1, 600) {
			return f.fail(c, j, in, true)
		}
		prompt = sh.Description + "\nCamera: " + sh.Camera
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
		prompt += "\nStyle: " + in.Project.Style + "\nGenre: " + in.Project.Idea.Genre + "\nMoods: " + strings.Join(in.Project.Idea.Moods, ", ") + "\nAspect ratio: " + in.Project.AspectRatio
		ref := ""
		for _, ch := range in.Characters {
			if directorContains(characters, ch.ID) {
				prompt += "\nCharacter: " + ch.Description
				if ref == "" {
					u, _, e := f.storage.SignedURL(c, ch.Key, time.Hour)
					if e != nil {
						return f.fail(c, j, in, true)
					}
					ref = u
				}
			}
		}
		if ref == "" && in.CoverKey != "" && j.Kind != "cover" && in.Project.Storyboard.StyleReference == "cover" {
			u, _, e := f.storage.SignedURL(c, in.CoverKey, time.Hour)
			if e != nil {
				return f.fail(c, j, in, true)
			}
			ref = u
			prompt += "\nUse the reference image only for visual style and color tone, retain the described scene content."
		}
		data, e := f.images.GenerateImage(c, modelark.ImageRequest{Model: in.ImageModel, Prompt: prompt, Size: directorImageSize(in.Project.AspectRatio), ReferenceURL: ref})
		if e != nil {
			_, definitive := storyDefinitiveImageError(e)
			return f.fail(c, j, in, definitive)
		}
		if !storyValidImage(data) {
			return f.fail(c, j, in, true)
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
