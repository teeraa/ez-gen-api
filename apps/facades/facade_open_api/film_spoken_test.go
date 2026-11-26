package facade_open_api

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

// An English delivery note must never sit beside a spoken line: the video model read
// "(Firm, low growl, ...)" aloud before the Thai line. It goes on its own acting line, and the
// prompt says only the quoted Thai is spoken.
func TestFilmSpokenLinesKeepDirectionSilent(t *testing.T) {
	may := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "เมย์"}
	id := may.ID.String()
	note := "Firm, low growl, spoken quickly to the empty car interior."
	sh := &filmShot{ID: "s", Seconds: 8, CharacterIDs: []string{id}, Action: "she ducks under the searchlight",
		Dialogue: []filmLine{{SpeakerID: id, Line: "ฉันจะไม่ยอมให้มันสำเร็จ", Delivery: note}}}
	for name, b := range map[string]*filmBrief{
		"native":   {Shot: sh, Cast: []*models.FilmElement{&may}, Style: "cinematic", Filmed: true, Dubbed: map[string]bool{id: true}, AudioRef: true, Native: true},
		"provider": {Shot: sh, Cast: []*models.FilmElement{&may}, Style: "cinematic", Filmed: true},
	} {
		var w strings.Builder
		filmPerformance(b, &w)
		p := w.String()
		if !strings.Contains(p, `in Thai: "ฉันจะไม่ยอมให้มันสำเร็จ"`+"\n") {
			t.Fatalf("%s: the quoted Thai line must stand alone:\n%s", name, p)
		}
		if strings.Contains(p, "("+note) || strings.Contains(p, ", "+note) {
			t.Fatalf("%s: delivery note still beside the line:\n%s", name, p)
		}
		if !strings.Contains(p, "never spoken aloud): "+note) || !strings.Contains(p, filmSpokenOnly) {
			t.Fatalf("%s: delivery must be silent direction and only the Thai is spoken:\n%s", name, p)
		}
	}
	// A dubbed line (voice added later) keeps its note: the model makes no voice for it.
	b := &filmBrief{Shot: sh, Cast: []*models.FilmElement{&may}, Style: "cinematic", Dubbed: map[string]bool{id: true}}
	var w strings.Builder
	filmPerformance(b, &w)
	if p := w.String(); strings.Contains(p, filmSpokenOnly) || !strings.Contains(p, "generate NO voice") {
		t.Fatalf("dubbed-later line:\n%s", p)
	}
}
