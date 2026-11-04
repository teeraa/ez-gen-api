package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

func TestAIComposeGroundedCaptionsAndReflow(t *testing.T) {
	job, asset := uuid.NewString(), uuid.NewString()
	first := models.EditorAIDraftSegment{ID: uuid.NewString(), AssetID: asset, Start: 10, End: 12, Title: "ประเด็นแรก", Volume: 1}
	second := models.EditorAIDraftSegment{ID: uuid.NewString(), AssetID: asset, Start: 20, End: 23, Title: "ประเด็นถัดไป", Volume: 1}
	transcripts := []models.EditorAITranscript{{AssetID: asset, HasSpeech: true, Segments: []models.EditorAISpeech{
		{Start: 8, End: 14, Text: "coarse context must not be assigned to partial cut"},
		{Start: 9, End: 13, Text: "exact words", Words: []models.EditorAIWord{{Start: 9, End: 9.5, Text: "outside"}, {Start: 10.2, End: 10.6, Text: "inside"}, {Start: 11, End: 11.5, Text: "aligned"}, {Start: 11.8, End: 12.5, Text: "crossing"}}},
		{Start: 20.5, End: 22, Text: "whole segment"},
	}}}
	d := models.EditorAIDraft{Duration: 5, Segments: []models.EditorAIDraftSegment{first, second}}
	input := models.EditorAIInput{AutoCompose: true}
	composeAIDraft(&d, input, job, transcripts)
	found := false
	for _, caption := range d.TextLayers {
		if strings.Contains(caption.Text, "coarse") || strings.Contains(caption.Text, "outside") || strings.Contains(caption.Text, "crossing") {
			t.Fatalf("ungrounded caption %+v", caption)
		}
		if caption.Text == "inside aligned" {
			found = true
			if caption.Start < .19999 || caption.End != 1.5 {
				t.Fatalf("bad source mapping %+v", caption)
			}
		}
	}
	if !found {
		t.Fatal("exact words missing")
	}
	d.Segments = []models.EditorAIDraftSegment{second, first}
	composeAIDraft(&d, input, job, transcripts)
	for _, caption := range d.TextLayers {
		if caption.Text == "inside aligned" && caption.Start < 3.19999 {
			t.Fatalf("stale reordered caption %+v", caption)
		}
	}
	d.Segments[1].Start = 11.7
	d.Duration = 3.3
	composeAIDraft(&d, input, job, transcripts)
	for _, caption := range d.TextLayers {
		if strings.Contains(caption.Text, "inside") || strings.Contains(caption.Text, "aligned") {
			t.Fatal("trim retained outside words")
		}
	}
}

func TestAIComposeMusicAndLimits(t *testing.T) {
	job, asset, audio := uuid.NewString(), uuid.NewString(), uuid.NewString()
	d := models.EditorAIDraft{Duration: 4, Segments: []models.EditorAIDraftSegment{{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 4, Title: "สรุปไฮไลต์", Volume: 1}}}
	input := models.EditorAIInput{AutoCompose: true, AudioSources: []models.EditorAISource{{ID: audio, Duration: 2}}}
	composeAIDraft(&d, input, job, nil)
	if len(d.AudioClips) != 1 || d.AudioClips[0].AssetID != audio || d.AudioClips[0].End != 2 || d.AudioClips[0].Volume != .12 {
		t.Fatalf("audio %+v", d.AudioClips)
	}
	if err := validateAIComposition(d, input, job); err != nil {
		t.Fatal(err)
	}
	d.AudioClips[0].AssetID = uuid.NewString()
	if err := validateAIComposition(d, input, job); err == nil {
		t.Fatal("unowned audio accepted")
	}
	input.ManualAudio = true
	input.ManualTextCount = 50
	composeAIDraft(&d, input, job, nil)
	if len(d.TextLayers) != 0 || len(d.AudioClips) != 0 {
		t.Fatal("did not reserve manual tracks")
	}
	input.ManualAudio = false
	input.ManualTextCount = 0
	input.AudioSources = nil
	composeAIDraft(&d, input, job, nil)
	if len(d.AudioClips) != 0 || !strings.Contains(strings.Join(d.CompositionNotes, " "), "ไม่มีไฟล์เสียง") {
		t.Fatal("music fallback fabricated audio")
	}
	legacy := aiDTO(&models.VideoEditorAIJob{Input: aiJSON(models.EditorAIInput{}), Draft: aiJSON(models.EditorAIDraft{Revision: 1, Segments: []models.EditorAIDraftSegment{}})})
	if legacy.AutoCompose || legacy.Draft == nil {
		t.Fatal("legacy draft changed")
	}
}

// This lifecycle uses real PostgreSQL/handlers' facades, synthetic media and
// in-process fake ASR/Gemini; no cloud provider or persistent schema is touched.
func TestAIAutoComposeLifecycleAndAtomicTracks(t *testing.T) {
	ctx := context.Background()
	db := veTestDBWith(t, "021_video_editor.sql", "024_video_editor_audio_text.sql", "022_video_editor_ai.sql")
	store := newVETestStore(t)
	editor := NewVideoEditorFacade(repo.NewVideoEditorRepository(db), store, videoprocessing.NewFFmpegProcessor(""))
	user := veTestUser(t, db)
	project, _, err := editor.CreateProject(ctx, user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "auto highlights"})
	if err != nil {
		t.Fatal(err)
	}
	video, _, err := editor.Upload(ctx, project.ID, user, uuid.NewString(), "source.mp4", "video/mp4", veFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	audio, _, err := editor.Upload(ctx, project.ID, user, uuid.NewString(), "music.wav", "audio/wav", veAudioFixture(t, "music.wav", "-c:a", "pcm_s16le"))
	if err != nil {
		t.Fatal(err)
	}
	oldJob := uuid.NewString()
	manual := veTestText()
	manual.Text = "manual"
	manual.End = 1
	oldAI := veTestText()
	oldAI.OriginJobID = oldJob
	oldAI.Text = "old AI"
	oldAI.End = 1
	text := []models.VideoEditorTextLayer{manual, oldAI}
	oldAudio := []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: audio.ID, At: 0, Start: 0, End: 1, Volume: .2, OriginJobID: oldJob}}
	if _, err = editor.SaveProject(ctx, project.ID, user, req.SaveVideoEditorProject{Revision: 1, Title: project.Title, AspectRatio: "16:9", Segments: []models.VideoEditorSegment{}, TextLayers: &text, AudioClips: &oldAudio}); err != nil {
		t.Fatal(err)
	}
	f := NewVideoEditorAIFacade(repo.NewVideoEditorAIRepository(db), editor, &aiTestProvider{}, &aiTestASR{}, EditorAIOptions{Enabled: true, Fixture: true})
	input := aiRequest(video.ID)
	input.Revision = 2
	input.AutoCompose = true
	j, _, err := f.Create(ctx, project.ID, user, input)
	if err != nil {
		t.Fatal(err)
	}
	if !j.AutoCompose || !f.Capabilities().AutoCompose {
		t.Fatal("auto compose unavailable")
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	j, err = f.Get(ctx, j.ID, user)
	if err != nil || j.Status != "queued" || j.Phase != "planning" || j.SelectedConceptID == nil {
		t.Fatalf("auto selection %+v %v", j, err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	j, err = f.Get(ctx, j.ID, user)
	if err != nil || j.Status != "review" || j.Draft == nil || len(j.Draft.TextLayers) == 0 || len(j.Draft.AudioClips) != 1 {
		t.Fatalf("composition %+v %v", j, err)
	}
	changed := append([]models.EditorAIDraftSegment(nil), j.Draft.Segments...)
	changed[0].Start += .1
	j, err = f.UpdateDraft(ctx, j.ID, user, req.UpdateVideoEditorAIDraft{DraftRevision: j.Draft.Revision, Segments: changed})
	if err != nil {
		t.Fatal(err)
	}
	if j.Draft == nil || j.Draft.AudioClips[0].End > j.Draft.Duration+.001 {
		t.Fatal("draft composition not reflowed")
	}
	apply := req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 2, DraftRevision: j.Draft.Revision}
	for _, segment := range j.Draft.Segments {
		apply.ApprovedSegmentIDs = append(apply.ApprovedSegmentIDs, segment.ID)
	}
	stale := apply
	stale.Revision = 1
	if _, err = f.Apply(ctx, j.ID, user, stale); !errors.Is(err, repo.ErrVERevision) {
		t.Fatalf("revision guard %v", err)
	}
	result, err := f.Apply(ctx, j.ID, user, apply)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TextLayers) < 2 || result.TextLayers[0].ID != manual.ID || len(result.AudioClips) != 1 || result.AudioClips[0].OriginJobID != j.ID {
		t.Fatalf("track merge %+v %+v", result.TextLayers, result.AudioClips)
	}
	for _, layer := range result.TextLayers {
		if layer.ID == oldAI.ID {
			t.Fatal("old AI track retained")
		}
	}
	replay, err := f.Apply(ctx, j.ID, user, apply)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(result)
	two, _ := json.Marshal(replay)
	if result.Revision != replay.Revision || len(result.TextLayers) != len(replay.TextLayers) || len(result.AudioClips) != len(replay.AudioClips) {
		t.Fatalf("replay differs %s %s", one, two)
	}
	reloaded, err := editor.Project(ctx, project.ID, user)
	if err != nil || len(reloaded.TextLayers) != len(result.TextLayers) {
		t.Fatal("persisted composition missing", err)
	}

	manualAudio := []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: audio.ID, At: 0, Start: 0, End: 1, Volume: .5}}
	saved, err := editor.SaveProject(ctx, project.ID, user, req.SaveVideoEditorProject{Revision: result.Revision, Title: project.Title, AspectRatio: "16:9", Segments: result.Segments, AudioClips: &manualAudio})
	if err != nil {
		t.Fatal(err)
	}
	input.RequestID = uuid.NewString()
	input.Revision = saved.Revision
	next, _, err := f.Create(ctx, project.ID, user, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = f.ProcessNext(ctx); err != nil {
			t.Fatal(err)
		}
	}
	next, err = f.Get(ctx, next.ID, user)
	if err != nil || next.Draft == nil || len(next.Draft.AudioClips) != 0 {
		t.Fatal("manual audio not respected", err)
	}
	apply = req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: saved.Revision, DraftRevision: next.Draft.Revision}
	for _, segment := range next.Draft.Segments {
		apply.ApprovedSegmentIDs = append(apply.ApprovedSegmentIDs, segment.ID)
	}
	latest, err := f.Apply(ctx, next.ID, user, apply)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.AudioClips) != 1 || latest.AudioClips[0] != manualAudio[0] {
		t.Fatal("manual audio overwritten")
	}
	for _, layer := range latest.TextLayers {
		if layer.OriginJobID == j.ID {
			t.Fatal("prior AI-owned text retained")
		}
	}
}

func TestAIComposeApplyRejectsOverflowAndInvalidOrigin(t *testing.T) {
	manual := make([]models.VideoEditorTextLayer, 50)
	for i := range manual {
		manual[i] = veTestText()
	}
	project := models.VideoEditorProject{TextLayers: aiJSON(manual), AudioClips: aiJSON([]models.VideoEditorAudioClip{})}
	generated := veTestText()
	generated.OriginJobID = uuid.NewString()
	if err := validateAIApplyTracks(&project, models.EditorAIDraft{TextLayers: []models.VideoEditorTextLayer{generated}}, nil); err == nil {
		t.Fatal("silent manual track loss on overflow")
	}
	bad := veTestText()
	bad.OriginJobID = "not-a-job"
	if _, err := normalizeVETextLayers([]models.VideoEditorTextLayer{bad}); err == nil {
		t.Fatal("invalid origin accepted")
	}
	asset := veTestAsset("audio", 5)
	if err := validateVEAudioClips([]models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: asset.ID.String(), Start: 0, End: 1, Volume: 1, OriginJobID: "invalid"}}, []models.VideoEditorAsset{asset}); err == nil {
		t.Fatal("invalid audio origin accepted")
	}
}

func TestAIComposeZeroDurationTokensDoNotCorruptThai(t *testing.T) {
	transcript := models.EditorAITranscript{HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 0, End: 2, Text: "กาแฟ", Words: []models.EditorAIWord{{Start: 0, End: 0, Text: "ก"}, {Start: 0, End: 2, Text: "าแฟ"}}}}}
	partial := aiCaptionWindows(transcript, models.EditorAIDraftSegment{Start: 1, End: 2})
	if len(partial) != 0 {
		t.Fatal("uncertain zero-duration context assigned to partial cut")
	}
	whole := aiCaptionWindows(transcript, models.EditorAIDraftSegment{Start: 0, End: 2})
	if len(whole) != 1 || whole[0].Text != "กาแฟ" {
		t.Fatal("Thai token lost", whole)
	}
}
