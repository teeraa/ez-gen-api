package facade_open_api

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

func TestVESegmentSpeedAndFadesValidation(t *testing.T) {
	t.Parallel()
	video := veTestAsset("video", 10)
	assets := []models.VideoEditorAsset{video}
	seg := func(change func(*models.VideoEditorSegment)) []models.VideoEditorSegment {
		s := models.VideoEditorSegment{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 4, Volume: 1}
		change(&s)
		return []models.VideoEditorSegment{s}
	}
	for name, change := range map[string]func(*models.VideoEditorSegment){
		"legacy":       func(*models.VideoEditorSegment) {},
		"fast":         func(s *models.VideoEditorSegment) { s.Speed = 4 },
		"slow":         func(s *models.VideoEditorSegment) { s.Speed = .25 },
		"fades fill":   func(s *models.VideoEditorSegment) { s.FadeIn, s.FadeOut = 2, 2 },
		"fade at 2x":   func(s *models.VideoEditorSegment) { s.Speed, s.FadeIn, s.FadeOut = 2, 1, 1 },
		"one fade max": func(s *models.VideoEditorSegment) { s.End, s.FadeOut = 10, 10 },
	} {
		if e := validateVESegments(seg(change), assets); e != nil {
			t.Fatalf("%s rejected: %v", name, e)
		}
	}
	for name, change := range map[string]func(*models.VideoEditorSegment){
		"too slow":         func(s *models.VideoEditorSegment) { s.Speed = .2 },
		"too fast":         func(s *models.VideoEditorSegment) { s.Speed = 4.5 },
		"negative speed":   func(s *models.VideoEditorSegment) { s.Speed = -1 },
		"nan speed":        func(s *models.VideoEditorSegment) { s.Speed = math.NaN() },
		"negative fade":    func(s *models.VideoEditorSegment) { s.FadeIn = -.1 },
		"fade too long":    func(s *models.VideoEditorSegment) { s.End, s.FadeIn = 10, 10.5 },
		"fades overflow":   func(s *models.VideoEditorSegment) { s.FadeIn, s.FadeOut = 3, 2 },
		"overflow at 2x":   func(s *models.VideoEditorSegment) { s.Speed, s.FadeIn, s.FadeOut = 2, 1.5, 1 },
		"infinite fadeout": func(s *models.VideoEditorSegment) { s.FadeOut = math.Inf(1) },
	} {
		if e := validateVESegments(seg(change), assets); !errors.Is(e, ErrVEInvalid) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
	// The 20-minute ceiling counts timeline time, so slowing a clip down uses more of it.
	long := veTestAsset("video", 700)
	slow := []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: long.ID.String(), Start: 0, End: 700, Volume: 1, Speed: .5}}
	if e := validateVESegments(slow, []models.VideoEditorAsset{long}); !errors.Is(e, ErrVEInvalid) {
		t.Fatalf("1400 s of slowed video accepted: %v", e)
	}
	slow[0].Speed = 2
	if e := validateVESegments(slow, []models.VideoEditorAsset{long}); e != nil {
		t.Fatalf("350 s of sped-up video rejected: %v", e)
	}
}

func TestVEAudioClipFadesAndDetachedVideoSound(t *testing.T) {
	t.Parallel()
	audio, silent, sound := veTestAsset("audio", 10), veTestAsset("video", 10), veTestAsset("video", 10)
	sound.HasAudio = true
	assets := []models.VideoEditorAsset{audio, silent, sound}
	clip := func(asset models.VideoEditorAsset, in, out float64) []models.VideoEditorAudioClip {
		return []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: asset.ID.String(), At: 1, Start: 0, End: 4, Volume: 1, FadeIn: in, FadeOut: out}}
	}
	if e := validateVEAudioClips(clip(audio, 1, 2), assets); e != nil {
		t.Fatal("faded clip rejected", e)
	}
	if e := validateVEAudioClips(clip(sound, 0, 0), assets); e != nil {
		t.Fatal("detached sound of a video with audio rejected", e)
	}
	for name, c := range map[string][]models.VideoEditorAudioClip{"silent video": clip(silent, 0, 0), "fades overflow": clip(audio, 3, 2), "negative fade": clip(audio, -1, 0), "fade over 10 s": clip(audio, 0, 11)} {
		if e := validateVEAudioClips(c, assets); !errors.Is(e, ErrVEInvalid) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
}

func TestVESnapshotCarriesSpeedAndFades(t *testing.T) {
	t.Parallel()
	video, audio := veTestAsset("video", 10), veTestAsset("audio", 20)
	segments := []models.VideoEditorSegment{
		{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 4, Volume: 1, Speed: 2, FadeIn: .5},
		{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 4, End: 5, Volume: 1, Speed: .5, FadeOut: 1},
	}
	// 4 s at 2× + 1 s at 0.5× = 4 s of video; the audio clip would run to 12 s and is cut.
	clips := []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: audio.ID.String(), At: 2, Start: 0, End: 10, Volume: 1, FadeIn: 1, FadeOut: 3}}
	snap, total := buildVESnapshot("9:16", "720p", segments, clips, nil, []models.VideoEditorAsset{video, audio})
	if math.Abs(total-4) > 1e-9 {
		t.Fatalf("total %v, want 4", total)
	}
	if c := snap.Clips[0]; c.Speed != 2 || c.FadeIn != .5 || c.FadeOut != 0 {
		t.Fatalf("clip effects lost: %+v", c)
	}
	if c := snap.Clips[1]; c.Speed != .5 || c.FadeOut != 1 {
		t.Fatalf("clip effects lost: %+v", c)
	}
	// 2 s remain after the cut: the 1 s fade-in stays, the 3 s fade-out shrinks to 1 s.
	if a := snap.AudioClips[0]; a.End != 2 || a.FadeIn != 1 || a.FadeOut != 1 {
		t.Fatalf("cut audio fades %+v", a)
	}
	// Stored and snapshot JSON omit the fields at their defaults, so older readers see no change.
	raw, _ := json.Marshal(models.VideoEditorSegment{ID: "a", AssetID: "b", End: 1, Volume: 1})
	if string(raw) != `{"id":"a","assetId":"b","start":0,"end":1,"volume":1}` {
		t.Fatalf("default segment JSON changed: %s", raw)
	}
}

func TestVECapabilitiesAdvertiseClipEffects(t *testing.T) {
	t.Parallel()
	if c := (&VideoEditorFacade{}).Capabilities(); !c.ClipEffects || !c.ClipStyles {
		t.Fatal("clip effects/styles capability missing")
	}
}

func TestVESegmentLookValidationAndSnapshot(t *testing.T) {
	t.Parallel()
	video := veTestAsset("video", 10)
	assets := []models.VideoEditorAsset{video}
	look := func(l models.VideoEditorClipLook) []models.VideoEditorSegment {
		return []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: 2, Volume: 1, VideoEditorClipLook: l}}
	}
	ok := models.VideoEditorClipLook{Brightness: -.4, Contrast: 1, Saturation: -1, Fit: "cover", Zoom: 3, PanX: -1, PanY: .5}
	if e := validateVESegments(look(ok), assets); e != nil {
		t.Fatal("valid look rejected", e)
	}
	for name, bad := range map[string]models.VideoEditorClipLook{"bright": {Brightness: 1.1}, "zoom low": {Zoom: .9}, "zoom high": {Zoom: 3.5}, "fit": {Fit: "fill"}, "pan": {PanX: -2}, "nan": {Contrast: math.NaN()}} {
		if e := validateVESegments(look(bad), assets); !errors.Is(e, ErrVEInvalid) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
	snap, _ := buildVESnapshot("9:16", "720p", look(ok), nil, nil, assets)
	if snap.Clips[0].VideoEditorClipLook != ok {
		t.Fatalf("look lost in snapshot: %+v", snap.Clips[0])
	}
	// The look is stored flat on the segment, and omitted entirely when unchanged.
	raw, _ := json.Marshal(look(models.VideoEditorClipLook{Fit: "cover", Zoom: 1.5})[0])
	var flat map[string]any
	_ = json.Unmarshal(raw, &flat)
	if flat["fit"] != "cover" || flat["zoom"] != 1.5 || flat["brightness"] != nil {
		t.Fatalf("segment JSON %s", raw)
	}
}

func TestVETextStyleValidationAndRender(t *testing.T) {
	t.Parallel()
	ok := veTestText()
	ok.OutlineColor, ok.OutlineWidth, ok.BoxColor, ok.BoxOpacity, ok.Animation = "#000000", 6, "#FFD43B", .8, "slide"
	got, e := normalizeVETextLayers([]models.VideoEditorTextLayer{ok})
	if e != nil {
		t.Fatal("styled text rejected", e)
	}
	r := veRenderText(got)[0]
	if r.OutlineColor != "000000" || r.OutlineWidth != 6 || r.BoxColor != "FFD43B" || r.BoxOpacity != .8 || r.Animation != "slide" {
		t.Fatalf("style lost on render: %+v", r)
	}
	for name, change := range map[string]func(*models.VideoEditorTextLayer){
		"outline width":     func(l *models.VideoEditorTextLayer) { l.OutlineWidth, l.OutlineColor = 21, "#000000" },
		"outline no colour": func(l *models.VideoEditorTextLayer) { l.OutlineWidth = 2 },
		"colour no outline": func(l *models.VideoEditorTextLayer) { l.OutlineColor = "#000000" },
		"box no colour":     func(l *models.VideoEditorTextLayer) { l.BoxOpacity = .5 },
		"box opacity":       func(l *models.VideoEditorTextLayer) { l.BoxOpacity, l.BoxColor = 1.5, "#000000" },
		"bad colour":        func(l *models.VideoEditorTextLayer) { l.BoxOpacity, l.BoxColor = .5, "red" },
		"animation":         func(l *models.VideoEditorTextLayer) { l.Animation = "spin" },
	} {
		l := veTestText()
		change(&l)
		if _, e := normalizeVETextLayers([]models.VideoEditorTextLayer{l}); !errors.Is(e, ErrVEInvalid) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
}

func TestVETransitionsValidationAndTimeline(t *testing.T) {
	t.Parallel()
	video, audio := veTestAsset("video", 10), veTestAsset("audio", 20)
	assets := []models.VideoEditorAsset{video, audio}
	seg := func(end float64, transition string, duration float64) models.VideoEditorSegment {
		return models.VideoEditorSegment{ID: uuid.NewString(), AssetID: video.ID.String(), Start: 0, End: end, Volume: 1, VideoEditorTransition: models.VideoEditorTransition{Transition: transition, TransitionDuration: duration}}
	}
	ok := []models.VideoEditorSegment{seg(4, "", 0), seg(2, "slideleft", 1), seg(3, "circleopen", .5)}
	if e := validateVESegments(ok, assets); e != nil {
		t.Fatal("valid transitions rejected", e)
	}
	if got := models.VideoEditorTimelineLength(ok); math.Abs(got-7.5) > 1e-9 {
		t.Fatalf("timeline %v, want 9 - 1.5 = 7.5", got)
	}
	snap, total := buildVESnapshot("16:9", "720p", ok, nil, nil, assets)
	if math.Abs(total-7.5) > 1e-9 || snap.Clips[1].Transition != "slideleft" || snap.Clips[1].TransitionDuration != 1 {
		t.Fatalf("snapshot %v %+v", total, snap.Clips[1])
	}
	for name, segments := range map[string][]models.VideoEditorSegment{
		"first segment": {seg(4, "fade", .5)},
		"unknown":       {seg(4, "", 0), seg(4, "spin", .5)},
		"over half":     {seg(4, "", 0), seg(1, "fade", .6)},
		"over 2 s":      {seg(10, "", 0), seg(10, "fade", 2.5)},
		"orphan":        {seg(4, "", 0), seg(4, "", .5)},
	} {
		if e := validateVESegments(segments, assets); !errors.Is(e, ErrVEInvalid) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
}

func TestVEImageLayersValidationSnapshotAndCapabilities(t *testing.T) {
	t.Parallel()
	image, video := veTestAsset("image", 0), veTestAsset("video", 10)
	assets := []models.VideoEditorAsset{image, video}
	layer := func(change func(*models.VideoEditorImageLayer)) []models.VideoEditorImageLayer {
		l := models.VideoEditorImageLayer{ID: uuid.NewString(), AssetID: image.ID.String(), Start: 1, End: 4, X: .9, Y: .1, Width: .2, Opacity: .8}
		change(&l)
		return []models.VideoEditorImageLayer{l}
	}
	if e := validateVEImageLayers(layer(func(*models.VideoEditorImageLayer) {}), assets); e != nil {
		t.Fatal("valid image layer rejected", e)
	}
	for name, change := range map[string]func(*models.VideoEditorImageLayer){
		"video asset": func(l *models.VideoEditorImageLayer) { l.AssetID = video.ID.String() },
		"tiny":        func(l *models.VideoEditorImageLayer) { l.Width = .01 },
		"transparent": func(l *models.VideoEditorImageLayer) { l.Opacity = 0 },
		"outside":     func(l *models.VideoEditorImageLayer) { l.X = 1.2 },
		"timing":      func(l *models.VideoEditorImageLayer) { l.End = l.Start },
		"bad id":      func(l *models.VideoEditorImageLayer) { l.ID = "x" },
		"nan":         func(l *models.VideoEditorImageLayer) { l.Y = math.NaN() },
	} {
		if e := validateVEImageLayers(layer(change), assets); !errors.Is(e, ErrVEInvalid) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
	// Layers are clipped to the video like text; ones starting after it are dropped.
	snap := veSnapshotImages(append(layer(func(l *models.VideoEditorImageLayer) { l.End = 9 }), layer(func(l *models.VideoEditorImageLayer) { l.Start, l.End = 6, 7 })...), assets, 5)
	if len(snap) != 1 || snap[0].End != 5 || snap[0].Key != "key-image" {
		t.Fatalf("snapshot images %+v", snap)
	}
	if veKind(&image) != "image" {
		t.Fatal("image kind lost")
	}
	if !veMIMECompatible("image/png", "image/png") || veMIMECompatible("image/png", "image/jpeg") {
		t.Fatal("image MIME checks")
	}
	c := (&VideoEditorFacade{}).Capabilities()
	if c.ImageLayers || c.MaxImageLayers != 20 {
		t.Fatalf("unavailable media must not advertise image layers: %+v", c)
	}
}
