package facade_open_api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/google/uuid"
)

func TestPipelineImageEnqueuesDedicatedCompilerAndFreezesReplay(t *testing.T) {
	t.Parallel()
	var v6ImagePrompt string
	var v6Snapshot string
	for _, version := range []string{"", characterdesign.PipelineCompilerVersion, characterdesign.PipelineRealisticCompilerVersion, characterdesign.PipelineSoftRealisticCompilerVersion, characterdesign.PipelineSemiRealisticCompilerVersion, characterdesign.PipelinePolished3DCompilerVersion, characterdesign.PipelineStylized3DCompilerVersion, promptcompiler.PipelineCinematicVideoVersion, promptcompiler.PipelineFilmedHumanVideoVersion} {
		t.Run(version, func(t *testing.T) {
			owner, id := uuid.New(), uuid.New()
			r := &assetRepoFake{}
			uploads := &characterReferenceUploads{owner: owner.String(), upload: &models.Upload{Base: models.Base{ID: id}, MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
			f := NewStudioFacade(r, nil, uploads, nil, "fixture", time.Hour, "image-fixture", 3)
			ref := id.String()
			in := req.CreateImageRequest{Category: "character", VisualStyle: "cinematic_realistic", Prompt: "man in a suit", AspectRatio: "9:16", ReferenceImageID: &ref, IdempotencyKey: uuid.NewString()}
			if _, err := f.CreatePipelineImage(t.Context(), owner.String(), in, "pinned-model", version); err != nil {
				t.Fatal(err)
			}
			job := r.queued
			var snap struct{ Model, CompilerVersion, ImageGenerationMode string }
			if err := json.Unmarshal(job.EffectiveInputs, &snap); err != nil {
				t.Fatal(err)
			}
			expected := characterdesign.CompilerVersion
			if version != "" {
				expected = version
			}
			if version == promptcompiler.PipelineCinematicVideoVersion || version == promptcompiler.PipelineFilmedHumanVideoVersion {
				expected = characterdesign.PipelineStylized3DCompilerVersion
			}
			if snap.Model != "pinned-model" || snap.CompilerVersion != expected || snap.ImageGenerationMode != "image_to_image" || job.UploadID == nil || *job.UploadID != id || job.Prompt != in.Prompt {
				t.Fatalf("wrong frozen image inputs: %+v", snap)
			}
			if strings.Contains(*job.CompiledPrompt, "Clearly stylized CG") != (version == characterdesign.PipelineCompilerVersion) {
				t.Fatal("pipeline compiler selection incorrect")
			}
			if version == characterdesign.PipelineRealisticCompilerVersion {
				for _, want := range []string{"Lifelike digital human", "similar overall facial structure", "subtle synthetic rendering treatment"} {
					if !strings.Contains(*job.CompiledPrompt, want) {
						t.Errorf("v2 image prompt missing %q", want)
					}
				}
				if strings.Contains(*job.CompiledPrompt, "Redesign facial geometry") {
					t.Fatal("v2 image still redesigns face")
				}
			}
			if version == characterdesign.PipelineSoftRealisticCompilerVersion {
				for _, want := range []string{"similar overall facial structure", "subtly softened, gently smoothed skin", "lightly illustrated digital finish", "realistic hair, clothing", "Omit fine photographic skin pores"} {
					if !strings.Contains(*job.CompiledPrompt, want) {
						t.Errorf("v3 image prompt missing %q", want)
					}
				}
				for _, forbidden := range []string{"Redesign facial geometry", "for a photorealistic cinematic production", "Clearly stylized CG"} {
					if strings.Contains(*job.CompiledPrompt, forbidden) {
						t.Errorf("conflicting v3 image instruction: %q", forbidden)
					}
				}
			}
			if version == characterdesign.PipelineSemiRealisticCompilerVersion {
				for _, want := range []string{"Clearly authored semi-realistic digital character", "smooth stylized skin", "slightly sculpted clean facial planes", "natural human facial and body proportions", "Preserve recognizable broad facial structure"} {
					if !strings.Contains(*job.CompiledPrompt, want) {
						t.Errorf("v4 image prompt missing %q", want)
					}
				}
				if strings.Contains(*job.CompiledPrompt, "Soften only the surface rendering slightly") || strings.Contains(*job.CompiledPrompt, "Redesign facial geometry") {
					t.Fatal("v4 image treatment incorrect")
				}
			}
			if version == characterdesign.PipelinePolished3DCompilerVersion {
				for _, want := range []string{"Polished semi-realistic 3D digital character", "softly smoothed skin", "continuous gentle facial shading", "defined softly shaded locks and finer strands", "realistic fabric texture, seams, folds and thickness", "relative eye size and spacing", "generic idealized face"} {
					if !strings.Contains(*job.CompiledPrompt, want) {
						t.Errorf("v5 image prompt missing %q", want)
					}
				}
				if strings.Contains(*job.CompiledPrompt, "Redesign facial geometry") || strings.Contains(*job.CompiledPrompt, "slightly sculpted clean facial planes") {
					t.Fatal("conflicting v5 face redesign")
				}
			}
			if version == characterdesign.PipelineStylized3DCompilerVersion || version == promptcompiler.PipelineCinematicVideoVersion || version == promptcompiler.PipelineFilmedHumanVideoVersion {
				for _, want := range []string{"Visibly stylized 3D fictional character", "smooth matte skin and simplified microtexture", "slightly stylized facial forms", "detailed stylized locks", "simplified fine weave", "Preserve broad face shape", "not a photographic portrait or a retouched photo"} {
					if !strings.Contains(*job.CompiledPrompt, want) {
						t.Errorf("v6 image prompt missing %q", want)
					}
				}
				if strings.Contains(*job.CompiledPrompt, "Polished semi-realistic 3D digital character") || strings.Contains(*job.CompiledPrompt, "defined softly shaded locks and finer strands") {
					t.Fatal("v5 image direction persisted in v6")
				}
			}
			if version == characterdesign.PipelineStylized3DCompilerVersion {
				v6ImagePrompt, v6Snapshot = *job.CompiledPrompt, string(job.EffectiveInputs)
			}
			if (version == promptcompiler.PipelineCinematicVideoVersion || version == promptcompiler.PipelineFilmedHumanVideoVersion) && (*job.CompiledPrompt != v6ImagePrompt || string(job.EffectiveInputs) != v6Snapshot) {
				t.Fatal("video-only version altered image prompt or effective inputs")
			}
			before, prompt := string(job.EffectiveInputs), *job.CompiledPrompt
			replay := NewStudioFacade(r, &assetJobFake{job: job}, nil, nil, "fixture", time.Hour, "", 3)
			again, err := replay.CreatePipelineImage(t.Context(), owner.String(), in, "changed-model", version)
			if err != nil || again.JobID != job.ID.String() || before != string(job.EffectiveInputs) || prompt != *job.CompiledPrompt {
				t.Fatal("image replay mutated frozen input", err)
			}
		})
	}
}

func TestPipelineRenderingVersionSurvivesRestartAndLegacyResume(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"", characterdesign.PipelineCompilerVersion, characterdesign.PipelineRealisticCompilerVersion, characterdesign.PipelineSoftRealisticCompilerVersion, characterdesign.PipelineSemiRealisticCompilerVersion, characterdesign.PipelinePolished3DCompilerVersion, characterdesign.PipelineStylized3DCompilerVersion, promptcompiler.PipelineCinematicVideoVersion, promptcompiler.PipelineFilmedHumanVideoVersion} {
		f, m, images, videos, user, in := pipelineFixture(t)
		in.ScenePrompt = strings.Repeat("ก", 2000)
		in.Dialogue = ""
		if _, err := f.Create(t.Context(), user, in); err != nil {
			t.Fatal("max scene should remain accepted", err)
		}
		var frozen pipelineSnapshot
		if err := json.Unmarshal(m.p.Inputs, &frozen); err != nil {
			t.Fatal(err)
		}
		if frozen.RenderingVersion != promptcompiler.PipelineFilmedHumanVideoVersion {
			t.Fatal("new request version not pinned")
		}
		if version != promptcompiler.PipelineFilmedHumanVideoVersion {
			frozen.RenderingVersion = version
			m.p.Inputs, _ = json.Marshal(frozen)
		}
		restarted := *f
		runPipelineTicks(t, &restarted, 8)
		if images.renderingVersion != frozen.RenderingVersion || videos.seen.pipelineRenderingVersion != frozen.RenderingVersion || videos.seen.Prompt != in.ScenePrompt {
			t.Fatal("frozen rendering or raw prompt changed")
		}
	}
}

func TestPipelineExplicitArtStylesKeepFrozenCGMode(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"anime", "clay", "illustration", "3d_animation"} {
		f, m, _, _, user, in := pipelineFixture(t)
		in.VisualStyle = style
		if _, err := f.Create(t.Context(), user, in); err != nil {
			t.Fatal(err)
		}
		var frozen pipelineSnapshot
		if err := json.Unmarshal(m.p.Inputs, &frozen); err != nil {
			t.Fatal(err)
		}
		if frozen.RenderingVersion != characterdesign.PipelineCompilerVersion {
			t.Fatalf("explicit %s style lost", style)
		}
	}
}

func TestPipelineV6VideoPromptRemainsByteStable(t *testing.T) {
	t.Parallel()
	registry, _ := modelark.NewRegistry("")
	model, _ := registry.Lookup("violet-motion-2")
	result, err := promptcompiler.Compile(promptcompiler.Inputs{PipelineRenderingVersion: characterdesign.PipelineStylized3DCompilerVersion, ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "walk through a garden"}, Assets: []promptcompiler.AssetRef{{StorageKey: "fixture.png", Role: "reference", MimeType: "image/png"}}}, model)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(result.CompiledPrompt))); got != "3fcf6b4b2a1935a39f487f07df3a6762878e46f10773d858c5a074a3ada8476b" {
		t.Fatalf("frozen v6 video prompt changed: %s", got)
	}
}

func TestPipelineCinematicVideoPreservesDialogueBudgetAndSceneCamera(t *testing.T) {
	t.Parallel()
	registry, _ := modelark.NewRegistry("")
	model, _ := registry.Lookup("violet-motion-2")
	in := promptcompiler.Inputs{PipelineRenderingVersion: characterdesign.PipelineStylized3DCompilerVersion, ModelID: "violet-motion-2", Quality: "standard", Dialogue: "hello", Scene: &promptcompiler.SceneInputs{Description: "x", CameraOverride: "static medium shot"}, Assets: []promptcompiler.AssetRef{{StorageKey: "fixture.png", Role: "reference", MimeType: "image/png"}}}
	old, err := promptcompiler.Compile(in, model)
	if err != nil {
		t.Fatal(err)
	}
	in.Scene.Description = strings.Repeat("x", 2000-utf8.RuneCountInString(old.CompiledPrompt)+1)
	old, err = promptcompiler.Compile(in, model)
	if err != nil || utf8.RuneCountInString(old.CompiledPrompt) != 2000 {
		t.Fatal("old prompt budget fixture invalid", err)
	}
	in.PipelineRenderingVersion = promptcompiler.PipelineCinematicVideoVersion
	result, err := promptcompiler.Compile(in, model)
	if err != nil {
		t.Fatal("cinematic controls reduced dialogue budget", err)
	}
	if len(result.ContentItems) != 2 || result.ContentItems[1].Role != modelark.RoleReferenceImage || result.Resolution != old.Resolution || result.Snapshot.Dialogue != in.Dialogue || result.Snapshot.PipelineRenderingVersion != promptcompiler.PipelineCinematicVideoVersion {
		t.Fatal("video parameters or snapshot changed")
	}
	for _, want := range []string{in.Scene.Description, "static medium shot", "follow scene camera/framing", "hello", "No CG/cartoon/plastic/doll"} {
		if !strings.Contains(result.CompiledPrompt, want) {
			t.Errorf("compiled cinematic prompt missing %q", want)
		}
	}
	if utf8.RuneCountInString(result.CompiledPrompt) > utf8.RuneCountInString(old.CompiledPrompt) {
		t.Fatal("cinematic controls expanded old prompt budget")
	}
}

func TestPipelineV7VideoPromptRemainsByteStable(t *testing.T) {
	t.Parallel()
	registry, _ := modelark.NewRegistry("")
	model, _ := registry.Lookup("violet-motion-2")
	result, err := promptcompiler.Compile(promptcompiler.Inputs{PipelineRenderingVersion: promptcompiler.PipelineCinematicVideoVersion, ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "walk through a garden"}, Assets: []promptcompiler.AssetRef{{StorageKey: "fixture.png", Role: "reference", MimeType: "image/png"}}}, model)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(result.CompiledPrompt))); got != "7470626036a8b09d178ba7577ec1202d1f3dc77ae8f437e64e6e314c9ebec1d1" {
		t.Fatalf("frozen v7 video prompt changed: %s", got)
	}
}

func TestPipelineFilmedHumanBudgetPreservesV7AndScene(t *testing.T) {
	t.Parallel()
	registry, _ := modelark.NewRegistry("")
	model, _ := registry.Lookup("violet-motion-2")
	in := promptcompiler.Inputs{PipelineRenderingVersion: promptcompiler.PipelineCinematicVideoVersion, ModelID: "violet-motion-2", Quality: "standard", Dialogue: "hello", Scene: &promptcompiler.SceneInputs{Description: "x", CameraOverride: "static medium shot", SettingOverride: "a rainy city street"}, Assets: []promptcompiler.AssetRef{{StorageKey: "fixture.png", Role: "reference", MimeType: "image/png"}}}
	old, err := promptcompiler.Compile(in, model)
	if err != nil {
		t.Fatal(err)
	}
	in.Scene.Description = strings.Repeat("x", 2000-utf8.RuneCountInString(old.CompiledPrompt)+1)
	old, err = promptcompiler.Compile(in, model)
	if err != nil || utf8.RuneCountInString(old.CompiledPrompt) != 2000 {
		t.Fatal("v7 boundary fixture invalid", err)
	}
	in.PipelineRenderingVersion = promptcompiler.PipelineFilmedHumanVideoVersion
	result, err := promptcompiler.Compile(in, model)
	if err != nil {
		t.Fatal("v8 reduced v7 dialogue budget", err)
	}
	if len(result.ContentItems) != 2 || result.ContentItems[1].Role != modelark.RoleReferenceImage || result.Resolution != old.Resolution || result.Snapshot.Dialogue != in.Dialogue || result.Snapshot.PipelineRenderingVersion != promptcompiler.PipelineFilmedHumanVideoVersion {
		t.Fatal("video parameters or snapshot changed")
	}
	for _, want := range []string{in.Scene.Description, "static medium shot", "a rainy city street", "follow scene setting, action and camera", "spoken lip sync", "hello"} {
		if !strings.Contains(result.CompiledPrompt, want) {
			t.Errorf("v8 lost scene/dialogue control %q", want)
		}
	}
	if utf8.RuneCountInString(result.CompiledPrompt) > utf8.RuneCountInString(old.CompiledPrompt) {
		t.Fatal("v8 combined controls increased prompt budget")
	}
	in.Scene.Description = strings.Repeat("x", 2000)
	if _, err = promptcompiler.Compile(in, model); !errors.Is(err, promptcompiler.ErrPromptBudget) {
		t.Fatal("over-budget dialogue no longer reports established error", err)
	}
}
