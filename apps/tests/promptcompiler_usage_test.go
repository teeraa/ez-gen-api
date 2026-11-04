package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

// The studio preflight shows PromptUsage to users; it must agree with what Compile enforces,
// or the page would let through requests the create endpoint rejects (or block valid ones).
func TestPromptUsageMatchesCompileBudget(t *testing.T) {
	t.Parallel()
	reg, _ := modelark.NewRegistry("")
	for _, id := range []string{"violet-motion-2", "violet-realism"} {
		m, ok := reg.Lookup(id)
		if !ok || !m.SupportsAudio {
			continue
		}
		for _, dialogue := range []string{"สวัสดีครับ วันนี้อากาศดีมาก", "Hello there, nice to meet you"} {
			// Walk the scene length across the limit; the two must flip at the same point.
			for sceneLen := 1300; sceneLen <= 2000; sceneLen += 7 {
				in := promptcompiler.Inputs{Dialogue: dialogue, ModelID: m.InternalID, Quality: "standard",
					Scene:  &promptcompiler.SceneInputs{Description: strings.Repeat("ก", sceneLen)},
					Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png", StorageKey: "generated/image.png"}}}
				used, limit := promptcompiler.PromptUsage(in, m)
				_, err := promptcompiler.Compile(in, m)
				if over := used > limit; over != errors.Is(err, promptcompiler.ErrPromptBudget) {
					t.Fatalf("%s scene=%d used=%d limit=%d err=%v", id, sceneLen, used, limit, err)
				}
			}
		}
	}
}
