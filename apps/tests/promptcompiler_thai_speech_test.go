package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

func TestThaiSpeechResolvedModelAndExactDialogue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, override, internal, dialogue string
		thai                               bool
	}{
		{"Thai", "", "violet-realism", "สวัสดีครับทุกคน", true},
		{"quotes_newline_mixed", "", "violet-realism", "  สวัสดี \"ทุกคน\"\nยินดีต้อนรับเข้าสู่ Violet {studio}!  ", true},
		{"override_to_25", "dreamina-seedance-2-5-260628", "violet-motion-2", "พูดภาษาไทย", true},
		{"override_from_25", "dreamina-seedance-2-0-fast-260128", "violet-realism", "พูดภาษาไทย", false},
		{"English", "", "violet-realism", "Hello everyone", false},
		{"Thai_with_English_brand", "", "violet-realism", "วันนี้เราขอแนะนำสินค้าแบรนด์ Violet ให้ทุกคนได้รู้จัก", true},
		{"English_with_Thai_brand", "", "violet-realism", "Welcome to the new สยาม collection today", false},
		{"ambiguous_mix", "", "violet-realism", "กข AB", false},
		{"Thai_digits_marks", "", "violet-realism", "123 ๑๒๓ ่ ฿ ฯ", false},
		{"empty", "", "violet-realism", "", false},
		{"whitespace", "", "violet-realism", " \n\t ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, e := modelark.NewRegistry(tc.override)
			if e != nil {
				t.Fatal(e)
			}
			model, _ := reg.Lookup(tc.internal)
			in := promptcompiler.Inputs{Dialogue: tc.dialogue, Scene: &promptcompiler.SceneInputs{Description: "Walk through garden with soft background music"}}
			got, e := promptcompiler.Compile(in, model)
			if e != nil {
				t.Fatal(e)
			}
			if got.Snapshot.Dialogue != tc.dialogue || got.GenerateAudio != model.SupportsAudio || got.ContentItems[0].Text != got.CompiledPrompt {
				t.Fatalf("lost audio or snapshot %+v", got)
			}
			if strings.Contains(got.CompiledPrompt, "{Thai: ") != tc.thai {
				t.Fatal(got.CompiledPrompt)
			}
			if tc.thai {
				if !strings.Contains(got.CompiledPrompt, "clear native Thai pronunciation, natural unhurried pace") {
					t.Fatal(got.CompiledPrompt)
				}
				payload := strings.SplitN(got.CompiledPrompt, "{Thai: ", 2)[1]
				var decoded string
				decoder := json.NewDecoder(strings.NewReader(payload))
				if e = decoder.Decode(&decoded); e != nil || decoded != tc.dialogue {
					t.Fatalf("changed spoken words %q %v", decoded, e)
				}
				quoted, _ := json.Marshal(tc.dialogue)
				if !strings.Contains(got.CompiledPrompt, "{Thai: "+string(quoted)+"}") {
					t.Fatal("missing brace delimiter")
				}
			} else if tc.dialogue != "" {
				quoted, _ := json.Marshal(tc.dialogue)
				if !strings.HasPrefix(got.CompiledPrompt, "Spoken dialogue, say these exact words with natural lip sync and native audio, not subtitles: "+string(quoted)) {
					t.Fatal("other model/language changed")
				}
			} else if strings.Contains(got.CompiledPrompt, "Spoken dialogue") {
				t.Fatal("empty dialogue gained speech instruction")
			}
			if !strings.Contains(got.CompiledPrompt, in.Scene.Description) {
				t.Fatal("scene/audio intent changed")
			}
		})
	}
}

func TestThaiSpeechPromptBudgetExactBoundary(t *testing.T) {
	t.Parallel()
	reg, _ := modelark.NewRegistry("")
	model, _ := reg.Lookup("violet-realism")
	in := promptcompiler.Inputs{Dialogue: "สวัสดี \"ทุกคน\"\nWelcome", Scene: &promptcompiler.SceneInputs{Description: "x"}, Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png", StorageKey: "fixture.png"}}}
	base, e := promptcompiler.Compile(in, model)
	if e != nil {
		t.Fatal(e)
	}
	// Expand only scene text so all language, escaping and role controls count.
	remaining := 2000 - utf8.RuneCountInString(base.CompiledPrompt)
	in.Scene.Description = strings.Repeat("ก", remaining+1)
	boundary, e := promptcompiler.Compile(in, model)
	if e != nil || utf8.RuneCountInString(boundary.CompiledPrompt) != 2000 || boundary.Snapshot.Dialogue != in.Dialogue {
		t.Fatalf("boundary len=%v err=%v", boundary, e)
	}
	in.Scene.Description += "ก"
	if _, e = promptcompiler.Compile(in, model); !errors.Is(e, promptcompiler.ErrPromptBudget) {
		t.Fatalf("want actionable budget error got %v", e)
	}
}

func TestThaiSpeechSDKWirePayload(t *testing.T) {
	dialogue := "สวัสดี \"ทุกคน\"\nยินดีต้อนรับเข้าสู่ Violet"
	reg, _ := modelark.NewRegistry("dreamina-seedance-2-5-260628")
	model, _ := reg.Lookup("violet-motion-2")
	compiled, e := promptcompiler.Compile(promptcompiler.Inputs{Dialogue: dialogue, Scene: &promptcompiler.SceneInputs{Description: "สวน แสงเย็น"}}, model)
	if e != nil {
		t.Fatal(e)
	}
	var wire struct {
		Model   string `json:"model"`
		Audio   bool   `json:"generate_audio"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := json.NewDecoder(r.Body).Decode(&wire); e != nil {
			t.Error(e)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fixture-task"}`))
	}))
	defer server.Close()
	_, e = modelark.NewClient("fixture-only", server.URL+"/api/v3").CreateTask(context.Background(), modelark.CreateTaskRequest{Model: compiled.ProviderModelID, Content: compiled.ContentItems, GenerateAudio: compiled.GenerateAudio})
	if e != nil || !wire.Audio || wire.Model != model.ProviderModelID || len(wire.Content) != 1 || wire.Content[0].Type != "text" || wire.Content[0].Text != compiled.CompiledPrompt {
		t.Fatalf("wire %+v err %v", wire, e)
	}
	quoted, _ := json.Marshal(dialogue)
	if !strings.Contains(wire.Content[0].Text, "{Thai: "+string(quoted)+"}") {
		t.Fatal("SDK changed dialogue")
	}
}
