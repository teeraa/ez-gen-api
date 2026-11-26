package promptcompiler

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

// A dubbed pipeline line is already spoken in reference audio 1: the prompt asks for lip-sync to it
// instead of native speech, the budget counts the same text, and the snapshot records the choice.
func TestDialogueAudioAsksForLipSyncToAudioOne(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	in := Inputs{Dialogue: "สวัสดีค่ะ วันนี้มีของดีมาฝาก", Scene: &SceneInputs{Description: "ยิ้มทักทายกล้อง"}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png"}}}
	spoken, e := Compile(in, model)
	if e != nil {
		t.Fatal(e)
	}
	in.DialogueAudio = true
	dubbed, e := Compile(in, model)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(spoken.CompiledPrompt, "Audio 1") || spoken.Snapshot.DialogueAudio {
		t.Fatal("plain dialogue must stay native speech")
	}
	if !strings.HasPrefix(dubbed.CompiledPrompt, "Audio 1 is the finished dialogue track") || !strings.Contains(dubbed.CompiledPrompt, `"สวัสดีค่ะ วันนี้มีของดีมาฝาก"`) || strings.Contains(dubbed.CompiledPrompt, "native audio") {
		t.Fatalf("dubbed prompt does not ask for lip-sync to audio 1: %q", dubbed.CompiledPrompt)
	}
	if !dubbed.Snapshot.DialogueAudio || !dubbed.GenerateAudio {
		t.Fatal("dubbed snapshot/audio flags not set")
	}
	if used, _ := PromptUsage(in, model); used != len([]rune(dubbed.CompiledPrompt)) {
		t.Fatalf("budget %d does not match compiled prompt %d", used, len([]rune(dubbed.CompiledPrompt)))
	}
}
