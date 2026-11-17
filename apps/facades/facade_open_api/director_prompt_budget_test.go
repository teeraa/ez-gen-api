package facade_open_api

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

func TestDirectorVideoBuildersPreserveLongThaiTail(t *testing.T) {
	t.Parallel()
	d := directorDocument{Project: directorTestProject()}
	sh := &d.Project.Scenes[0].Shots[0]
	sh.Description = strings.Repeat("เดินในฉากเดิมอย่างเป็นธรรมชาติ ", 150) + "ท้ายฉากยังอยู่🙂"
	for _, prompt := range []string{directorVideoPrompt(&d, &d.Project, sh, nil, false), directorExtensionPrompt(&d, &d.Project, sh, nil, 5)} {
		if utf8.RuneCountInString(prompt) <= 2000 || !strings.Contains(prompt, sh.Description) || promptcompiler.ValidateVideoPrompt(prompt) != nil {
			t.Fatal("Director video builder clipped an allowed long description")
		}
	}
}
func TestDirectorOverbudgetFilmedPromptFailsBeforeImageOrVideoExpense(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	d := continuityDoc(t, s.continuityStore)
	d.Project.Scenes[0].Shots[0].Description = strings.Repeat("ก", promptcompiler.VideoPromptRuneLimit) + "ท้าย🙂"
	in.Project = d.Project
	_ = directorEncode(&s.row, &d)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	video := continuityDoc(t, s.continuityStore).Project.Scenes[0].Shots[0].Video
	if len(img.calls) != 0 || len(v.CreatedRequests) != 0 || j.Status != "failed" || s.refunds != 1 || video.FailureCode == nil || *video.FailureCode != "VIDEO_PROMPT_TOO_LONG" {
		t.Fatalf("invalid filmed prompt incurred work or lacked explicit error: %+v", video)
	}
}
func TestDirectorOverbudgetNormalAndExtensionNeverSubmit(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"first_frame", "extension"} {
		t.Run(kind, func(t *testing.T) {
			f, s, v, _, j, in := newContinuityFixture(t)
			if kind == "extension" {
				var store *syntheticTestStore
				f, store, v, _, _, j, in = extensionFixture(t, "dreamina-seedance-2-0-fast-260128")
				s = store.continuityStore
			}
			d := continuityDoc(t, s)
			sh := directorFindShot(&d.Project, in.ShotID)
			sh.Description = strings.Repeat("ย", promptcompiler.VideoPromptRuneLimit)
			_ = directorEncode(&s.row, &d)
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			sh = directorFindShot(&continuityDocCopy(t, s).Project, in.ShotID)
			if len(v.CreatedRequests) != 0 || s.refunds != 1 || j.Status != "failed" || sh.Video.FailureCode == nil || *sh.Video.FailureCode != "VIDEO_PROMPT_TOO_LONG" {
				t.Fatal("overbudget request submitted or not refunded")
			}
		})
	}
}
func continuityDocCopy(t *testing.T, s *continuityStore) *directorDocument {
	d := continuityDoc(t, s)
	return &d
}
func TestDirectorLongFilmedVideoDoesNotChangeOriginalImagePrompt(t *testing.T) {
	t.Parallel()
	f, s, v, img, _, j, in := syntheticFixture(t)
	d := continuityDoc(t, s.continuityStore)
	text := strings.Repeat("Actor moves through the scene with natural motion. ", 150) + "COMPLETE_END_ท้าย🙂"
	d.Project.Scenes[0].Shots[0].Description = text
	in.Project = d.Project
	_ = directorEncode(&s.row, &d)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(img.calls) != 1 || len(v.CreatedRequests) != 1 || !strings.Contains(v.CreatedRequests[0].Content[0].Text, text) || !strings.Contains(img.calls[0].Prompt, "Re-render only the characters") {
		t.Fatal("long compiled video was clipped or image preparation changed")
	}
}
