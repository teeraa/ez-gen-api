package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

const directorSyntheticLegacyVersion = "director-scene-3d-v2/" + characterdesign.PipelineStylized3DCompilerVersion + "/" + promptcompiler.PipelineFilmedHumanVideoVersion
const directorSyntheticPreviousVersion = "director-scene-3d-v3/" + characterdesign.PipelineStylized3DCompilerVersion + "/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/director-filmed-look-v1"
const directorSyntheticMaterialVersion = "director-scene-3d-v4/director-synthetic-scene-materials-v1/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/director-filmed-look-v2"
const directorSyntheticVideoOnlyVersion = "director-scene-3d-v5/" + characterdesign.PipelineStylized3DCompilerVersion + "/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/director-filmed-look-v2"
const directorSyntheticTemplateVersion = "director-scene-3d-v6/" + characterdesign.PipelineStylized3DCompilerVersion + "/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/" + promptcompiler.FilmedTemplateVersion
const directorSyntheticLayoutVersion = "director-scene-3d-v7/" + characterdesign.PipelineStylized3DCompilerVersion + "/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/" + promptcompiler.FilmedTemplateVersion + "/director-filmed-layout-v2"
const directorSyntheticRealCGVersion = "director-scene-3d-v8/director-filmed-scene-cg-v1/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/" + promptcompiler.FilmedTemplateVersion + "/director-filmed-layout-v3/" + directorFilmedNeutralVersion
const directorSyntheticStylizedVersion = "director-scene-3d-v9/director-filmed-scene-stylized-v2/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/" + promptcompiler.FilmedTemplateVersion + "/director-filmed-layout-v4/" + directorFilmedNeutralVersion
const directorSyntheticVersion = "director-scene-3d-v10/" + characterdesign.DirectorPoseCastingVersion + "/" + promptcompiler.PipelineFilmedHumanVideoVersion + "/" + promptcompiler.FilmedTemplateVersion + "/" + promptcompiler.FilmedPoseCastingVersion + "/" + promptcompiler.FilmedLayoutVersion + "/" + directorFilmedNeutralVersion

// External-call markers deliberately remain durable when a response is uncertain.
type directorSyntheticState struct {
	Look          promptcompiler.FilmedLook  `json:"filmedLook,omitempty"`
	Phase         string                     `json:"syntheticPhase,omitempty"`
	Version       string                     `json:"syntheticVersion,omitempty"`
	SourceKey     string                     `json:"sourceKey,omitempty"`
	SourceClipKey string                     `json:"sourceClipKey,omitempty"`
	ImageModel    string                     `json:"imageModel,omitempty"`
	Style         string                     `json:"style,omitempty"`
	Key           string                     `json:"key,omitempty"`
	Bytes         []byte                     `json:"bytes,omitempty"`
	Request       modelark.CreateTaskRequest `json:"request"`
}

func (f *DirectorFacade) syntheticCheckpoint(c context.Context, j *models.DirectorJob, state directorSyntheticState) error {
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

func directorSyntheticStage(j *models.DirectorJob, in directorJobInput, stage string, versions ...string) string {
	style := "other"
	if in.Project.Style == "cinematic" || in.Project.Style == "realistic" {
		style = in.Project.Style
	}
	version := directorSyntheticVersion
	if len(versions) > 0 {
		switch versions[0] {
		case directorSyntheticVersion, directorSyntheticStylizedVersion, directorSyntheticRealCGVersion, directorSyntheticLayoutVersion, directorSyntheticTemplateVersion, directorSyntheticVideoOnlyVersion, directorSyntheticMaterialVersion, directorSyntheticPreviousVersion, directorSyntheticLegacyVersion:
			version = versions[0]
		default:
			version = "unknown"
		}
	}
	line := fmt.Sprintf("director: job=%s pipeline=%s style=%s stage=%s", j.ID, version, style, stage)
	log.Print(line)
	return line
}

func (f *DirectorFacade) directorSyntheticVideo(c context.Context, j *models.DirectorJob, in directorJobInput, state directorSyntheticState) error {
	now := time.Now().UTC()
	if state.Phase == "generating" || state.Phase == "submitting" {
		return f.fail(c, j, in, false)
	}
	if state.Phase == "" {
		directorSyntheticStage(j, in, "preparation_start")
		if !f.videoAvailable() || f.images == nil || in.ImageKey == "" || in.VideoModel == "" || in.ImageModel == "" {
			return f.fail(c, j, in, true)
		}
		row, e := f.store.Get(c, j.ProjectID.String(), j.UserID.String())
		if e != nil {
			return e
		}
		doc, e := directorDecode(row)
		if e != nil {
			return e
		}
		sh := directorFindShot(&doc.Project, in.ShotID)
		if sh == nil {
			return f.fail(c, j, in, true)
		}
		// Pose casting starts from the shot's own storyboard frame. New jobs only reach this for a scene's
		// first shot (user decision 2026-11-09); later shots continue from the previous clip's last frame.
		source, sourceClip := in.ImageKey, ""
		prev := directorPreviousShot(&doc.Project, sh)
		continues := prev != nil
		if continues && directorFacelessRecipe(in.FilmedRecipe) {
			// Continue from the previous clip's actual last frame (its pose and position); the face is
			// re-cast so no realistic face is sent to the provider.
			if directorVideoBusy(prev) {
				return f.store.Wait(c, j, nil, now.Add(directorVideoPoll))
			}
			frame := doc.VideoFrames[prev.ID]
			if prev.Video == nil || prev.Video.Status != "succeeded" || prev.Video.Stale || frame.Key == "" || frame.ClipKey != doc.Keys[directorVideoTarget(prev.ID)] {
				return f.failWith(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "กรุณาสร้างวิดีโอ shot ก่อนหน้าใหม่ให้สำเร็จก่อน", nil)
			}
			source, sourceClip = frame.Key, frame.ClipKey
		}
		u, _, e := f.storage.SignedURL(c, source, time.Hour)
		if e != nil {
			return f.failWith(c, j, in, true, "REFERENCE_UNAVAILABLE", "เปิดภาพอ้างอิงไม่ได้ กรุณาลองใหม่", e)
		}
		if f.options.ReferenceCheck != nil {
			if e = f.options.ReferenceCheck(c, u); e != nil {
				return f.failWith(c, j, in, true, "REFERENCE_UNAVAILABLE", "เปิดภาพอ้างอิงไม่ได้ กรุณาลองใหม่", e)
			}
		}
		model, ok := modelark.ProviderModel(in.VideoModel)
		if !ok || !model.SupportsRefImage {
			return f.failWith(c, j, in, true, "MODEL_UNAVAILABLE", "โมเดลวิดีโอไม่รองรับภาพอ้างอิงนี้", nil)
		}
		frozen := doc
		if snap := directorFindShot(&in.Project, in.ShotID); snap != nil {
			sh = snap
			frozen.Project = in.Project
		}
		scene := directorShotAction(&frozen, sh)
		setting := directorSceneSetting(&frozen, directorFindScene(&frozen.Project, sh.SceneID))
		cast := directorFeaturedCast(directorShotCharacters(in.Characters, sh.CharacterAssetIDs), scene)
		for _, ch := range cast {
			scene += "\nCharacter: " + ch.Name + ": " + directorTruncate(ch.Description, 600)
		}
		scene += "\nCamera: " + directorCameraTerm(sh.Camera) + "; movement: " + directorCameraMotionTerm(sh.Camera)
		scene += "\n" + strings.TrimSpace(directorPropLines(&frozen, directorShotAction(&frozen, sh)))
		if continues {
			scene += "\nContinue naturally from the preceding shot of the same scene, keeping wardrobe, lighting and the characters' surroundings consistent."
		}
		scene, setting = directorFilmedNeutralize(scene), directorFilmedNeutralize(setting)
		videoScene := scene
		if directorFacelessRecipe(in.FilmedRecipe) || in.FilmedRecipe == "clay" {
			videoScene += "\nThe reference image hides faces and surface materials on purpose: render every face, skin, hair and fabric photorealistically as live-action from the character descriptions; never copy the reference's blur or gray clay."
		}
		look := promptcompiler.FilmedLookNatural
		if in.Project.Style == "cinematic" {
			look = promptcompiler.FilmedLookCinematic
		}
		compiled, e := promptcompiler.Compile(promptcompiler.Inputs{FilmedLook: look, PipelineRenderingVersion: promptcompiler.PipelineFilmedHumanVideoVersion, ModelID: in.VideoModel, Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: videoScene, SettingOverride: setting}, Assets: []promptcompiler.AssetRef{{StorageKey: "synthetic", Role: "reference", MimeType: "image/png", PoseCasting: true}}}, model)
		if e != nil {
			if errors.Is(e, promptcompiler.ErrPromptBudget) {
				return f.failWith(c, j, in, true, "VIDEO_PROMPT_TOO_LONG", "รายละเอียดวิดีโอรวมยาวเกินขีดจำกัด กรุณาย่อข้อความแล้วลองใหม่", nil)
			}
			return f.failWith(c, j, in, true, "VIDEO_INVALID_INPUT", "เตรียมคำอธิบายวิดีโอไม่สำเร็จ กรุณาปรับคำบรรยาย", e)
		}
		state = directorSyntheticState{Look: look, Phase: "generating", Version: directorSyntheticVersion, SourceKey: source, SourceClipKey: sourceClip, ImageModel: in.ImageModel, Style: in.Project.Style, Request: modelark.CreateTaskRequest{Model: in.VideoModel, Content: compiled.ContentItems, Ratio: in.Ratio, DurationSeconds: int64(in.Seconds), Resolution: in.Resolution, Draft: in.Draft, ReturnLastFrame: true}}
		if in.Draft {
			state.Request.Resolution = "480p"
		}
		if e = f.syntheticCheckpoint(c, j, state); e != nil {
			return e
		}
		directorSyntheticStage(j, in, "image_generate_start", state.Version)
		casting := characterdesign.CompileDirectorFacelessCasting(scene)
		switch in.FilmedRecipe {
		case "pose3d":
			casting = characterdesign.CompileDirectorPoseCasting(scene)
		case "clay":
			casting = characterdesign.CompileDirectorClayCasting(scene)
		}
		directorSyntheticStage(j, in, "recipe_"+directorRecipeLabel(in.FilmedRecipe), state.Version)
		data, e := f.images.GenerateImage(c, modelark.ImageRequest{Model: state.ImageModel, Prompt: casting, Size: directorImageSize(in.Ratio), ReferenceURLs: []string{u}})
		if e != nil {
			directorSyntheticStage(j, in, "image_generate_failed", state.Version)
			known, definitive := storyDefinitiveImageError(e)
			code, message := "VIDEO_REFERENCE_PREPARATION_FAILED", "เตรียมภาพสำหรับวิดีโอไม่สำเร็จ กรุณาลองใหม่"
			if known != nil {
				code, message = known.Code, known.Message
			}
			return f.failWith(c, j, in, definitive, code, message, e)
		}
		if len(data) > 32<<20 || !storyValidImage(data) {
			return f.failWith(c, j, in, true, "VIDEO_REFERENCE_INVALID", "ภาพสำหรับวิดีโอใช้ไม่ได้ กรุณาลองใหม่", nil)
		}
		directorSyntheticStage(j, in, "image_prepared", state.Version)
		state.Bytes, state.Phase = data, "prepared"
		ext := ".png"
		if http.DetectContentType(data) == "image/jpeg" {
			ext = ".jpg"
		}
		state.Key = fmt.Sprintf("director/%s/%s/%s/%s-synthetic%s", j.UserID, j.ProjectID, in.ShotID, j.ID, ext)
		if e = f.syntheticCheckpoint(c, j, state); e != nil {
			return e
		}
	}
	if (state.Version != directorSyntheticVersion && state.Version != directorSyntheticStylizedVersion && state.Version != directorSyntheticRealCGVersion && state.Version != directorSyntheticLayoutVersion && state.Version != directorSyntheticTemplateVersion && state.Version != directorSyntheticVideoOnlyVersion && state.Version != directorSyntheticMaterialVersion && state.Version != directorSyntheticPreviousVersion && state.Version != directorSyntheticLegacyVersion) || state.SourceKey == "" || state.Key == "" || len(state.Request.Content) != 2 {
		return f.fail(c, j, in, false)
	}
	expectedBase := fmt.Sprintf("director/%s/%s/%s/%s-synthetic", j.UserID, j.ProjectID, in.ShotID, j.ID)
	if (state.Key != expectedBase+".png" && state.Key != expectedBase+".jpg") || state.Request.Content[0].Type != modelark.ContentTypeText || state.Request.Content[1].Type != modelark.ContentTypeImageURL || state.Request.Content[1].Role != modelark.RoleReferenceImage {
		return f.failWith(c, j, in, false, "VIDEO_SYNTHETIC_REFERENCE_UNVERIFIED", "ยืนยันภาพสังเคราะห์เดิมไม่ได้ กรุณาติดต่อผู้ดูแลระบบก่อนส่งซ้ำ", nil)
	}
	if state.Phase == "prepared" {
		if !storyValidImage(state.Bytes) || len(state.Bytes) > 32<<20 {
			return f.fail(c, j, in, false)
		}
		if e := f.storage.Upload(c, state.Key, state.Bytes, http.DetectContentType(state.Bytes)); e != nil {
			return f.store.Wait(c, j, j.Output, now.Add(directorVideoRetry))
		}
		state.Bytes, state.Phase = nil, "ready"
		if e := f.syntheticCheckpoint(c, j, state); e != nil {
			return e
		}
	}
	if state.Phase != "ready" {
		return f.fail(c, j, in, false)
	}
	u, _, e := f.storage.SignedURL(c, state.Key, time.Hour)
	if e != nil {
		return f.store.Wait(c, j, j.Output, now.Add(directorVideoRetry))
	}
	state.Request.Content[1].URL = u
	dependencyChanged := errors.New("video source changed before submission")
	if e = f.progress(c, j, in, func(d *directorDocument) error {
		sh := directorFindShot(&d.Project, in.ShotID)
		if sh == nil || sh.Video == nil {
			return repo.ErrConflict
		}
		prev := directorPreviousShot(&d.Project, sh)
		if state.SourceClipKey != "" {
			if prev == nil || prev.Video == nil || prev.Video.Status != "succeeded" || prev.Video.Stale || d.Keys[directorVideoTarget(prev.ID)] != state.SourceClipKey || d.VideoFrames[prev.ID].Key != state.SourceKey || d.VideoFrames[prev.ID].ClipKey != state.SourceClipKey {
				return dependencyChanged
			}
		} else if prev != nil && state.Version != directorSyntheticVersion {
			return dependencyChanged
		}
		sh.Video.Status = "processing"
		sh.Video.Progress = 5
		return nil
	}); e != nil {
		if errors.Is(e, dependencyChanged) {
			return f.failWith(c, j, in, true, "VIDEO_DEPENDENCY_NOT_READY", "วิดีโอ shot ก่อนหน้าเปลี่ยน กรุณารอให้สำเร็จแล้วลองใหม่", nil)
		}
		return e
	}
	state.Phase = "submitting"
	// Signed URLs are transient; store only the owned object key in checkpoints.
	request := state.Request
	state.Request.Content = append([]modelark.ContentItem(nil), state.Request.Content...)
	state.Request.Content[1].URL = state.Key
	if e = f.syntheticCheckpoint(c, j, state); e != nil {
		return e
	}
	directorSyntheticStage(j, in, "video_submit_start", state.Version)
	id, e := f.video.CreateTask(c, request)
	if e != nil {
		directorSyntheticStage(j, in, "video_submit_failed", state.Version)
		d := modelark.VideoFailureDiagnostic(e)
		definitive := d.HTTPStatus >= 400 && d.HTTPStatus < 500 || errors.Is(e, modelark.ErrNotSubmitted)
		code, message := modelark.VideoFailure(e)
		return f.failWith(c, j, in, definitive, code, message, e)
	}
	if strings.TrimSpace(id) == "" {
		return f.fail(c, j, in, false)
	}
	directorSyntheticStage(j, in, "video_submitted", state.Version)
	raw, e := json.Marshal(directorVideoTask{TaskID: id, SubmittedAt: now, SourceClipKey: state.SourceClipKey, FirstImageKey: state.SourceKey})
	if e != nil {
		return e
	}
	if e = f.store.RecordOutput(c, j, raw); e != nil {
		return e
	}
	j.Output = raw
	return f.store.Wait(c, j, raw, now.Add(directorVideoPoll))
}

const directorFilmedNeutralVersion = "director-filmed-neutral-v1"

// Library and storyboard text often names an art style ("3D", "anime"); in the filmed path those
// words would pull both the synthetic frame and the video back toward a rendered look.
var directorFilmedStyleTerms = regexp.MustCompile(`(?i)\b(?:3[- ]?d(?:[- ](?:render(?:ed|ing)?|animat(?:ed|ion)|model(?:ed)?|art|style|cartoon))*(?:[- ]character)?|cgi|pixar(?:[- ]style)?|disney(?:[- ]style)?|anime(?:[- ]style)?|manga|cartoon(?:ish|y|[- ]style)?|chibi|cel[- ]shaded|toon(?:[- ]shaded)?|stylized|stylised|illustrat(?:ed|ion)|game[- ]engine|unreal engine|claymation)\b|สามมิติ|3 มิติ|การ์ตูน|อนิเมะ|แอนิเมชัน|อนิเมชัน`)
var directorFilmedSpaces = regexp.MustCompile(`[ \t]{2,}`)
var directorFilmedEmptyList = regexp.MustCompile(`(?:,\s*){2,}`)

func directorRecipeLabel(recipe string) string {
	if recipe == "" {
		return "faceless"
	}
	return recipe
}

func directorFilmedNeutralize(text string) string {
	text = directorFilmedStyleTerms.ReplaceAllString(text, "")
	text = directorFilmedEmptyList.ReplaceAllString(text, ", ")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = directorFilmedSpaces.ReplaceAllString(line, " ")
		line = strings.ReplaceAll(line, " ,", ",")
		line = strings.ReplaceAll(strings.ReplaceAll(line, "( )", ""), "()", "")
		lines[i] = strings.TrimRight(strings.TrimSpace(line), ",")
	}
	return strings.Join(lines, "\n")
}
