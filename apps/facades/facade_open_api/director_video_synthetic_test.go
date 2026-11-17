package facade_open_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

type syntheticTestStore struct {
	claimed *models.DirectorJob
	*continuityStore
	persisted []byte
	failWait  bool
}

func (s *syntheticTestStore) RecordOutput(_ context.Context, _ *models.DirectorJob, b []byte) error {
	s.persisted = bytes.Clone(b)
	return nil
}
func (s *syntheticTestStore) Wait(c context.Context, j *models.DirectorJob, b []byte, until time.Time) error {
	if s.failWait {
		return errors.New("wait unavailable")
	}
	return s.continuityStore.Wait(c, j, b, until)
}

type syntheticTestImages struct {
	onGenerate func()
	calls      []modelark.ImageRequest
	invalid    bool
	failure    error
}

func (i *syntheticTestImages) GenerateImage(_ context.Context, r modelark.ImageRequest) ([]byte, error) {
	i.calls = append(i.calls, r)
	if i.onGenerate != nil {
		i.onGenerate()
	}
	if i.failure != nil {
		return nil, i.failure
	}
	if i.invalid {
		return []byte("invalid"), nil
	}
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	return b.Bytes(), nil
}

type syntheticTestMedia struct {
	onSign func(string)
	*continuityMedia
	failUpload bool
}

func (m *syntheticTestMedia) SignedURL(c context.Context, k string, ttl time.Duration) (string, time.Time, error) {
	if m.onSign != nil {
		m.onSign(k)
	}
	return m.continuityMedia.SignedURL(c, k, ttl)
}
func (m *syntheticTestMedia) Upload(c context.Context, k string, b []byte, kind string) error {
	if m.failUpload {
		return errors.New("storage unavailable")
	}
	return m.continuityMedia.Upload(c, k, b, kind)
}
func syntheticFixture(t *testing.T) (*DirectorFacade, *syntheticTestStore, *modelark.MockClient, *syntheticTestImages, *syntheticTestMedia, *models.DirectorJob, directorJobInput) {
	t.Helper()
	f, s, v, m, j, in := newContinuityFixture(t)
	store := &syntheticTestStore{continuityStore: s}
	images := &syntheticTestImages{}
	media := &syntheticTestMedia{continuityMedia: m}
	f.store = store
	f.images = images
	f.storage = media
	d := continuityDoc(t, s)
	d.Project.Style = "realistic"
	d.Project.Scenes[0].Shots[0].CharacterAssetIDs = []string{"cast"}
	d.Project.Scenes[0].Shots[0].Description = "actor walks through the scene"
	_ = directorEncode(&s.row, &d)
	in.Project = d.Project
	in.ImageModel = "seedream-test"
	in.VideoModel = "dreamina-seedance-2-0-fast-260128"
	in.Characters = []directorCharacter{{ID: "cast", Name: "actor", Description: "distinctive green coat", Key: "raw-cast.png"}}
	return f, store, v, images, media, j, in
}
func TestDirectorSyntheticVideoUsesOnlyPreparedScene(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	before := bytes.Clone(s.row.Document)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 1 || len(img.calls[0].ReferenceURLs) != 1 || !strings.HasSuffix(img.calls[0].ReferenceURLs[0], "a.png") {
		t.Fatalf("image requests: %+v", img.calls)
	}
	p := img.calls[0].Prompt
	for _, bad := range []string{"Plain light-gray studio background", "single full-body", "No typography, logos, watermarks or scene staging"} {
		if strings.Contains(p, bad) {
			t.Fatalf("contradictory scene prompt: %s", p)
		}
	}
	if !strings.Contains(p, "Re-render only the characters") || !strings.Contains(p, "plain light-gray studio background") || !strings.Contains(p, "exactly in their current pose") {
		t.Fatal(p)
	}
	if len(v.CreatedRequests) != 1 || len(v.CreatedRequests[0].Content) != 2 || !strings.Contains(v.CreatedRequests[0].Content[1].URL, "-synthetic.png") || strings.Contains(v.CreatedRequests[0].Content[0].Text, "exact first frame") || !strings.Contains(v.CreatedRequests[0].Content[0].Text, "Filmed live-action human actor look") {
		t.Fatalf("video request: %+v", v.CreatedRequests)
	}
	if strings.Contains(v.CreatedRequests[0].Content[1].URL, "raw-cast") || !strings.Contains(v.CreatedRequests[0].Content[0].Text, "distinctive green coat") {
		t.Fatal("selected cast not preserved as description only")
	}
	if text := v.CreatedRequests[0].Content[0].Text; !strings.Contains(text, "pose, gesture, expression and relative placement") || strings.Contains(text, "Preserve the reference image's broad framing, composition, environment") || strings.Contains(text, "Exclude its shading, CG materials, drawn skin, pose and background") {
		t.Fatal("pose casting must keep pose but take the environment from text, not from the reference")
	}
	var old directorDocument
	_ = json.Unmarshal(before, &old)
	d := continuityDoc(t, s.continuityStore)
	if d.Keys["a"] != old.Keys["a"] || d.Project.Scenes[0].Shots[0].Image != old.Project.Scenes[0].Shots[0].Image {
		t.Fatal("storyboard changed")
	}
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 1 || len(v.CreatedRequests) != 1 {
		t.Fatal("poll repeated generation")
	}
}
func TestDirectorSyntheticVideoStorageRecoveryDoesNotGenerateAgain(t *testing.T) {
	t.Parallel()
	f, s, v, img, m, j, in := syntheticFixture(t)
	m.failUpload = true
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 0 {
		t.Fatal("submitted before durable image")
	}
	var state directorSyntheticState
	_ = json.Unmarshal(s.persisted, &state)
	if state.Phase != "prepared" || state.SourceKey != "a.png" || state.Version != directorSyntheticVersion || state.ImageModel != in.ImageModel {
		t.Fatalf("checkpoint %+v", state)
	}
	m.failUpload = false
	j.Output = bytes.Clone(s.persisted)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 1 || len(v.CreatedRequests) != 1 {
		t.Fatal("storage recovery repeated generation")
	}
}
func TestDirectorSyntheticVideoAcceptedTaskSurvivesWaitFailure(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	s.failWait = true
	if e := f.directorVideo(context.Background(), j, in); e == nil {
		t.Fatal("expected wait error")
	}
	var task directorVideoTask
	_ = json.Unmarshal(s.persisted, &task)
	if task.TaskID == "" {
		t.Fatal("accepted task not checkpointed")
	}
	s.failWait = false
	j.Output = bytes.Clone(s.persisted)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 1 || len(v.CreatedRequests) != 1 {
		t.Fatal("accepted task repeated")
	}
}
func TestDirectorSyntheticVideoUncertainMarkersNeverRepeat(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"generating", "submitting"} {
		t.Run(phase, func(t *testing.T) {
			f, s, v, img, _, j, in := syntheticFixture(t)
			j.Output, _ = json.Marshal(directorSyntheticState{Phase: phase})
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if j.Status != "needs_attention" || s.refunds != 0 || len(img.calls) != 0 || len(v.CreatedRequests) != 0 {
				t.Fatal("uncertain external call repeated or refunded")
			}
		})
	}
}
func TestDirectorSyntheticVideoInvalidIntermediateNeverFallsBack(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	img.invalid = true
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if j.Status != "failed" || s.refunds != 1 || len(v.CreatedRequests) != 0 {
		t.Fatal("invalid intermediate used raw fallback")
	}
}

// Later shots of a realistic/cinematic scene continue from the previous clip (user decision 2026-11-09),
// so a failed predecessor blocks them instead of re-casting their own storyboard frame.
func TestDirectorSyntheticLaterShotNeedsPredecessor(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	d := continuityDoc(t, s.continuityStore)
	d.Project.Scenes[0].Shots[0].Video.Status = "failed"
	_ = directorEncode(&s.row, &d)
	in.ShotID = "b"
	in.ImageKey = "b.png"
	in.Generation = "gb"
	j.TargetID = "video:b"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 0 || len(v.CreatedRequests) != 0 {
		t.Fatalf("later shot must not be cast or submitted without its predecessor: images=%+v videos=%+v", img.calls, v.CreatedRequests)
	}
}
func TestDirectorSyntheticVideoFinalizeBypassesPreparation(t *testing.T) {
	t.Parallel()
	f, _, v, img, _, j, in := syntheticFixture(t)
	in.DraftTaskID = "draft-task"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 0 || len(v.CreatedRequests) != 1 || v.CreatedRequests[0].Content[0].DraftTaskID != "draft-task" {
		t.Fatal("finalize regenerated synthetic image")
	}
}

func (s *syntheticTestStore) Claim(context.Context) (*models.DirectorJob, error) {
	return s.claimed, nil
}

func TestDirectorSyntheticTickReclaimsPreparedAndUncertainPhases(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"prepared", "generating", "submitting"} {
		t.Run(phase, func(t *testing.T) {
			f, s, v, img, m, j, in := syntheticFixture(t)
			if phase == "prepared" {
				m.failUpload = true
				if e := f.directorVideo(context.Background(), j, in); e != nil {
					t.Fatal(e)
				}
				m.failUpload = false
			} else {
				j.Output, _ = json.Marshal(directorSyntheticState{Phase: phase})
			}
			j.Status = "submitting"
			j.Snapshot, _ = json.Marshal(in)
			s.claimed = j
			if e := f.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if phase == "prepared" {
				if len(img.calls) != 1 || len(v.CreatedRequests) != 1 {
					t.Fatal("reclaim repeated image or failed video")
				}
			} else if len(img.calls) != 0 || len(v.CreatedRequests) != 0 || j.Status != "needs_attention" {
				t.Fatal("reclaim repeated ambiguous call")
			}
		})
	}
}
func TestDirectorSyntheticConversionFailureNeverUsesRawReference(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	img.failure = context.DeadlineExceeded
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 0 || s.refunds != 0 || j.Status != "needs_attention" {
		t.Fatal("ambiguous image failure fell back or refunded")
	}
}
func TestDirectorNonRealisticVideoSkipsSyntheticPreparation(t *testing.T) {
	t.Parallel()
	f, _, v, img, _, j, in := syntheticFixture(t)
	in.Project.Style = "anime"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 0 || len(v.CreatedRequests) != 1 || v.CreatedRequests[0].Content[1].URL != "https://owned.test/a.png" {
		t.Fatal("nonrealistic flow changed")
	}
}

func TestDirectorSyntheticVideoTimeoutKeepsUncertainSubmission(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	v.CreateErr = context.DeadlineExceeded
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if j.Status != "needs_attention" || s.refunds != 0 {
		t.Fatal("uncertain submit refunded")
	}
	var state directorSyntheticState
	_ = json.Unmarshal(s.persisted, &state)
	if state.Phase != "submitting" {
		t.Fatal("missing durable submission marker")
	}
	j.Output = bytes.Clone(s.persisted)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 1 {
		t.Fatal("uncertain submit regenerated image")
	}
}

func TestDirectorFilmedLooksMapFromStyleAndKeepSyntheticPrepass(t *testing.T) {
	t.Parallel()
	prompts := map[string]string{}
	imagePrompts := map[string]string{}
	for _, style := range []string{"realistic", "cinematic"} {
		f, s, v, img, _, j, in := syntheticFixture(t)
		d := continuityDoc(t, s.continuityStore)
		d.Project.Style = style
		in.Project = d.Project
		_ = directorEncode(&s.row, &d)
		if e := f.directorVideo(context.Background(), j, in); e != nil {
			t.Fatal(e)
		}
		if len(img.calls) != 1 || len(v.CreatedRequests) != 1 {
			t.Fatal("staged generation changed")
		}
		prompts[style] = v.CreatedRequests[0].Content[0].Text
		imagePrompts[style] = img.calls[0].Prompt
		expectedScene := "actor walks through the scene\nCharacter: actor: distinctive green coat\nCamera: ; movement: subtle cinematic camera movement\n" + directorObjectIntegrity + "\n" + directorFaceIntegrity
		if imagePrompts[style] != characterdesign.CompileDirectorFacelessCasting(expectedScene) {
			t.Fatalf("%s image prompt differs from original compiler bytes", style)
		}
		want := "REALISTIC MODE — AUTHENTIC OBSERVATIONAL FOOTAGE"
		if style == "cinematic" {
			want = "CINEMATIC MODE — PHOTOREALISTIC LIVE-ACTION CINEMATOGRAPHY"
		}
		if !strings.Contains(prompts[style], want) || !strings.Contains(prompts[style], "Image 1: casting and pose guide") || strings.Contains(prompts[style], "Image 1 is the primary reference") {
			t.Fatalf("selected look missing for %s: %s", style, prompts[style])
		}
		if !strings.Contains(imagePrompts[style], "Remove the entire environment") || !strings.Contains(prompts[style], "Scene environment:") || !strings.Contains(prompts[style], "cast each character as a real person") {
			t.Fatal("Director must keep the synthetic frame stylized and move realism into the video prompt")
		}
		for _, detail := range []string{"skin with subtle texture and translucency, individual hair and real cloth", "Exclude its shading, CG materials, drawn skin, lighting and gray background", "Negative constraints:"} {
			if !strings.Contains(prompts[style], detail) {
				t.Fatalf("video-only detail missing %q", detail)
			}
		}

	}
	if prompts["realistic"] == prompts["cinematic"] || imagePrompts["realistic"] != imagePrompts["cinematic"] {
		t.Fatal("video looks not distinct or image prepass drifted")
	}
}
func TestDirectorPreparedLegacyFilmedRequestResumesWithoutRecompile(t *testing.T) {
	t.Parallel()
	for _, version := range []string{directorSyntheticLegacyVersion, directorSyntheticPreviousVersion, directorSyntheticMaterialVersion, directorSyntheticVideoOnlyVersion, directorSyntheticTemplateVersion, directorSyntheticLayoutVersion, directorSyntheticRealCGVersion, directorSyntheticStylizedVersion} {
		t.Run(version, func(t *testing.T) {
			f, _, v, img, m, j, in := syntheticFixture(t)
			m.failUpload = true
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			var state directorSyntheticState
			if e := json.Unmarshal(j.Output, &state); e != nil {
				t.Fatal(e)
			}
			state.Version = version
			state.Look = ""
			state.Request.Content[0].Text = "frozen legacy filmed direction"
			j.Output, _ = json.Marshal(state)
			m.failUpload = false
			in.Project.Style = "cinematic"
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if len(img.calls) != 1 || len(v.CreatedRequests) != 1 || v.CreatedRequests[0].Content[0].Text != "frozen legacy filmed direction" {
				t.Fatal("legacy prepared job regenerated or changed its frozen prompt")
			}
		})
	}
}

func TestDirectorFilmedStageLogsKnownFrozenVersionOnly(t *testing.T) {
	t.Parallel()
	_, _, _, _, _, j, in := syntheticFixture(t)
	line := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticLegacyVersion)
	if !strings.Contains(line, "pipeline="+directorSyntheticLegacyVersion+" ") || strings.Contains(line, directorSyntheticVersion) {
		t.Fatal("legacy request mislabeled as the new look version")
	}
	previous := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticPreviousVersion)
	if !strings.Contains(previous, "pipeline="+directorSyntheticPreviousVersion+" ") {
		t.Fatal("v3 frozen request mislabeled")
	}
	material := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticMaterialVersion)
	if !strings.Contains(material, "pipeline="+directorSyntheticMaterialVersion+" ") {
		t.Fatal("frozen v4 request mislabeled")
	}
	template := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticTemplateVersion)
	if !strings.Contains(template, "pipeline="+directorSyntheticTemplateVersion+" ") || strings.Contains(template, directorSyntheticVersion) {
		t.Fatal("frozen v6 request mislabeled")
	}
	fresh := directorSyntheticStage(j, in, "preparation_start")
	layout := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticLayoutVersion)
	if !strings.Contains(layout, "pipeline="+directorSyntheticLayoutVersion+" ") || !strings.HasSuffix(directorSyntheticLayoutVersion, "/director-filmed-layout-v2") {
		t.Fatal("frozen v7 request mislabeled")
	}
	realCG := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticRealCGVersion)
	if !strings.Contains(realCG, "pipeline="+directorSyntheticRealCGVersion+" ") || !strings.HasPrefix(directorSyntheticRealCGVersion, "director-scene-3d-v8/director-filmed-scene-cg-v1/") || !strings.Contains(directorSyntheticRealCGVersion, "/director-filmed-layout-v3/") {
		t.Fatal("frozen v8 request mislabeled")
	}
	stylized := directorSyntheticStage(j, in, "video_submit_start", directorSyntheticStylizedVersion)
	if !strings.Contains(stylized, "pipeline="+directorSyntheticStylizedVersion+" ") || !strings.HasPrefix(directorSyntheticStylizedVersion, "director-scene-3d-v9/director-filmed-scene-stylized-v2/") || !strings.Contains(directorSyntheticStylizedVersion, "/director-filmed-layout-v4/") {
		t.Fatal("frozen v9 request mislabeled")
	}
	if !strings.Contains(fresh, "pipeline="+directorSyntheticVersion+" ") || !strings.HasPrefix(directorSyntheticVersion, "director-scene-3d-v10/director-pose-casting-3d-v1/") || !strings.Contains(directorSyntheticVersion, "/director-pose-casting-v1/") {
		t.Fatal("new jobs must carry the v10 fingerprint")
	}
	unknown := directorSyntheticStage(j, in, "video_submit_start", "untrusted-version\nsecret-payload")
	if !strings.Contains(unknown, "pipeline=unknown ") || strings.Contains(unknown, "untrusted") || strings.Contains(unknown, "secret-payload") {
		t.Fatal("untrusted pipeline version logged")
	}
}

func TestDirectorSyntheticCheckpointCannotSubmitRawOrUnownedReference(t *testing.T) {
	t.Parallel()
	for _, version := range []string{directorSyntheticLegacyVersion, directorSyntheticPreviousVersion, directorSyntheticMaterialVersion, directorSyntheticVideoOnlyVersion, directorSyntheticTemplateVersion, directorSyntheticLayoutVersion, directorSyntheticRealCGVersion, directorSyntheticStylizedVersion, directorSyntheticVersion} {
		for _, kind := range []string{"raw_key", "foreign_key", "wrong_role", "extra_image"} {
			t.Run(version+"/"+kind, func(t *testing.T) {
				f, s, v, img, m, j, in := syntheticFixture(t)
				m.failUpload = true
				if e := f.directorVideo(context.Background(), j, in); e != nil {
					t.Fatal(e)
				}
				var state directorSyntheticState
				_ = json.Unmarshal(j.Output, &state)
				state.Version = version
				switch kind {
				case "raw_key":
					state.Key = in.ImageKey
				case "foreign_key":
					state.Key = "director/foreign/project/shot/job-synthetic.png"
				case "wrong_role":
					state.Request.Content[1].Role = modelark.RoleFirstFrame
				case "extra_image":
					state.Request.Content = append(state.Request.Content, modelark.ContentItem{Type: modelark.ContentTypeImageURL, URL: "raw-cast.png", Role: modelark.RoleReferenceImage})
				}
				j.Output, _ = json.Marshal(state)
				m.failUpload = false
				if e := f.directorVideo(context.Background(), j, in); e != nil {
					t.Fatal(e)
				}
				sh := continuityDoc(t, s.continuityStore).Project.Scenes[0].Shots[0]
				if len(v.CreatedRequests) != 0 || len(img.calls) != 1 || j.Status != "needs_attention" || s.refunds != 0 {
					t.Fatalf("unverified reference submitted or regenerated: %+v", sh.Video)
				}
			})
		}
	}
}

func TestDirectorFilmedNeutralizeRemovesArtStyleWords(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"young swordsman, 3D animated character, navy robe": "young swordsman, navy robe",
		"Pixar-style girl with anime eyes and a red scarf":  "girl with eyes and a red scarf",
		"ตัวละครการ์ตูนสามมิติ ผมยาว":                       "ตัวละคร ผมยาว",
		"Stylized CGI hero (3d)":                            "hero",
		"walks 3 days later in a 3-door car":                "walks 3 days later in a 3-door car",
		"Character: Mali: late 20s\nCamera: medium shot":    "Character: Mali: late 20s\nCamera: medium shot",
	}
	for in, want := range cases {
		if got := directorFilmedNeutralize(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
