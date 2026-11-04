package tests

import (
	"context"
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeDialogueExactAndBudget(t *testing.T) {
	t.Parallel()
	reg, _ := modelark.NewRegistry("")
	m, _ := reg.Lookup("violet-motion-2")
	speech := "สวัสดี \"ทุกคน\"\nมาพบกัน"
	in := promptcompiler.Inputs{Dialogue: speech, ModelID: m.InternalID, Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "เดินในสวน"}, Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png", StorageKey: "generated/character.png"}}}
	r, e := promptcompiler.Compile(in, m)
	if e != nil {
		t.Fatal(e)
	}
	quoted, _ := json.Marshal(speech)
	if !r.GenerateAudio || !strings.Contains(r.CompiledPrompt, string(quoted)) || !strings.Contains(r.CompiledPrompt, "เดินในสวน") || r.Snapshot.Dialogue != speech || utf8.RuneCountInString(r.CompiledPrompt) > 2000 || len(r.ContentItems) != 2 || r.ContentItems[1].Role != modelark.RoleReferenceImage {
		t.Fatalf("lost native dialogue %+v", r)
	}
	in.Scene.Description = strings.Repeat("ก", 2000)
	if _, e = promptcompiler.Compile(in, m); e == nil {
		t.Fatal("silent scene truncation")
	}
	in.Dialogue = ""
	r, e = promptcompiler.Compile(in, m)
	if e != nil || r.GenerateAudio {
		t.Fatal("legacy default changed", e)
	}
	m.SupportsAudio = false
	in.Dialogue = speech
	if _, e = promptcompiler.Compile(in, m); e == nil {
		t.Fatal("unsupported audio accepted")
	}
}

func TestNativeDialogueSDKSerialization(t *testing.T) {
	t.Parallel()
	dialogue := "สวัสดี \"ทุกคน\"\nยินดีที่ได้พบ"
	registry, _ := modelark.NewRegistry("")
	m, _ := registry.Lookup("violet-motion-2")
	compiled, e := promptcompiler.Compile(promptcompiler.Inputs{Dialogue: dialogue, Scene: &promptcompiler.SceneInputs{Description: "สวนยามเย็น"}, Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png", StorageKey: "https://durable.fixture/generated.png"}}}, m)
	if e != nil {
		t.Fatal(e)
	}
	type item struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Role     string `json:"role"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	var got struct {
		GenerateAudio bool   `json:"generate_audio"`
		Content       []item `json:"content"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
			t.Error(e)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fixture-task"}`))
	}))
	defer server.Close()
	_, e = modelark.NewClient("fixture-only", server.URL+"/api/v3").CreateTask(context.Background(), modelark.CreateTaskRequest{Model: m.ProviderModelID, Content: compiled.ContentItems, GenerateAudio: compiled.GenerateAudio})
	if e != nil {
		t.Fatal(e)
	}
	quoted, _ := json.Marshal(dialogue)
	if !got.GenerateAudio || len(got.Content) != 2 || got.Content[1].Role != "reference_image" || got.Content[1].ImageURL.URL != "https://durable.fixture/generated.png" || !strings.Contains(got.Content[0].Text, string(quoted)) {
		t.Fatalf("actual SDK wire mismatch %+v", got)
	}
}
