package facade_open_api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
)

func TestDirectorSingleShotAdmissionPreparesSceneBeforeVideo(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"cinematic", "realistic"} {
		for _, draft := range []bool{false, true} {
			name := style + "/normal"
			if draft {
				name = style + "/draft"
			}
			t.Run(name, func(t *testing.T) {
				f, s, v, img, _, j, _ := syntheticFixture(t)
				d := continuityDoc(t, s.continuityStore)
				d.Project.Style = style
				d.Project.Scenes[0].Shots[0].Video = nil
				f.options.Enabled = true
				f.options.AnalysisModel = "analysis-fixture"
				f.options.ImageModel = "seedream-fixture"
				f.options.StoryPoints = 1
				f.options.ImagePoints = 1
				f.options.DraftPoints = 8
				if draft {
					d.Project.Storyboard.VideoModelID = "seedance-2-5"
				}
				body := `{"shotIds":["a"]}`
				if draft {
					body = `{"shotIds":["a"],"draft":true}`
				}
				var request req.DirectorRequest
				if e := json.Unmarshal([]byte(body), &request); e != nil {
					t.Fatal(e)
				}
				var snapshot directorJobInput
				if e := f.enqueueVideos(&d, request, func(kind, target string, cost int) error {
					job, e := f.job(&d, kind, target, cost, request, func(string) (*models.LibraryAsset, error) { t.Fatal("unexpected asset lookup"); return nil, nil })
					if e != nil {
						return e
					}
					*j = job
					return json.Unmarshal(job.Snapshot, &snapshot)
				}); e != nil {
					t.Fatal(e)
				}
				if snapshot.ImageModel != "seedream-fixture" || snapshot.Project.Style != style || snapshot.DraftTaskID != "" || snapshot.Draft != draft {
					t.Fatalf("bad single-shot snapshot %+v", snapshot)
				}
				if e := directorEncode(&s.row, &d); e != nil {
					t.Fatal(e)
				}
				img.onGenerate = func() {
					if len(v.CreatedRequests) != 0 {
						t.Fatal("video submitted before image preparation")
					}
				}
				if e := f.directorVideo(context.Background(), j, snapshot); e != nil {
					t.Fatal(e)
				}
				if len(img.calls) != 1 || len(v.CreatedRequests) != 1 || len(v.CreatedRequests[0].Content) != 2 || !strings.Contains(v.CreatedRequests[0].Content[1].URL, "-synthetic.png") || v.CreatedRequests[0].Draft != draft {
					t.Fatalf("single shot skipped preprocessing: image=%d video=%+v", len(img.calls), v.CreatedRequests)
				}
				if draft && v.CreatedRequests[0].Resolution != "480p" {
					t.Fatal("draft resolution changed")
				}
			})
		}
	}
}

func TestDirectorSingleShotFinalizeAdmissionInheritsDraftWithoutNewImage(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, _ := syntheticFixture(t)
	d := continuityDoc(t, s.continuityStore)
	f.options.Enabled = true
	f.options.AnalysisModel = "analysis-fixture"
	f.options.ImageModel = "seedream-fixture"
	f.options.StoryPoints = 1
	f.options.ImagePoints = 1
	f.options.DraftPoints = 8
	d.Project.Storyboard.VideoModelID = "seedance-2-5"
	d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
	d.Project.Scenes[0].Shots[0].Video.Draft = true
	d.VideoDrafts["a"] = directorVideoDraft{TaskID: "prepared-draft", Provider: "dreamina-seedance-2-5-260628", CreatedAt: time.Now()}
	ids := []string{"a"}
	request := req.DirectorRequest{ShotIDs: &ids, Finalize: true}
	var snapshot directorJobInput
	if e := f.enqueueVideos(&d, request, func(kind, target string, cost int) error {
		job, e := f.job(&d, kind, target, cost, request, func(string) (*models.LibraryAsset, error) { t.Fatal("unexpected asset lookup"); return nil, nil })
		if e != nil {
			return e
		}
		*j = job
		return json.Unmarshal(job.Snapshot, &snapshot)
	}); e != nil {
		t.Fatal(e)
	}
	if e := directorEncode(&s.row, &d); e != nil {
		t.Fatal(e)
	}
	if e := f.directorVideo(context.Background(), j, snapshot); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 0 || len(v.CreatedRequests) != 1 || len(v.CreatedRequests[0].Content) != 1 || v.CreatedRequests[0].Content[0].DraftTaskID != "prepared-draft" {
		t.Fatalf("finalize did not inherit prepared draft: %+v", v.CreatedRequests)
	}
}

type templateSDKStorage struct {
	mediaStorage
	uploaded func(string)
}

func (s *templateSDKStorage) Upload(c context.Context, key string, data []byte, kind string) error {
	if e := s.mediaStorage.Upload(c, key, data, kind); e != nil {
		return e
	}
	s.uploaded(key)
	return nil
}

func TestDirectorAdmissionSDKUsesOnlyGeneratedSyntheticImage(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"realistic", "cinematic"} {
		t.Run(style, func(t *testing.T) {
			f, s, _, _, media, j, _ := syntheticFixture(t)
			d := continuityDoc(t, s.continuityStore)
			d.Project.Style = style
			d.Project.Scenes[0].Shots[0].Video = nil
			castID := uuid.NewString()
			d.Project.Idea.CharacterAssetIDs = []string{castID}
			d.Project.Scenes[0].Shots[0].CharacterAssetIDs = []string{castID}
			f.options.Enabled = true
			f.options.AnalysisModel = "analysis-fixture"
			f.options.ImageModel = "image-fixture"
			f.options.StoryPoints = 1
			f.options.ImagePoints = 1
			var mu sync.Mutex
			events := []string{}
			add := func(event string) { mu.Lock(); events = append(events, event); mu.Unlock() }
			var imageWire struct {
				Prompt string `json:"prompt"`
				Image  string `json:"image"`
			}
			var videoWire struct {
				Content []struct {
					Type     string `json:"type"`
					Role     string `json:"role"`
					Text     string `json:"text"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
				Negative json.RawMessage `json:"negative_prompt"`
			}
			pngData, _, _ := media.continuityMedia.Fetch(context.Background(), "https://fixture.test/frame.png")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/images/generations"):
					if e := json.NewDecoder(r.Body).Decode(&imageWire); e != nil {
						w.WriteHeader(400)
						return
					}
					add("image")
					_ = json.NewEncoder(w).Encode(map[string]any{"model": "image-fixture", "created": 1, "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(pngData)}}})
				case strings.HasSuffix(r.URL.Path, "/contents/generations/tasks"):
					if e := json.NewDecoder(r.Body).Decode(&videoWire); e != nil {
						w.WriteHeader(400)
						return
					}
					add("video")
					_, _ = w.Write([]byte(`{"id":"synthetic-template-sdk-fixture"}`))
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			f.images = modelark.NewImageClient("fixture-only", server.URL, "fallback")
			f.video = modelark.NewClient("fixture-only", server.URL+"/api/v3")
			f.storage = &templateSDKStorage{mediaStorage: media, uploaded: func(string) { add("upload") }}
			ids := []string{"a"}
			request := req.DirectorRequest{ShotIDs: &ids}
			var snapshot directorJobInput
			if e := f.enqueueVideos(&d, request, func(kind, target string, cost int) error {
				job, e := f.job(&d, kind, target, cost, request, func(string) (*models.LibraryAsset, error) {
					key := "raw-cast.png"
					return &models.LibraryAsset{Base: models.Base{ID: uuid.MustParse(castID)}, Name: "actor", Description: "distinctive green coat", StorageKey: &key}, nil
				})
				if e != nil {
					return e
				}
				*j = job
				return json.Unmarshal(job.Snapshot, &snapshot)
			}); e != nil {
				t.Fatal(e)
			}
			_ = directorEncode(&s.row, &d)
			if e := f.directorVideo(context.Background(), j, snapshot); e != nil {
				t.Fatal(e)
			}
			mu.Lock()
			order := strings.Join(events, ",")
			mu.Unlock()
			if order != "image,upload,video" {
				t.Fatalf("generation order %s", order)
			}
			expectedScene := "actor walks through the scene\nCharacter: actor: distinctive green coat\nCamera: ; movement: subtle cinematic camera movement\n" + directorObjectIntegrity + "\n" + directorFaceIntegrity
			if imageWire.Prompt != characterdesign.CompileDirectorFacelessCasting(expectedScene) || imageWire.Image != "https://owned.test/a.png" {
				t.Fatal("original synthetic image request changed")
			}
			expectedKey := "director/" + j.UserID.String() + "/" + j.ProjectID.String() + "/a/" + j.ID.String() + "-synthetic.png"
			if len(videoWire.Content) != 2 || videoWire.Content[1].Type != "image_url" || videoWire.Content[1].Role != "reference_image" || videoWire.Content[1].ImageURL.URL != "https://owned.test/"+expectedKey || len(media.objects[expectedKey]) == 0 || len(videoWire.Negative) != 0 {
				t.Fatalf("SDK did not receive only its generated synthetic image %+v", videoWire)
			}
			text := videoWire.Content[0].Text
			mode, otherMode := "REALISTIC MODE — AUTHENTIC OBSERVATIONAL FOOTAGE", "CINEMATIC MODE — PHOTOREALISTIC LIVE-ACTION CINEMATOGRAPHY"
			if style == "cinematic" {
				mode, otherMode = otherMode, mode
			}
			if strings.Contains(text, otherMode) || strings.Count(text, "Negative constraints:") != 1 {
				t.Fatal("SDK JSON mixed both filmed modes or repeated negative constraints")
			}
			if !strings.Contains(text, "Image 1: casting and pose guide") || !strings.Contains(text, mode) || !strings.Contains(text, "Negative constraints:") || !strings.Contains(text, "inconsistent lighting.") || !strings.Contains(text, "distinctive green coat") {
				t.Fatal("full user template or scene did not reach SDK JSON")
			}
		})
	}
}
