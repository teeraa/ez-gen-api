package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func veTestAsset(kind string, duration float64) models.VideoEditorAsset {
	return models.VideoEditorAsset{Base: models.Base{ID: uuid.New()}, Kind: kind, Status: "ready", Duration: duration, SourceKey: "key-" + kind}
}
func veTestText() models.VideoEditorTextLayer {
	return models.VideoEditorTextLayer{ID: uuid.NewString(), Text: "สวัสดี", Start: 0, End: 2, X: .5, Y: .8, FontSize: 48, Color: "#FFFFFF", Align: "center"}
}

func TestVEAudioClipValidation(t *testing.T) {
	t.Parallel()
	audio, video := veTestAsset("audio", 10), veTestAsset("video", 10)
	assets := []models.VideoEditorAsset{audio, video}
	ok := models.VideoEditorAudioClip{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 3, Start: 1, End: 9, Volume: .5}
	if e := validateVEAudioClips([]models.VideoEditorAudioClip{ok}, assets); e != nil {
		t.Fatal("valid clip rejected", e)
	}
	if e := validateVEAudioClips(nil, assets); e != nil {
		t.Fatal("empty rejected", e)
	}
	cases := map[string]func(*models.VideoEditorAudioClip){
		"video asset":    func(c *models.VideoEditorAudioClip) { c.AssetID = video.ID.String() },
		"unknown asset":  func(c *models.VideoEditorAudioClip) { c.AssetID = uuid.NewString() },
		"bad id":         func(c *models.VideoEditorAudioClip) { c.ID = "clip-1" },
		"negative at":    func(c *models.VideoEditorAudioClip) { c.At = -.1 },
		"nan at":         func(c *models.VideoEditorAudioClip) { c.At = math.NaN() },
		"start>=end":     func(c *models.VideoEditorAudioClip) { c.Start = 9 },
		"past source":    func(c *models.VideoEditorAudioClip) { c.End = 10.5 },
		"negative start": func(c *models.VideoEditorAudioClip) { c.Start = -1 },
		"volume>1":       func(c *models.VideoEditorAudioClip) { c.Volume = 1.01 },
		"volume<0":       func(c *models.VideoEditorAudioClip) { c.Volume = -.01 },
		"past timeline":  func(c *models.VideoEditorAudioClip) { c.At = 1195 },
		"inf end":        func(c *models.VideoEditorAudioClip) { c.End = math.Inf(1) },
	}
	for name, mutate := range cases {
		c := ok
		mutate(&c)
		if e := validateVEAudioClips([]models.VideoEditorAudioClip{c}, assets); !errors.Is(e, ErrVEInvalid) {
			t.Errorf("%s accepted: %v", name, e)
		}
	}
	if e := validateVEAudioClips([]models.VideoEditorAudioClip{ok, ok}, assets); !errors.Is(e, ErrVEInvalid) {
		t.Error("duplicate id accepted")
	}
	many := []models.VideoEditorAudioClip{}
	for i := 0; i < 21; i++ {
		c := ok
		c.ID = uuid.NewString()
		many = append(many, c)
	}
	if e := validateVEAudioClips(many[:20], assets); e != nil {
		t.Fatal("20 clips rejected", e)
	}
	if e := validateVEAudioClips(many, assets); !errors.Is(e, ErrVEInvalid) {
		t.Error("21 clips accepted")
	}
	processing := audio
	processing.Status = "processing"
	if e := validateVEAudioClips([]models.VideoEditorAudioClip{ok}, []models.VideoEditorAsset{processing}); !errors.Is(e, ErrVEInvalid) {
		t.Error("processing asset accepted")
	}
}

func TestVESegmentsRequireVideoAssets(t *testing.T) {
	t.Parallel()
	audio, video, legacy := veTestAsset("audio", 10), veTestAsset("video", 10), veTestAsset("", 10)
	assets := []models.VideoEditorAsset{audio, video, legacy}
	seg := func(id string) []models.VideoEditorSegment {
		return []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: id, Start: 0, End: 1, Volume: 1}}
	}
	if e := validateVESegments(seg(video.ID.String()), assets); e != nil {
		t.Fatal(e)
	}
	if e := validateVESegments(seg(legacy.ID.String()), assets); e != nil {
		t.Fatal("legacy kind-less asset must count as video", e)
	}
	if e := validateVESegments(seg(audio.ID.String()), assets); !errors.Is(e, ErrVEInvalid) {
		t.Fatal("audio asset accepted as video segment", e)
	}
}

func TestVETextLayerValidationAndNormalization(t *testing.T) {
	t.Parallel()
	l := veTestText()
	l.Text = "  บรรทัดหนึ่ง\r\nบรรทัดสอง  "
	out, e := normalizeVETextLayers([]models.VideoEditorTextLayer{l})
	if e != nil || len(out) != 1 || out[0].Text != "บรรทัดหนึ่ง\nบรรทัดสอง" {
		t.Fatalf("normalize %+v %v", out, e)
	}
	if out, e = normalizeVETextLayers(nil); e != nil || out == nil || len(out) != 0 {
		t.Fatal("nil layers must normalize to empty array", e)
	}
	sixLines := strings.Repeat("ก\n", 5) + "ก"
	if _, e = normalizeVETextLayers([]models.VideoEditorTextLayer{func() models.VideoEditorTextLayer { x := veTestText(); x.Text = sixLines; return x }()}); e != nil {
		t.Fatal("six lines rejected", e)
	}
	max := veTestText()
	max.Text = strings.Repeat("ก", 500)
	if _, e = normalizeVETextLayers([]models.VideoEditorTextLayer{max}); e != nil {
		t.Fatal("500 Thai runes rejected", e)
	}
	cases := map[string]func(*models.VideoEditorTextLayer){
		"blank":       func(x *models.VideoEditorTextLayer) { x.Text = " \n\t " },
		"501 runes":   func(x *models.VideoEditorTextLayer) { x.Text = strings.Repeat("ก", 501) },
		"7 lines":     func(x *models.VideoEditorTextLayer) { x.Text = sixLines + "\nข" },
		"control":     func(x *models.VideoEditorTextLayer) { x.Text = "a\x00b" },
		"bad utf8":    func(x *models.VideoEditorTextLayer) { x.Text = "a\xffb" },
		"bad id":      func(x *models.VideoEditorTextLayer) { x.ID = "" },
		"start>=end":  func(x *models.VideoEditorTextLayer) { x.Start = 2 },
		"neg start":   func(x *models.VideoEditorTextLayer) { x.Start = -1 },
		"end>1200":    func(x *models.VideoEditorTextLayer) { x.End = 1200.5 },
		"x>1":         func(x *models.VideoEditorTextLayer) { x.X = 1.1 },
		"y<0":         func(x *models.VideoEditorTextLayer) { x.Y = -.1 },
		"nan x":       func(x *models.VideoEditorTextLayer) { x.X = math.NaN() },
		"font<12":     func(x *models.VideoEditorTextLayer) { x.FontSize = 11 },
		"font>200":    func(x *models.VideoEditorTextLayer) { x.FontSize = 201 },
		"color short": func(x *models.VideoEditorTextLayer) { x.Color = "#FFF" },
		"color name":  func(x *models.VideoEditorTextLayer) { x.Color = "white" },
		"color nohash": func(x *models.VideoEditorTextLayer) {
			x.Color = "FFFFFF"
		},
		"align": func(x *models.VideoEditorTextLayer) { x.Align = "justify" },
	}
	for name, mutate := range cases {
		x := veTestText()
		mutate(&x)
		if _, e := normalizeVETextLayers([]models.VideoEditorTextLayer{x}); !errors.Is(e, ErrVEInvalid) {
			t.Errorf("%s accepted", name)
		}
	}
	dup := veTestText()
	if _, e = normalizeVETextLayers([]models.VideoEditorTextLayer{dup, dup}); !errors.Is(e, ErrVEInvalid) {
		t.Error("duplicate id accepted")
	}
	many := []models.VideoEditorTextLayer{}
	for i := 0; i < 51; i++ {
		many = append(many, veTestText())
	}
	if _, e = normalizeVETextLayers(many[:50]); e != nil {
		t.Fatal("50 layers rejected", e)
	}
	if _, e = normalizeVETextLayers(many); !errors.Is(e, ErrVEInvalid) {
		t.Error("51 layers accepted")
	}
}

func TestVESnapshotCutsAudioAndClipsText(t *testing.T) {
	t.Parallel()
	video, audio := veTestAsset("video", 10), veTestAsset("audio", 30)
	segments := []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 3, Volume: 1}, {ID: uuid.NewString(), AssetID: video.ID.String(), Start: 5, End: 7, Volume: 0}}
	clips := []models.VideoEditorAudioClip{
		{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 1, Start: 2, End: 4, Volume: .5}, // inside
		{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 4, Start: 0, End: 10, Volume: 1}, // crosses end
		{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 5, Start: 0, End: 1, Volume: 1},  // starts at end
		{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 20, Start: 0, End: 1, Volume: 1}, // after end
	}
	a, b, c := veTestText(), veTestText(), veTestText()
	a.Start, a.End = 0, 2
	b.Start, b.End = 4, 9
	c.Start, c.End = 5, 6
	snap, total := buildVESnapshot("16:9", "720p", segments, clips, []models.VideoEditorTextLayer{a, b, c}, []models.VideoEditorAsset{video, audio})
	if total != 5 || len(snap.Clips) != 2 || snap.Clips[0].Key != "key-video" {
		t.Fatalf("video snapshot %+v %v", snap.Clips, total)
	}
	if len(snap.AudioClips) != 2 || snap.AudioClips[0].Key != "key-audio" || snap.AudioClips[0].End != 4 || snap.AudioClips[0].At != 1 || snap.AudioClips[0].Volume != .5 {
		t.Fatalf("audio snapshot %+v", snap.AudioClips)
	}
	if snap.AudioClips[1].Start != 0 || snap.AudioClips[1].End != 1 || snap.AudioClips[1].At != 4 {
		t.Fatalf("crossing audio must be cut at the end: %+v", snap.AudioClips[1])
	}
	if len(snap.TextLayers) != 2 || snap.TextLayers[0].End != 2 || snap.TextLayers[1].Start != 4 || snap.TextLayers[1].End != 5 {
		t.Fatalf("text snapshot %+v", snap.TextLayers)
	}
	raw, _ := json.Marshal(snap)
	for _, key := range []string{`"audioClips":[`, `"textLayers":[`, `"key":"key-audio"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("snapshot JSON missing %s: %s", key, raw)
		}
	}
	empty, _ := buildVESnapshot("16:9", "720p", segments, nil, nil, []models.VideoEditorAsset{video})
	raw, _ = json.Marshal(empty)
	if !strings.Contains(string(raw), `"audioClips":[]`) || !strings.Contains(string(raw), `"textLayers":[]`) {
		t.Fatalf("empty arrays expected: %s", raw)
	}
	render := veRenderText(snap.TextLayers)
	if len(render) != 2 || !render[0].Absolute || !render[0].HasXY || render[0].Color != "FFFFFF" || render[1].EndTime != 5 {
		t.Fatalf("render text %+v", render)
	}
}

func TestVELegacyProjectReturnsEmptyArrays(t *testing.T) {
	t.Parallel()
	for _, raw := range []datatypes.JSON{nil, datatypes.JSON("null"), datatypes.JSON("[]")} {
		d := veProjectDTO(&models.VideoEditorProject{Segments: raw, AudioClips: raw, TextLayers: raw})
		b, _ := json.Marshal(d)
		if !strings.Contains(string(b), `"audioClips":[]`) || !strings.Contains(string(b), `"textLayers":[]`) || !strings.Contains(string(b), `"segments":[]`) {
			t.Fatalf("legacy %q -> %s", raw, b)
		}
	}
	legacy := veTestAsset("", 1)
	if d, _ := (&VideoEditorFacade{}).assetDTO(t.Context(), &models.VideoEditorAsset{Base: legacy.Base, Status: "failed"}); d.Kind != "video" {
		t.Fatalf("legacy asset kind %q", d.Kind)
	}
}

func TestVEUploadMIMECompatibility(t *testing.T) {
	t.Parallel()
	accept := [][2]string{{"audio/mpeg", "audio/mpeg"}, {"audio/mpeg", "audio/mp3"}, {"audio/wav", "audio/x-wav"}, {"audio/wav", "audio/wav"}, {"audio/mp4", "audio/x-m4a"}, {"audio/mp4", "audio/mp4"}, {"audio/mp4", "video/mp4"}, {"audio/mpeg", "application/octet-stream"}, {"video/mp4", "video/quicktime"}, {"video/webm", "video/webm"}}
	for _, c := range accept {
		if !veMIMECompatible(c[0], c[1]) || !veUploadMIME[c[1]] {
			t.Errorf("rejected %v", c)
		}
	}
	reject := [][2]string{{"audio/mpeg", "audio/wav"}, {"audio/wav", "audio/mpeg"}, {"video/mp4", "audio/mp4"}, {"video/mp4", "video/webm"}, {"video/webm", "video/mp4"}, {"audio/mp4", "audio/mpeg"}}
	for _, c := range reject {
		if veMIMECompatible(c[0], c[1]) {
			t.Errorf("accepted %v", c)
		}
	}
	// Still images became media with image layers (migration 028); plain text never is.
	if veUploadMIME["text/plain"] || !veUploadMIME["image/png"] {
		t.Error("upload MIME allow-list changed unexpectedly")
	}
}

func TestVECapabilitiesAdvertiseAudioText(t *testing.T) {
	t.Parallel()
	c := (&VideoEditorFacade{}).Capabilities()
	if c.MaxAudioClips != 20 || c.MaxTextLayers != 50 || c.TextOverlays || strings.Join(c.AudioFormats, ",") != "mp3,wav,m4a" {
		t.Fatalf("capabilities %+v", c)
	}
}

func TestVETextRejectsInvisibleAndBidiControls(t *testing.T) {
	t.Parallel()
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF, 0x2028, 0x2029, '\t', 0x85} {
		x := veTestText()
		x.Text = "ก" + string(r) + "ข"
		if _, e := normalizeVETextLayers([]models.VideoEditorTextLayer{x}); !errors.Is(e, ErrVEInvalid) {
			t.Errorf("U+%04X accepted", r)
		}
	}
	// Thai combining marks, NBSP and ordinary punctuation stay valid.
	x := veTestText()
	x.Text = "ก่ข้\u00a0\"ทดสอบ\" — 100%"
	if _, e := normalizeVETextLayers([]models.VideoEditorTextLayer{x}); e != nil {
		t.Fatal("ordinary text rejected", e)
	}
}

func TestVESnapshotDropsSubMillisecondRemainders(t *testing.T) {
	t.Parallel()
	video, audio := veTestAsset("video", 10), veTestAsset("audio", 30)
	segments := []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 2, Volume: 1}}
	clips := []models.VideoEditorAudioClip{
		{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 1.9995, Start: 0, End: 5, Volume: 1}, // 0.5 ms left
		{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 1.999, Start: 0, End: 5, Volume: 1},  // exactly 1 ms left
	}
	a, b := veTestText(), veTestText()
	a.Start, a.End = 1.9995, 3
	b.Start, b.End = 1.998, 3
	snap, _ := buildVESnapshot("16:9", "720p", segments, clips, []models.VideoEditorTextLayer{a, b}, []models.VideoEditorAsset{video, audio})
	if len(snap.AudioClips) != 1 || snap.AudioClips[0].At != 1.999 {
		t.Fatalf("audio %+v", snap.AudioClips)
	}
	if len(snap.TextLayers) != 1 || snap.TextLayers[0].ID != b.ID || snap.TextLayers[0].End != 2 {
		t.Fatalf("text %+v", snap.TextLayers)
	}
}

type veFakeTextProcessor struct {
	videoprocessing.VideoProcessor
	ok    atomic.Bool
	calls atomic.Int32
}

func (p *veFakeTextProcessor) SupportsThaiTextOverlays(context.Context) bool {
	p.calls.Add(1)
	return p.ok.Load()
}

func TestVETextCapabilityCachesSuccessAndRetriesFailure(t *testing.T) {
	t.Parallel()
	p := &veFakeTextProcessor{}
	f := &VideoEditorFacade{processor: p, available: true, textRetry: 50 * time.Millisecond}
	if f.TextOverlays() || f.TextOverlays() || p.calls.Load() != 1 {
		t.Fatalf("failure must be cached within the retry window: calls=%d", p.calls.Load())
	}
	p.ok.Store(true)
	time.Sleep(60 * time.Millisecond)
	if !f.TextOverlays() || p.calls.Load() != 2 {
		t.Fatalf("failure must be re-probed after the window: calls=%d", p.calls.Load())
	}
	p.ok.Store(false)
	time.Sleep(60 * time.Millisecond)
	if !f.TextOverlays() || p.calls.Load() != 2 {
		t.Fatalf("success must be cached: calls=%d", p.calls.Load())
	}
	if (&VideoEditorFacade{processor: p}).TextOverlays() {
		t.Fatal("unavailable editor reported text overlays")
	}
}

func TestVETextTransformsPersistAndRender(t *testing.T) {
	t.Parallel()
	sx, sy := 2.0, .5
	layer := veTestText()
	layer.ScaleX, layer.ScaleY = &sx, &sy
	normalized, err := normalizeVETextLayers([]models.VideoEditorTextLayer{layer})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	project := models.VideoEditorProject{TextLayers: raw}
	restored := veTextLayers(&project)
	if len(restored) != 1 || restored[0].ScaleX == nil || *restored[0].ScaleX != 2 || restored[0].ScaleY == nil || *restored[0].ScaleY != .5 {
		t.Fatalf("transform lost on JSON roundtrip: %s", raw)
	}
	video := veTestAsset("video", 3)
	snap, _ := buildVESnapshot("16:9", "720p", []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 3, Volume: 1}}, nil, restored, []models.VideoEditorAsset{video})
	rendered := veRenderText(snap.TextLayers)
	if len(rendered) != 1 || rendered[0].ScaleX != 2 || rendered[0].ScaleY != .5 {
		t.Fatalf("transform lost on snapshot/render: %+v", rendered)
	}
	legacy := veRenderText([]models.VideoEditorTextLayer{veTestText()})
	if legacy[0].ScaleX != 1 || legacy[0].ScaleY != 1 {
		t.Fatal("legacy transforms changed")
	}
	if !(&VideoEditorFacade{}).Capabilities().TextTransforms {
		t.Fatal("missing transform capability")
	}
}

func TestVETextTransformsRejectInvalid(t *testing.T) {
	t.Parallel()
	for _, value := range []float64{0, -1, .099, 5.001, math.NaN(), math.Inf(1)} {
		for _, axis := range []string{"x", "y"} {
			layer := veTestText()
			if axis == "x" {
				layer.ScaleX = &value
			} else {
				layer.ScaleY = &value
			}
			if _, err := normalizeVETextLayers([]models.VideoEditorTextLayer{layer}); !errors.Is(err, ErrVEInvalid) {
				t.Fatalf("accepted %s scale %v", axis, value)
			}
		}
	}
	for _, value := range []float64{.1, 1, 5} {
		layer := veTestText()
		layer.ScaleX = &value
		layer.ScaleY = &value
		if _, err := normalizeVETextLayers([]models.VideoEditorTextLayer{layer}); err != nil {
			t.Fatalf("rejected scale %v: %v", value, err)
		}
	}
}
