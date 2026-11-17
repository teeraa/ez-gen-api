package facade_open_api

import (
	"fmt"
	"strings"
	"testing"
)

func TestVEUploadKindAndFailureMessages(t *testing.T) {
	for mime, kind := range map[string]string{"audio/mpeg": "audio", "audio/x-m4a": "audio", "image/png": "image", "video/mp4": "video", "application/octet-stream": "video"} {
		if got := veDeclaredKind(mime); got != kind {
			t.Fatalf("%s: got %s want %s", mime, got, kind)
		}
	}
	if code, message := veUploadFailure(fmt.Errorf("x: %w", ErrVEMediaTooLong)); code != "MEDIA_TOO_LONG" || !strings.Contains(message, "60 นาที") {
		t.Fatalf("too long: %s %s", code, message)
	}
	if code, _ := veUploadFailure(ErrVEType); code != "INVALID_FILE_TYPE" {
		t.Fatal(code)
	}
	if code, _ := veUploadFailure(ErrVEStorage); code != "STORAGE_UNAVAILABLE" {
		t.Fatal(code)
	}
}
