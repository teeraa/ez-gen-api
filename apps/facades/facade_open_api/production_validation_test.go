package facade_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
	"math"
	"testing"
)

func TestProductionTrimValidation(t *testing.T) {
	for _, tc := range []struct {
		start, end float64
		ok         bool
	}{{0, 0, true}, {0, .5, true}, {.25, .75, true}, {-1, 0, false}, {1, 0, false}, {0, 1.5, false}, {.75, .5, false}, {math.NaN(), 0, false}, {0, math.Inf(1), false}} {
		if validTrim(tc.start, tc.end, 1000) != tc.ok {
			t.Fatalf("trim %v %v", tc.start, tc.end)
		}
	}
}
func TestProductionCapabilityValidation(t *testing.T) {
	registry, _ := modelark.NewRegistry("")
	fast, _ := registry.Lookup("violet-fast")
	v := models.VideoSettings{Mode: "text", AspectRatio: "16:9", Duration: 5, Quality: "high"}
	if ValidateVideoSettings(v, fast) != ErrModelInvalid {
		t.Fatal("fast must not accept unsupported1080p")
	}
	v.Quality = "standard"
	if e := ValidateVideoSettings(v, fast); e != nil {
		t.Fatal(e)
	}
	classic, _ := registry.Lookup("violet-classic")
	v.Duration = 2
	if e := ValidateVideoSettings(v, classic); e != nil {
		t.Fatal(e)
	}
	v.Mode = "image_image"
	if ValidateVideoSettings(v, classic) != ErrModelInvalid {
		t.Fatal("classic reference-image unsupported")
	}
	latest, _ := registry.Lookup("violet-realism")
	v.Duration = 5
	v.Mode = "image_video"
	if ValidateVideoSettings(v, latest) != ErrModelInvalid {
		t.Fatal("adaptive-only firstframe cannot silently acceptfixedratio")
	}
}
func TestProductionRejectsWrongSceneAndImageOutput(t *testing.T) {
	sid := uuid.New()
	other := uuid.New()
	key := "outputs/test.mp4"
	j := &models.VideoGenerationJob{SceneID: &sid, Status: models.JobStatusSucceeded, Mode: "text", OutputMediaKey: &key}
	if !isSceneVideo(j, sid) || isSceneVideo(j, other) {
		t.Fatal("scene ownership mismatch")
	}
	j.Mode = "text_image"
	if isSceneVideo(j, sid) {
		t.Fatal("image is notclip")
	}
	j.Mode = "text"
	j.Status = models.JobStatusProcessing
	if isSceneVideo(j, sid) {
		t.Fatal("unfinished job accepted")
	}
}
