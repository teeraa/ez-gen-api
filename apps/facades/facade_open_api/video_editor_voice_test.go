package facade_open_api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestVoicePromptKeepsDescriptionApartFromText(t *testing.T) {
	t.Parallel()
	if p := voicePrompt(VoiceRequest{Text: " สวัสดีครับ "}); p != "สวัสดีครับ" {
		t.Fatalf("plain text: %q", p)
	}
	p := voicePrompt(VoiceRequest{Text: "สวัสดีครับ", Voice: "ชายวัยกลางคน ทุ้ม"})
	if !strings.HasPrefix(p, "Voice: ชายวัยกลางคน ทุ้ม.") || !strings.HasSuffix(p, "\nสวัสดีครับ") || !strings.Contains(p, "Do not read") {
		t.Fatalf("described voice: %q", p)
	}
	if p := voicePrompt(VoiceRequest{Text: "hi", Speaker: "x"}); !strings.HasPrefix(p, "@Audio1") {
		t.Fatalf("speaker: %q", p)
	}
}

func TestVoiceValidationAndName(t *testing.T) {
	t.Parallel()
	ok := VoiceRequest{ClientUploadID: uuid.NewString(), Text: "ทดสอบ"}
	if !validVoice(ok) {
		t.Fatal("valid request rejected")
	}
	for _, bad := range []VoiceRequest{
		{ClientUploadID: "x", Text: "a"},
		{ClientUploadID: ok.ClientUploadID, Text: "  "},
		{ClientUploadID: ok.ClientUploadID, Text: strings.Repeat("ก", 3001)},
		{ClientUploadID: ok.ClientUploadID, Text: "a", SpeechRate: 101},
		{ClientUploadID: ok.ClientUploadID, Text: "a", PitchRate: -13},
	} {
		if validVoice(bad) {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if n := voiceAssetName(strings.Repeat("ก", 40)); !strings.HasSuffix(n, "….mp3") || !strings.HasPrefix(n, "เสียงพูด · ") {
		t.Fatalf("name %q", n)
	}
	if _, _, e := (&VideoEditorFacade{}).GenerateVoice(context.Background(), uuid.NewString(), uuid.NewString(), ok); !errors.Is(e, ErrVEVoiceUnavailable) {
		t.Fatalf("no client must be unavailable: %v", e)
	}
}
