package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

func veAudioFixture(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("requires FFmpeg")
	}
	path := filepath.Join(t.TempDir(), name)
	full := append([]string{"-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3"}, args...)
	if out, e := exec.Command("ffmpeg", append(full, path)...).CombinedOutput(); e != nil {
		t.Fatalf("audio fixture: %v %s", e, out)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func veMeanVolume(t *testing.T, path string, start, duration float64) float64 {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "info", "-ss", fmt.Sprint(start), "-t", fmt.Sprint(duration), "-i", path, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
	if e != nil {
		t.Fatalf("volumedetect: %v %s", e, out)
	}
	m := regexp.MustCompile(`mean_volume: (-?[0-9.]+|-inf) dB`).FindStringSubmatch(string(out))
	if m == nil || m[1] == "-inf" {
		return -200
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

func TestVideoEditorAudioTextPersistenceAndExport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := veTestDB(t)
	store := newVETestStore(t)
	// A missing font keeps text overlays unavailable on every host.
	f := NewVideoEditorFacade(repo.NewVideoEditorRepository(db), store, videoprocessing.NewFFmpegProcessor(filepath.Join(t.TempDir(), "missing.ttf")))
	if f.Capabilities().TextOverlays {
		t.Fatal("text overlays advertised without a font")
	}
	user := veTestUser(t, db)
	p, _, e := f.CreateProject(ctx, user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "เสียงและข้อความ"})
	if e != nil || p.AudioClips == nil || p.TextLayers == nil || len(p.AudioClips)+len(p.TextLayers) != 0 {
		t.Fatal("new project arrays", p, e)
	}
	video, _, e := f.Upload(ctx, p.ID, user, uuid.NewString(), "source.mp4", "video/mp4", veFixture(t))
	if e != nil || video.Kind != "video" {
		t.Fatal("video upload", video, e)
	}
	wav, _, e := f.Upload(ctx, p.ID, user, uuid.NewString(), "เพลง.wav", "audio/wav", veAudioFixture(t, "a.wav", "-c:a", "pcm_s16le"))
	if e != nil || wav.Kind != "audio" || wav.Width != 0 || wav.Height != 0 || !wav.HasAudio || len(wav.Frames) != 0 || wav.PreviewURL == nil || wav.ExpiresAt == nil || wav.Duration < 2.9 {
		t.Fatalf("wav upload %+v %v", wav, e)
	}
	m4a, _, e := f.Upload(ctx, p.ID, user, uuid.NewString(), "voice.m4a", "audio/x-m4a", veAudioFixture(t, "a.m4a", "-c:a", "aac"))
	if e != nil || m4a.Kind != "audio" {
		t.Fatalf("m4a upload %+v %v", m4a, e)
	}
	mp3 := veAudioFixture(t, "a.mp3", "-c:a", "libmp3lame")
	if _, _, e = f.Upload(ctx, p.ID, user, uuid.NewString(), "a.mp3", "audio/wav", mp3); !errors.Is(e, ErrVEType) {
		t.Fatal("mp3 declared as wav accepted", e)
	}
	if a, _, e := f.Upload(ctx, p.ID, user, uuid.NewString(), "a.mp3", "audio/mpeg", mp3); e != nil || a.Kind != "audio" {
		t.Fatal("mp3 upload", a, e)
	}
	segments := []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: video.ID, Start: 0, End: 1, Volume: 0}, {ID: uuid.NewString(), AssetID: video.ID, Start: 1, End: 2, Volume: 0}}
	clip := models.VideoEditorAudioClip{ID: uuid.NewString(), AssetID: wav.ID, At: 1, Start: .5, End: 2.9, Volume: .8}
	text := models.VideoEditorTextLayer{ID: uuid.NewString(), Text: " สวัสดี\r\nครับ ", Start: .5, End: 10, X: .5, Y: .85, FontSize: 64, Color: "#FFCC00", Align: "center"}
	scaleX, scaleY := 1.75, .6
	text.ScaleX, text.ScaleY = &scaleX, &scaleY
	audio, layers := []models.VideoEditorAudioClip{clip}, []models.VideoEditorTextLayer{text}
	save := req.SaveVideoEditorProject{Revision: 1, Title: p.Title, AspectRatio: "16:9", Segments: segments, AudioClips: &audio, TextLayers: &layers}
	saved, e := f.SaveProject(ctx, p.ID, user, save)
	if e != nil || saved.Revision != 2 || len(saved.AudioClips) != 1 || saved.AudioClips[0] != clip || len(saved.TextLayers) != 1 || saved.TextLayers[0].Text != "สวัสดี\nครับ" {
		t.Fatalf("save %+v %v", saved, e)
	}
	keep := req.SaveVideoEditorProject{Revision: 2, Title: "เปลี่ยนชื่อ", AspectRatio: "16:9", Segments: segments}
	kept, e := f.SaveProject(ctx, p.ID, user, keep)
	if e != nil || kept.Revision != 3 || len(kept.AudioClips) != 1 || len(kept.TextLayers) != 1 {
		t.Fatalf("omitted fields must keep stored values: %+v %v", kept, e)
	}
	var body req.SaveVideoEditorProject
	if e = json.Unmarshal([]byte(`{"revision":3,"title":"x","aspectRatio":"16:9","segments":[],"audioClips":null}`), &body); e != nil || body.AudioClips != nil || body.TextLayers != nil {
		t.Fatal("null/omitted must decode as keep", e)
	}
	badSeg := keep
	badSeg.Revision = 3
	badSeg.Segments = []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: wav.ID, Start: 0, End: 1, Volume: 1}}
	if _, e = f.SaveProject(ctx, p.ID, user, badSeg); !errors.Is(e, ErrVEInvalid) {
		t.Fatal("audio asset accepted as segment", e)
	}
	badAudio := []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: video.ID, At: 0, Start: 0, End: 1, Volume: 1}}
	badClip := keep
	badClip.Revision = 3
	badClip.AudioClips = &badAudio
	if _, e = f.SaveProject(ctx, p.ID, user, badClip); !errors.Is(e, ErrVEInvalid) {
		t.Fatal("video asset accepted as audio clip", e)
	}
	got, e := f.Project(ctx, p.ID, user)
	if e != nil || got.Revision != 3 || len(got.AudioClips) != 1 || len(got.TextLayers) != 1 || got.TextLayers[0].Color != "#FFCC00" || got.TextLayers[0].ScaleX == nil || *got.TextLayers[0].ScaleX != scaleX || got.TextLayers[0].ScaleY == nil || *got.TextLayers[0].ScaleY != scaleY {
		t.Fatalf("reload %+v %v", got, e)
	}
	exportReq := req.CreateVideoEditorExport{RequestID: uuid.NewString(), Revision: 3, Resolution: "720p"}
	if _, _, e = f.CreateExport(ctx, p.ID, user, exportReq); !errors.Is(e, ErrVETextUnavailable) {
		t.Fatal("text export without overlays must map to ErrVETextUnavailable", e)
	}
	none := []models.VideoEditorTextLayer{}
	clear := keep
	clear.Revision = 3
	clear.TextLayers = &none
	cleared, e := f.SaveProject(ctx, p.ID, user, clear)
	if e != nil || cleared.Revision != 4 || len(cleared.TextLayers) != 0 || cleared.TextLayers == nil || len(cleared.AudioClips) != 1 {
		t.Fatalf("present empty array must replace: %+v %v", cleared, e)
	}
	exportReq = req.CreateVideoEditorExport{RequestID: uuid.NewString(), Revision: 4, Resolution: "720p"}
	exp, created, e := f.CreateExport(ctx, p.ID, user, exportReq)
	if e != nil || !created || exp.Duration != 2 {
		t.Fatal("audio export", exp, e)
	}
	var row models.VideoEditorExport
	if e = db.Where("id=?", exp.ID).First(&row).Error; e != nil {
		t.Fatal(e)
	}
	var snap models.VideoEditorSnapshot
	if e = json.Unmarshal(row.Snapshot, &snap); e != nil || len(snap.AudioClips) != 1 || snap.AudioClips[0].Key == "" || snap.AudioClips[0].At != 1 || snap.AudioClips[0].End != 1.5 || snap.TextLayers == nil {
		t.Fatalf("snapshot must freeze audio key and cut at the end: %s %v", row.Snapshot, e)
	}
	if done, e := f.ProcessNext(ctx); !done || e != nil {
		t.Fatal("render", done, e)
	}
	exp, e = f.Export(ctx, exp.ID, user)
	if e != nil || exp.Status != "completed" {
		t.Fatal("export state", exp, e)
	}
	if e = db.Where("id=?", exp.ID).First(&row).Error; e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	store.mu.Lock()
	data := store.files[row.OutputKey]
	store.mu.Unlock()
	if e = os.WriteFile(out, data, 0600); e != nil {
		t.Fatal(e)
	}
	before, after := veMeanVolume(t, out, 0, .9), veMeanVolume(t, out, 1.1, .8)
	if before > -80 || after < -30 {
		t.Fatalf("audio clip offset not rendered: before=%.1f after=%.1f", before, after)
	}
}

func TestVideoEditorLegacyRowsDefaultToEmptyArrays(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := veTestDB(t)
	f := NewVideoEditorFacade(repo.NewVideoEditorRepository(db), newVETestStore(t), videoprocessing.NewFFmpegProcessor(""))
	user := veTestUser(t, db)
	id := uuid.NewString()
	if e := db.Exec(`INSERT INTO video_editor_projects(id,user_id,request_id,request_hash,title,aspect_ratio) VALUES(?,?,?,'h','legacy','9:16')`, id, user, uuid.NewString()).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec(`INSERT INTO video_editor_assets(user_id,project_id,client_upload_id,content_hash,name,status,size_bytes,attempt_token) VALUES(?,?,?,'h','old.mp4','failed',10,?)`, user, id, uuid.NewString(), uuid.NewString()).Error; e != nil {
		t.Fatal(e)
	}
	p, e := f.Project(ctx, id, user)
	if e != nil || p.AudioClips == nil || p.TextLayers == nil || len(p.AudioClips)+len(p.TextLayers) != 0 || len(p.Assets) != 1 || p.Assets[0].Kind != "video" {
		t.Fatalf("legacy %+v %v", p, e)
	}
	if e := db.Exec(`UPDATE video_editor_assets SET kind='image' WHERE project_id=?`, id).Error; e == nil {
		t.Fatal("kind check constraint missing")
	}
	if e := db.Exec(`UPDATE video_editor_projects SET text_layers='{}' WHERE id=?`, id).Error; e == nil {
		t.Fatal("text_layers array constraint missing")
	}
}

func TestEditorAIRejectsAudioAndApplyPreservesAudioText(t *testing.T) {
	ctx := context.Background()
	_, editor, f, user, project, video := aiSetup(t, &aiTestASR{}, &aiTestProvider{})
	audio, _, e := editor.Upload(ctx, project, user, uuid.NewString(), "music.wav", "audio/wav", veAudioFixture(t, "m.wav", "-c:a", "pcm_s16le"))
	if e != nil || audio.Kind != "audio" {
		t.Fatal(audio, e)
	}
	for _, ids := range [][]string{{audio.ID}, {video, audio.ID}} {
		in := aiRequest(video)
		in.AssetIDs = ids
		if _, _, e = f.Create(ctx, project, user, in); !errors.Is(e, ErrVEInvalid) {
			t.Fatal("audio asset accepted for AI analysis", ids, e)
		}
	}
	clips := []models.VideoEditorAudioClip{{ID: uuid.NewString(), AssetID: audio.ID, At: .25, Start: 0, End: 2, Volume: .4}}
	layers := []models.VideoEditorTextLayer{{ID: uuid.NewString(), Text: "ซับไตเติล", Start: 0, End: 1, X: .5, Y: .9, FontSize: 40, Color: "#FFFFFF", Align: "center"}}
	saved, e := editor.SaveProject(ctx, project, user, req.SaveVideoEditorProject{Revision: 1, Title: "AI test", AspectRatio: "9:16", Segments: []models.VideoEditorSegment{}, AudioClips: &clips, TextLayers: &layers})
	if e != nil || saved.Revision != 2 {
		t.Fatal(saved, e)
	}
	in := aiRequest(video)
	in.Revision = 2
	j, _, e := f.Create(ctx, project, user, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	if j, e = f.Get(ctx, j.ID, user); e != nil || len(j.Concepts) == 0 {
		t.Fatal(j, e)
	}
	if _, e = f.Select(ctx, j.ID, user, j.Concepts[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	if j, e = f.Get(ctx, j.ID, user); e != nil || j.Status != "review" || j.Draft == nil {
		t.Fatal(j, e)
	}
	apply := req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 2, DraftRevision: j.Draft.Revision}
	for _, s := range j.Draft.Segments {
		apply.ApprovedSegmentIDs = append(apply.ApprovedSegmentIDs, s.ID)
	}
	applied, e := f.Apply(ctx, j.ID, user, apply)
	if e != nil || applied.Revision != 3 || len(applied.Segments) == 0 {
		t.Fatal(applied, e)
	}
	if len(applied.AudioClips) != 1 || applied.AudioClips[0] != clips[0] || len(applied.TextLayers) != 1 || applied.TextLayers[0] != layers[0] {
		t.Fatalf("apply dropped audio/text: %+v %+v", applied.AudioClips, applied.TextLayers)
	}
	reloaded, e := editor.Project(ctx, project, user)
	if e != nil || len(reloaded.AudioClips) != 1 || len(reloaded.TextLayers) != 1 {
		t.Fatal("persisted audio/text after apply", reloaded, e)
	}
	replay, e := f.Apply(ctx, j.ID, user, apply)
	if e != nil || len(replay.AudioClips) != 1 || len(replay.TextLayers) != 1 {
		t.Fatal("apply replay audio/text", replay, e)
	}
	if _, e = f.Apply(ctx, j.ID, user, req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 3, DraftRevision: j.Draft.Revision, ApprovedSegmentIDs: apply.ApprovedSegmentIDs}); !errors.Is(e, repo.ErrAIAlreadyApplied) {
		t.Fatal(e)
	}
}

// Without migration 024 writes fail with ErrVESchema (503 MEDIA_UNAVAILABLE),
// capabilities report unavailable, and reads of pre-024 rows still work.
func TestVideoEditorMissingMigration024IsUnavailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := veTestDBWith(t, "021_video_editor.sql")
	f := NewVideoEditorFacade(repo.NewVideoEditorRepository(db), newVETestStore(t), videoprocessing.NewFFmpegProcessor(""))
	if !f.available {
		t.Skip("requires media tools")
	}
	if c := f.Capabilities(); c.Available || c.UnavailableReason == "" {
		t.Fatalf("capabilities must report missing migration: %+v", c)
	}
	user := veTestUser(t, db)
	if _, _, e := f.CreateProject(ctx, user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "x"}); !errors.Is(e, ErrVESchema) || !errors.Is(e, ErrVEMedia) {
		t.Fatal("create without 024 must be ErrVESchema", e)
	}
	id := uuid.NewString()
	if e := db.Exec(`INSERT INTO video_editor_projects(id,user_id,request_id,request_hash,title,aspect_ratio) VALUES(?,?,?,'h','legacy','9:16')`, id, user, uuid.NewString()).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := f.SaveProject(ctx, id, user, req.SaveVideoEditorProject{Revision: 1, Title: "y", AspectRatio: "9:16", Segments: []models.VideoEditorSegment{}}); !errors.Is(e, ErrVESchema) {
		t.Fatal("save without 024 must be ErrVESchema", e)
	}
	if _, _, e := f.Upload(ctx, id, user, uuid.NewString(), "a.wav", "audio/wav", veAudioFixture(t, "a.wav", "-c:a", "pcm_s16le")); !errors.Is(e, ErrVESchema) {
		t.Fatal("upload without 024 must be ErrVESchema", e)
	}
	if _, _, e := f.CreateExport(ctx, id, user, req.CreateVideoEditorExport{RequestID: uuid.NewString(), Revision: 1, Resolution: "720p"}); !errors.Is(e, ErrVESchema) {
		t.Fatal("export without 024 must be ErrVESchema", e)
	}
	if p, e := f.Project(ctx, id, user); e != nil || len(p.AudioClips)+len(p.TextLayers) != 0 {
		t.Fatal("pre-024 read must still work", p, e)
	}
	// Applying 024 later is picked up without restart (only success is cached).
	migration, e := os.ReadFile("../../../migrations/024_video_editor_audio_text.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(string(migration)).Error; e != nil {
		t.Fatal(e)
	}
	if !f.Capabilities().Available {
		t.Fatal("capabilities must recover after 024")
	}
	if _, _, e := f.CreateProject(ctx, user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "x"}); e != nil {
		t.Fatal("create after 024", e)
	}
}
