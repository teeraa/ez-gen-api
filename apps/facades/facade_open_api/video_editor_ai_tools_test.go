package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func aiToolAsset(kind string, duration float64) models.VideoEditorAsset {
	return models.VideoEditorAsset{Base: models.Base{ID: uuid.New()}, Kind: kind, Status: "ready", Duration: duration, HasAudio: kind != "image"}
}

func TestAIDraftEffectsUseEditorValidators(t *testing.T) {
	t.Parallel()
	video := aiToolAsset("video", 30)
	segments := []models.EditorAIDraftSegment{
		{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 4, Volume: 1, Speed: 9, FadeIn: 20, FadeOut: 1, VideoEditorTransition: models.VideoEditorTransition{Transition: "fade", TransitionDuration: .5}, VideoEditorClipLook: models.VideoEditorClipLook{Fit: "contain", PanX: .5, Zoom: 2, Brightness: 3}},
		{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 10, End: 14, Volume: 1, Speed: 2, VideoEditorTransition: models.VideoEditorTransition{Transition: "wipeleft", TransitionDuration: .4}},
		{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 20, End: 22, Volume: 1, VideoEditorTransition: models.VideoEditorTransition{Transition: "bogus", TransitionDuration: .4}},
		{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 22, End: 23, Volume: 1, FadeIn: .6, FadeOut: .6, VideoEditorTransition: models.VideoEditorTransition{Transition: "fade", TransitionDuration: 1.5}},
	}
	sanitizeAIDraftEffects(segments)
	first, second, third, fourth := segments[0], segments[1], segments[2], segments[3]
	if first.Speed != 0 || first.FadeIn != 0 || first.FadeOut != 1 || first.Transition != "" || first.TransitionDuration != 0 || first.Fit != "" || first.PanX != .5 || first.Zoom != 2 || first.Brightness != 0 {
		t.Fatalf("first %+v", first)
	}
	if second.Speed != 2 || second.Transition != "wipeleft" || second.TransitionDuration != .4 {
		t.Fatalf("second %+v", second)
	}
	if third.Transition != "" || third.TransitionDuration != 0 {
		t.Fatalf("unknown transition kept %+v", third)
	}
	if fourth.FadeIn != .6 || fourth.FadeOut != 0 || fourth.Transition != "" {
		t.Fatalf("fades/transition beyond length kept %+v", fourth)
	}
	if err := validateVESegments(aiDraftTimeline(segments), []models.VideoEditorAsset{video}); err != nil {
		t.Fatalf("sanitized draft must pass the timeline validator: %v", err)
	}
	want := 4.0 + 2 - .4 + 2 + 1
	if got := aiDraftDuration(segments); math.Abs(got-want) > 1e-9 {
		t.Fatalf("duration %v want %v", got, want)
	}
}

func TestAIGeneratedDraftSlowMotionKeepsTargetRule(t *testing.T) {
	t.Parallel()
	asset := uuid.NewString()
	in := models.EditorAIInput{Sources: []models.EditorAISource{{ID: asset, Duration: 30}}, TargetDuration: 10}
	d := models.EditorAIDraft{Segments: []models.EditorAIDraftSegment{
		{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 5, Volume: 1, Title: "a", Speed: .5},
		{ID: uuid.NewString(), AssetID: asset, Start: 10, End: 15, Volume: 1, Title: "b", Speed: 2, VideoEditorTransition: models.VideoEditorTransition{Transition: "fade", TransitionDuration: 1}},
	}}
	if err := validateAIDraft(&d, in, nil); err != nil {
		t.Fatal(err)
	}
	if d.Segments[0].Speed != 0 || d.Segments[1].Speed != 2 || d.Duration > in.TargetDuration+.05 {
		t.Fatalf("slow motion must be dropped when it exceeds the target: %+v %v", d.Segments, d.Duration)
	}
	if want := 5 + 2.5 - 1.0; math.Abs(d.Duration-want) > 1e-9 {
		t.Fatalf("duration %v want %v", d.Duration, want)
	}
	manual := models.EditorAIDraft{Segments: []models.EditorAIDraftSegment{{ID: d.Segments[0].ID, AssetID: asset, Start: 0, End: 10, Volume: 1, Title: "a", Speed: .5}}}
	if err := validateAIDraft(&manual, in, map[string]bool{d.Segments[0].ID: true}); err != nil || manual.Segments[0].Speed != .5 || manual.Duration != 20 {
		t.Fatalf("manual edits may exceed the target like other manual changes: %v %+v", err, manual)
	}
}

func TestAICompositionFollowsSpeedAndTransitions(t *testing.T) {
	t.Parallel()
	job, asset := uuid.NewString(), uuid.NewString()
	in := models.EditorAIInput{AutoCompose: true, Sources: []models.EditorAISource{{ID: asset, Duration: 30}}, TargetDuration: 10}
	d := models.EditorAIDraft{
		TextStyles: &models.EditorAITextStyles{Caption: &models.EditorAITextStyle{Color: "#FFCC00", BoxColor: "#000000", BoxOpacity: .5}, Title: &models.EditorAITextStyle{Y: .3, Animation: "slide"}},
		Segments: []models.EditorAIDraftSegment{
			{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 4, Volume: 1, Title: "แรก", Speed: 2},
			{ID: uuid.NewString(), AssetID: asset, Start: 10, End: 12, Volume: 1, Title: "สอง", VideoEditorTransition: models.VideoEditorTransition{Transition: "fade", TransitionDuration: .5}},
		},
	}
	transcripts := []models.EditorAITranscript{{AssetID: asset, HasSpeech: true, Text: "x", Segments: []models.EditorAISpeech{{Start: 1, End: 3, Text: "หนึ่ง"}, {Start: 10.5, End: 11.5, Text: "สอง"}}}}
	if err := validateAIDraft(&d, in, nil); err != nil {
		t.Fatal(err)
	}
	composeAIDraft(&d, in, job, transcripts)
	if err := validateAIComposition(d, in, job); err != nil {
		t.Fatal(err)
	}
	spans := map[string][2]float64{}
	for _, l := range d.TextLayers {
		key := l.Text
		if l.Y == .3 {
			key = "title:" + l.Text
			if l.Animation != "slide" {
				t.Fatalf("title style %+v", l)
			}
		} else if l.Color != "#FFCC00" || l.BoxOpacity != .5 {
			t.Fatalf("caption style %+v", l)
		}
		spans[key] = [2]float64{l.Start, l.End}
	}
	check := func(key string, start, end float64) {
		got, ok := spans[key]
		if !ok || math.Abs(got[0]-start) > 1e-9 || math.Abs(got[1]-end) > 1e-9 {
			t.Fatalf("%s: got %v want [%v %v] (%v)", key, got, start, end, spans)
		}
	}
	check("หนึ่ง", .5, 1.5)
	check("title:สอง", 1.5, 3.5)
	check("สอง", 2, 3)
}

func TestAIMergeKeepsEffectBoundaries(t *testing.T) {
	t.Parallel()
	asset := uuid.NewString()
	base := models.EditorAIDraftSegment{AssetID: asset, Volume: 1, Title: "t", Reason: "r"}
	a, b := base, base
	a.ID, a.Start, a.End, a.Speed = "a", 0, 2, 2
	b.ID, b.Start, b.End, b.Speed = "b", 2, 4, 2
	if got := aiMergeAdjacent([]models.EditorAIDraftSegment{a, b}); len(got) != 1 || got[0].End != 4 || got[0].Speed != 2 {
		t.Fatalf("same effects should merge %+v", got)
	}
	b.Transition, b.TransitionDuration = "fade", .5
	if got := aiMergeAdjacent([]models.EditorAIDraftSegment{a, b}); len(got) != 2 {
		t.Fatalf("transition must not be merged away %+v", got)
	}
	b.VideoEditorTransition = models.VideoEditorTransition{}
	b.Speed = 1.5
	if got := aiMergeAdjacent([]models.EditorAIDraftSegment{a, b}); len(got) != 2 {
		t.Fatalf("different speeds must stay separate %+v", got)
	}
}

func TestAITextStyleSanitizedByTextValidator(t *testing.T) {
	t.Parallel()
	got := sanitizeAITextStyle(&models.EditorAITextStyle{Color: "red", OutlineColor: "#000000", OutlineWidth: 30, BoxColor: "#112233", BoxOpacity: .4, Animation: "spin", Y: .3, FontSize: 5})
	want := models.EditorAITextStyle{BoxColor: "#112233", BoxOpacity: .4, Y: .3}
	if got == nil || *got != want {
		t.Fatalf("got %+v", got)
	}
	if sanitizeAITextStyle(&models.EditorAITextStyle{Color: "nope"}) != nil {
		t.Fatal("all-invalid style should be nil")
	}
	wire := &aiWireTextStyle{FontSize: 40.5, OutlineWidth: 2, OutlineColor: "#FFFFFF"}
	if s := wire.style(); s == nil || s.FontSize != 0 || s.OutlineWidth != 2 {
		t.Fatalf("fractional size must drop only that field %+v", s)
	}
}

func TestAIPlanSchemaOffersEditorTools(t *testing.T) {
	t.Parallel()
	raw := aiPlanSchema([]models.EditorAIEvidence{{ID: uuid.NewString(), Start: 0, End: 5}}, 10)
	var schema struct {
		Properties struct {
			CaptionStyle json.RawMessage `json:"captionStyle"`
			TitleStyle   json.RawMessage `json:"titleStyle"`
			Segments     struct {
				Items struct {
					Required   []string `json:"required"`
					Properties map[string]struct {
						Enum []string `json:"enum"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"segments"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	items := schema.Properties.Segments.Items
	for _, name := range []string{"speed", "fadeIn", "fadeOut", "transition", "transitionDuration", "brightness", "contrast", "saturation", "fit", "zoom", "panX", "panY"} {
		if _, ok := items.Properties[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if strings.Join(items.Required, ",") != "evidenceId,title,reason" {
		t.Fatalf("effects must stay optional: %v", items.Required)
	}
	names := []string{}
	for name := range veTransitions {
		names = append(names, name)
	}
	sort.Strings(names)
	enum := append([]string{}, items.Properties["transition"].Enum...)
	sort.Strings(enum)
	if strings.Join(names, ",") != strings.Join(enum, ",") {
		t.Fatalf("schema transitions %v differ from editor %v", enum, names)
	}
	if len(schema.Properties.CaptionStyle) == 0 || len(schema.Properties.TitleStyle) == 0 || !json.Valid(aiEditSchema) {
		t.Fatal("style/edit schema missing")
	}
}

func aiEditTestState(t *testing.T) (aiEditState, []models.VideoEditorAsset) {
	t.Helper()
	video, audio, image := aiToolAsset("video", 30), aiToolAsset("audio", 30), aiToolAsset("image", 0)
	s := aiEditState{
		Segments: []models.VideoEditorSegment{
			{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 4, Volume: 1},
			{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 5, End: 9, Volume: 1, VideoEditorTransition: models.VideoEditorTransition{Transition: "fade", TransitionDuration: .5}},
		},
		AudioClips:  []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 0, Start: 0, End: 5, Volume: 1}},
		TextLayers:  []models.VideoEditorTextLayer{{ID: uuid.NewString(), Text: "เดิม", Start: 0, End: 2, X: .5, Y: .8, FontSize: 48, Color: "#FFFFFF", Align: "center"}},
		ImageLayers: []models.VideoEditorImageLayer{},
	}
	return s, []models.VideoEditorAsset{video, audio, image}
}

func aiEditF(v float64) *float64 { return &v }
func aiEditS(v string) *string   { return &v }

func TestAIEditOperationsUsePutValidators(t *testing.T) {
	t.Parallel()
	base, assets := aiEditTestState(t)
	video, audio, image := assets[0].ID.String(), assets[1].ID.String(), assets[2].ID.String()
	seg0, seg1, text, clip := base.Segments[0].ID, base.Segments[1].ID, base.TextLayers[0].ID, base.AudioClips[0].ID
	ops := []aiEditOp{
		{Tool: "move_segment", Target: seg1, Index: aiEditF(0)},
		{Tool: "trim_segment", Target: seg0, End: aiEditF(31)},
		{Tool: "trim_segment", Target: seg0, Start: aiEditF(1)},
		{Tool: "set_segment", Target: seg0, Fit: aiEditS("cover"), Zoom: aiEditF(1.5), Volume: aiEditF(.4)},
		{Tool: "set_segment", Target: seg0, Transition: aiEditS("none"), TransitionDuration: aiEditF(.5)},
		{Tool: "move_segment", Target: seg0, Index: aiEditF(.5)},
		{Tool: "update_text", Target: text, OutlineColor: aiEditS("#000000")},
		{Tool: "update_text", Target: text, BoxOpacity: aiEditF(.5)},
		{Tool: "update_text", Target: text, Color: aiEditS("pink")},
		{Tool: "add_image_layer", AssetID: aiEditS(video)},
		{Tool: "add_image_layer", AssetID: aiEditS(image), Width: aiEditF(.2)},
		{Tool: "update_audio_clip", Target: clip, Volume: aiEditF(.2), FadeOut: aiEditF(1)},
		{Tool: "update_audio_clip", Target: clip, FadeIn: aiEditF(4.5)},
		{Tool: "delete_segment", Target: seg1},
	}
	state, applied, rejected := applyAIEditResult(base, aiEditResult{Operations: ops}, assets, true)
	tools := []string{}
	for _, op := range applied {
		tools = append(tools, op.Tool)
	}
	if got := strings.Join(tools, ","); got != "move_segment,trim_segment,set_segment,update_text,add_image_layer,update_audio_clip,delete_segment" || rejected != 7 {
		t.Fatalf("applied %s rejected %d", got, rejected)
	}
	if len(state.Segments) != 1 || state.Segments[0].ID != seg0 || state.Segments[0].Transition != "" || state.Segments[0].Start != 1 || state.Segments[0].Fit != "cover" || state.Segments[0].Volume != .4 {
		t.Fatalf("segments %+v", state.Segments)
	}
	if l := state.TextLayers[0]; l.OutlineWidth != 3 || l.OutlineColor != "#000000" || l.BoxColor != "" || l.Color != "#FFFFFF" {
		t.Fatalf("text %+v (box colour is required by the validator, so that op is rejected)", l)
	}
	if len(state.ImageLayers) != 1 || state.ImageLayers[0].AssetID != image || state.ImageLayers[0].Width != .2 || applied[4].Target != state.ImageLayers[0].ID {
		t.Fatalf("images %+v", state.ImageLayers)
	}
	if c := state.AudioClips[0]; c.Volume != .2 || c.FadeOut != 1 || c.FadeIn != 0 || c.AssetID != audio {
		t.Fatalf("audio %+v", c)
	}
	if base.Segments[0].Start != 0 || len(base.Segments) != 2 {
		t.Fatal("base state mutated")
	}
	if _, _, rejected = applyAIEditResult(base, aiEditResult{Operations: []aiEditOp{{Tool: "add_image_layer", AssetID: aiEditS(image)}}}, assets, false); rejected != 1 {
		t.Fatal("image tools require the image schema")
	}
}

type aiEditFakeProvider struct {
	out    []byte
	err    error
	prompt string
	system string
	tokens int
}

func (p *aiEditFakeProvider) Available() bool   { return true }
func (p *aiEditFakeProvider) Signature() string { return "fake" }
func (p *aiEditFakeProvider) Generate(ctx context.Context, system, prompt string, _ []editorai.Image, _ json.RawMessage) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("provider call must be bounded")
	}
	p.prompt, p.system, p.tokens = prompt, system, editorai.MaxOutputTokens(ctx)
	return p.out, p.err
}

func TestAIEditPreviewProviderOutcomes(t *testing.T) {
	t.Parallel()
	base, assets := aiEditTestState(t)
	base.TextLayers[0].Text = strings.Repeat("ก", 400)
	raw := func(v any) datatypes.JSON { b, _ := json.Marshal(v); return b }
	p := &models.VideoEditorProject{Base: models.Base{ID: uuid.New()}, Title: "งาน", AspectRatio: "9:16", Revision: 7, Segments: raw(base.Segments), AudioClips: raw(base.AudioClips), TextLayers: raw(base.TextLayers), ImageLayers: raw(base.ImageLayers)}
	ctx := context.Background()
	for name, tc := range map[string]struct {
		provider *aiEditFakeProvider
		want     error
	}{
		"rate":        {&aiEditFakeProvider{err: &editorai.RateError{RetryAt: time.Now()}}, nil},
		"unavailable": {&aiEditFakeProvider{err: editorai.ErrUnavailable}, ErrAIEditProvider},
		"timeout":     {&aiEditFakeProvider{err: context.DeadlineExceeded}, ErrAIEditProvider},
		"invalid":     {&aiEditFakeProvider{err: editorai.ErrInvalid}, editorai.ErrInvalid},
		"bad shape":   {&aiEditFakeProvider{out: []byte(`{"summary":"x","operations":[],"extra":1}`)}, editorai.ErrInvalid},
		"nothing":     {&aiEditFakeProvider{out: []byte(`{"summary":"ทำไม่ได้","operations":[]}`)}, ErrAIEditNoChanges},
		"same values": {&aiEditFakeProvider{out: []byte(`{"summary":"x","operations":[{"tool":"set_segment","target":"` + base.Segments[0].ID + `","description":"","params":"{\"volume\":1}"}]}`)}, ErrAIEditNoChanges},
	} {
		_, err := previewAIEdit(ctx, tc.provider, p, assets, "ตัดต่อ", true, nil)
		var rate *editorai.RateError
		if name == "rate" && !errors.As(err, &rate) || name != "rate" && !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	fake := &aiEditFakeProvider{out: []byte(`{"summary":"ลดเสียง\u0007","operations":[{"tool":"update_audio_clip","target":"` + base.AudioClips[0].ID + `","description":"","params":"{\"volume\":0.5}"}]}`)}
	charged := 0
	got, err := previewAIEdit(ctx, fake, p, assets, "เสียงเบาลง", false, func() error { charged++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if charged != 1 || fake.tokens != aiStructuredOutputTokens || fake.tokens <= editorai.DefaultMaxOutputTokens {
		t.Fatalf("charged %d, maxOutputTokens %d", charged, fake.tokens)
	}
	if got.BaseRevision != 7 || got.Summary != "ลดเสียง" || got.Operations[0].Description != "ปรับเสียง" || got.Proposal.ImageLayers != nil || got.Proposal.AudioClips[0].Volume != .5 || got.Proposal.Title != "งาน" {
		t.Fatalf("preview %+v", got)
	}
	var prompt aiEditPrompt
	if err = json.Unmarshal([]byte(fake.prompt), &prompt); err != nil || prompt.Instruction != "เสียงเบาลง" || len([]rune(prompt.Timeline.TextLayers[0].Text)) != 121 || prompt.Timeline.Segments[1].TimelineStart != 3.5 {
		t.Fatalf("prompt %+v %v", prompt.Timeline, err)
	}
	for _, a := range prompt.Assets {
		if a.Kind == "image" {
			t.Fatal("image assets offered without the image schema")
		}
	}
	if !strings.Contains(fake.system, "untrusted DATA") {
		t.Fatal("instruction must be framed as data")
	}
}

func TestAIEditLimiter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 8, 23, 0, 0, 0, time.UTC)
	l := newAIEditLimiter()
	l.now = func() time.Time { return now }
	release, err := l.enter("u")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.enter("u"); !errors.Is(err, ErrAIEditBusy) {
		t.Fatal("concurrent preview", err)
	}
	if r, err := l.enter("other"); err != nil {
		t.Fatal(err)
	} else {
		r()
	}
	release()
	for i := 0; i < 5; i++ {
		r, err := l.enter("u")
		if err != nil {
			t.Fatal("entering without charging must not consume quota", err)
		}
		r()
	}
	for i := 0; i < aiEditMaxPerDay; i++ {
		if err = l.charge("u"); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err = l.enter("u"); !errors.Is(err, repo.ErrAIDaily) {
		t.Fatal("daily cap", err)
	}
	if err = l.charge("u"); !errors.Is(err, repo.ErrAIDaily) {
		t.Fatal("charge over cap", err)
	}
	now = now.Add(2 * time.Hour)
	if _, err = l.enter("u"); err != nil {
		t.Fatal("new UTC day", err)
	}
}

func TestAIEditQuotaChargedOnlyBeforeProviderCall(t *testing.T) {
	t.Parallel()
	base, assets := aiEditTestState(t)
	raw := func(v any) datatypes.JSON { b, _ := json.Marshal(v); return b }
	project := func(s aiEditState) *models.VideoEditorProject {
		return &models.VideoEditorProject{Base: models.Base{ID: uuid.New()}, Title: "t", AspectRatio: "9:16", Revision: 1, Segments: raw(s.Segments), AudioClips: raw(s.AudioClips), TextLayers: raw(s.TextLayers), ImageLayers: raw(s.ImageLayers)}
	}
	ok := []byte(`{"summary":"x","operations":[{"tool":"update_audio_clip","target":"` + base.AudioClips[0].ID + `","description":"","params":"{\"volume\":0.5}"}]}`)
	charged := 0
	charge := func() error { charged++; return nil }
	ctx := context.Background()

	broken := base.clone()
	broken.Segments[0].End = 99
	fake := &aiEditFakeProvider{out: ok}
	if _, err := previewAIEdit(ctx, fake, project(broken), assets, "x", true, charge); !errors.Is(err, ErrAIEditBaseInvalid) || charged != 0 || fake.prompt != "" {
		t.Fatalf("invalid stored timeline must fail before quota/provider: %v %d", err, charged)
	}
	many := append([]models.VideoEditorAsset{}, assets...)
	for i := 0; i < 400; i++ {
		a := aiToolAsset("image", 0)
		a.Name = strings.Repeat("ภ", 80)
		many = append(many, a)
	}
	if _, err := previewAIEdit(ctx, fake, project(base), many, "x", true, charge); !errors.Is(err, editorai.ErrTooLarge) || charged != 0 || fake.prompt != "" {
		t.Fatalf("oversized prompt must fail before quota/provider: %v %d", err, charged)
	}
	fake.prompt, charged = "", 0
	if _, err := previewAIEdit(ctx, fake, project(base), assets, "x", true, func() error { return repo.ErrAIDaily }); !errors.Is(err, repo.ErrAIDaily) || fake.prompt != "" {
		t.Fatal("exhausted quota must stop before the provider", err)
	}
	if _, err := previewAIEdit(ctx, &aiEditFakeProvider{err: editorai.ErrUnavailable}, project(base), assets, "x", true, charge); !errors.Is(err, ErrAIEditProvider) || charged != 1 {
		t.Fatal("provider failure is charged (no refund)", err, charged)
	}
}

func TestAIEditIgnoresNoopExtraFields(t *testing.T) {
	t.Parallel()
	base, assets := aiEditTestState(t)
	seg, text := base.Segments[1], base.TextLayers[0]
	ops := []aiEditOp{
		// volume/start/index equal current values, text is empty, transition "none": all no-ops for trim/text tools.
		{Tool: "trim_segment", Target: seg.ID, Start: aiEditF(6), Volume: aiEditF(seg.Volume), Index: aiEditF(1), Text: aiEditS(""), Speed: aiEditF(0)},
		{Tool: "update_text", Target: text.ID, Y: aiEditF(.5), Start: aiEditF(text.Start), Speed: aiEditF(0), Transition: aiEditS("none")},
		// A disallowed field that would change something still rejects the operation.
		{Tool: "trim_segment", Target: seg.ID, End: aiEditF(8), Volume: aiEditF(.2)},
		// Only no-op extras and no accepted field: nothing to do.
		{Tool: "set_segment", Target: seg.ID, Text: aiEditS("")},
	}
	state, applied, rejected := applyAIEditResult(base, aiEditResult{Operations: ops}, assets, true)
	if len(applied) != 2 || rejected != 2 || state.Segments[1].Start != 6 || state.Segments[1].End != 9 || state.Segments[1].Volume != 1 || state.TextLayers[0].Y != .5 {
		t.Fatalf("applied %+v rejected %d state %+v", applied, rejected, state)
	}
}

func TestAIDraftNormalizesDefaultsLikeEditTool(t *testing.T) {
	t.Parallel()
	asset := uuid.NewString()
	segments := []models.EditorAIDraftSegment{{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 2, Speed: 1, VideoEditorClipLook: models.VideoEditorClipLook{Zoom: 1, Fit: "cover"}}}
	sanitizeAIDraftEffects(segments)
	if segments[0].Speed != 0 || segments[0].Zoom != 0 || segments[0].Fit != "cover" {
		t.Fatalf("speed/zoom 1 must normalize to 0: %+v", segments[0])
	}
	got := sanitizeAITextStyle(&models.EditorAITextStyle{OutlineColor: "#000000", BoxColor: "#111111"})
	if got == nil || got.OutlineWidth != 3 || got.BoxOpacity != .6 {
		t.Fatalf("colour without size should get the edit tool default %+v", got)
	}
	l := aiApplyTextStyle(aiReferenceTextLayer(), &models.EditorAITextStyle{OutlineColor: "#000000"})
	if l.OutlineWidth != 3 || l.OutlineColor != "#000000" {
		t.Fatalf("applied %+v", l)
	}
}

// Gemini structured output accepts a subset of JSON Schema; every schema sent must stay in it.
var aiSchemaKeywords = map[string]bool{"type": true, "properties": true, "required": true, "items": true, "enum": true, "minimum": true, "maximum": true, "minItems": true, "maxItems": true, "description": true}

func TestAISchemasUseSupportedKeywords(t *testing.T) {
	t.Parallel()
	var walk func(path string, node map[string]any)
	walk = func(path string, node map[string]any) {
		for k, v := range node {
			if !aiSchemaKeywords[k] {
				t.Errorf("%s: unsupported keyword %q", path, k)
			}
			switch k {
			case "properties":
				for name, child := range v.(map[string]any) {
					walk(path+"."+name, child.(map[string]any))
				}
			case "items":
				walk(path+"[]", v.(map[string]any))
			}
		}
	}
	for name, raw := range map[string]json.RawMessage{
		"plan":       aiPlanSchema([]models.EditorAIEvidence{{ID: uuid.NewString(), Start: 0, End: 5}}, 10),
		"edit":       aiEditSchema,
		"concepts":   conceptSchema,
		"vision":     visionSchema,
		"refinement": refinementSchema,
	} {
		var node map[string]any
		if err := json.Unmarshal(raw, &node); err != nil {
			t.Fatal(name, err)
		}
		walk(name, node)
	}
}

func TestAIEditPreviewInputAndAvailability(t *testing.T) {
	t.Parallel()
	editor := NewVideoEditorFacade(nil, nil, nil)
	disabled := NewVideoEditorAIFacade(nil, editor, &aiEditFakeProvider{}, nil, EditorAIOptions{})
	ctx, project, user := context.Background(), uuid.NewString(), uuid.NewString()
	for name, in := range map[string]req.PreviewVideoEditorAIEdit{
		"empty":     {Instruction: "  ", Revision: 1},
		"too long":  {Instruction: strings.Repeat("ก", 501), Revision: 1},
		"bad utf8":  {Instruction: "\xff", Revision: 1},
		"revision0": {Instruction: "x", Revision: 0},
	} {
		if _, err := disabled.PreviewEdit(ctx, project, user, in); !errors.Is(err, ErrAIEditInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := disabled.PreviewEdit(ctx, "nope", user, req.PreviewVideoEditorAIEdit{Instruction: "x", Revision: 1}); !errors.Is(err, repo.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := disabled.PreviewEdit(ctx, project, user, req.PreviewVideoEditorAIEdit{Instruction: strings.Repeat("ก", 500), Revision: 1}); !errors.Is(err, editorai.ErrUnavailable) {
		t.Fatal("disabled AI must be unavailable", err)
	}
	if disabled.Capabilities().InstructionEdits {
		t.Fatal("capability advertised while disabled")
	}
	enabledNoMedia := NewVideoEditorAIFacade(nil, editor, &aiEditFakeProvider{}, nil, EditorAIOptions{Enabled: true})
	if enabledNoMedia.instructionEditsAvailable() {
		t.Fatal("editor media unavailable")
	}
}

func TestAIEditWireParamsDecodedPerCall(t *testing.T) {
	t.Parallel()
	got := aiEditFromWire(aiEditWireResult{Summary: "s", Operations: []aiEditWireOp{
		{Tool: "set_segment", Target: "a", Description: "d", Params: `{"speed":1.5,"transition":"fade"}`},
		{Tool: "delete_text", Target: "b", Params: "{}"},
		{Tool: "delete_text", Target: "c", Params: ""},
		{Tool: "set_segment", Target: "d", Params: `{"speed":`},
		{Tool: "set_segment", Target: "e", Params: `{"unknown":1}`},
		{Tool: "set_segment", Target: "f", Params: `{"tool":"delete_segment"}`},
		{Tool: "set_segment", Target: "g", Params: `{"speed":1}{"speed":2}`},
	}})
	if len(got.Operations) != 7 || got.Summary != "s" {
		t.Fatalf("%+v", got)
	}
	first := got.Operations[0]
	if first.Tool != "set_segment" || first.Target != "a" || first.Description != "d" || first.Speed == nil || *first.Speed != 1.5 || first.Transition == nil || *first.Transition != "fade" {
		t.Fatalf("valid params not decoded: %+v", first)
	}
	if got.Operations[1].Tool != "delete_text" || got.Operations[2].Tool != "delete_text" {
		t.Fatal("empty params must keep the call")
	}
	for _, op := range got.Operations[3:] {
		if op.Tool != aiEditInvalidTool {
			t.Fatalf("bad params must reject only that call: %+v", op)
		}
	}
	if !strings.Contains(string(aiEditSchema), `"params"`) || strings.Contains(string(aiEditSchema), `"speed"`) && !strings.Contains(string(aiEditSchema), `\"speed\"`) {
		t.Fatal("edit schema must carry tool fields only inside params")
	}
}
