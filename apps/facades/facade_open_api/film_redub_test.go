package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

func filmRedubFixture() (*filmShot, map[string]*models.FilmElement, *models.FilmElement) {
	hero := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "ขุนอินทร์",
		Voice: models.EncodeCharacterVoice(&models.CharacterVoice{VoiceID: "krit", VoiceName: "กฤต", Kind: "preset", Emotion: "ปกติ", Speed: 1})}
	byID := filmElementMap([]models.FilmElement{hero})
	el := byID[hero.ID.String()]
	sh := &filmShot{ID: "s1", Seconds: 6, Transition: "cut", CharacterIDs: []string{el.ID.String()}, Action: "he speaks",
		Dialogue: []filmLine{{SpeakerID: el.ID.String(), Line: "ข้าศึกมาแล้ว"}}}
	return sh, byID, el
}

func TestFilmVoiceStatus(t *testing.T) {
	sh, byID, hero := filmRedubFixture()
	base := filmShotHash(sh, byID, "cinematic")
	lines := filmVoiceLines(sh, byID)
	current := &filmVideo{Status: "succeeded", Hash: base, ClipKey: "c", RawKey: "r", VoiceHash: filmLinesHash(lines), Silent: true}
	if st := filmVoiceStatus(current, sh, byID, "cinematic"); st != "current" || !filmVideoFresh(current, sh, byID, "cinematic") {
		t.Fatalf("fresh clip with the current voices: %s", st)
	}

	hero.Voice = models.EncodeCharacterVoice(&models.CharacterVoice{VoiceID: "tawan", VoiceName: "ตะวัน", Kind: "preset", Emotion: "ปกติ", Speed: 1})
	if filmShotHash(sh, byID, "cinematic") != base {
		t.Fatal("a voice change must not make the video stale")
	}
	if st := filmVoiceStatus(current, sh, byID, "cinematic"); st != "outdated" {
		t.Fatalf("new voice → outdated, got %s", st)
	}
	want := filmLinesHash(filmVoiceLines(sh, byID))
	current.Redub = &filmRedub{Status: "processing", VoiceHash: want}
	if st := filmVoiceStatus(current, sh, byID, "cinematic"); st != "updating" {
		t.Fatalf("running re-dub → updating, got %s", st)
	}
	current.Redub = &filmRedub{Status: "failed", VoiceHash: want}
	if st := filmVoiceStatus(current, sh, byID, "cinematic"); st != "failed" {
		t.Fatalf("failed re-dub for the wanted voices → failed, got %s", st)
	}

	// Made while nobody had a voice: the provider spoke the lines.
	unvoiced := &filmVideo{Status: "succeeded", Hash: base, ClipKey: "c", RawKey: "c", VoiceHash: filmNoVoices}
	if st := filmVoiceStatus(unvoiced, sh, byID, "cinematic"); st != "legacy" {
		t.Fatalf("provider-voiced clip cannot be re-dubbed: %s", st)
	}
	// Made before voices were bound: provider voices, no raw clip.
	legacy := &filmVideo{Status: "succeeded", Hash: base, ClipKey: "c"}
	if st := filmVoiceStatus(legacy, sh, byID, "cinematic"); st != "legacy" || !filmVideoFresh(legacy, sh, byID, "cinematic") {
		t.Fatalf("legacy clip keeps its video but cannot change voice: %s", st)
	}
	// Made while voices were part of the shot hash: dubbed over the provider's own speech.
	combined := &filmVideo{Status: "succeeded", Hash: filmVoiceHash(base, filmVoiceLines(sh, byID)), ClipKey: "c"}
	if st := filmVoiceStatus(combined, sh, byID, "cinematic"); st != "overlap" || !filmVideoFresh(combined, sh, byID, "cinematic") {
		t.Fatalf("combined-hash clip plays two voices at once: %s", st)
	}
	// Dubbed over provider audio after raw clips were kept: also two voices.
	loud := &filmVideo{Status: "succeeded", Hash: base, ClipKey: "c", RawKey: "r", VoiceHash: filmLinesHash(filmVoiceLines(sh, byID))}
	if st := filmVoiceStatus(loud, sh, byID, "cinematic"); st != "overlap" {
		t.Fatalf("dubbed clip with provider audio overlaps: %s", st)
	}
	// A draft made with provider audio cannot become a dubbed full clip.
	if filmDraftDubbable(filmDraft{}, sh, byID) || !filmDraftDubbable(filmDraft{Silent: true}, sh, byID) {
		t.Fatal("only silent drafts are finalized for dubbed shots")
	}
	sh.Action = "he shouts"
	if filmVideoFresh(current, sh, byID, "cinematic") || filmVideoFresh(combined, sh, byID, "cinematic") {
		t.Fatal("changing the action makes the video stale")
	}
}

// filmFakeStore keeps one episode in memory; methods a test does not need panic via the nil interface.
type filmFakeStore struct {
	filmStore
	project  models.DirectorProject
	series   models.DirectorSeries
	episode  models.FilmEpisode
	elements []models.FilmElement
	jobs     []models.FilmJob
	finished map[uuid.UUID]string
	refunded map[uuid.UUID]bool
}

func (s *filmFakeStore) Episode(context.Context, string, string) (*models.DirectorProject, *models.DirectorSeries, *models.FilmEpisode, error) {
	ep := s.episode
	return &s.project, &s.series, &ep, nil
}
func (s *filmFakeStore) Elements(context.Context, string, string) ([]models.FilmElement, error) {
	return s.elements, nil
}
func (s *filmFakeStore) Balance(context.Context, string) (int64, error) { return 100, nil }
func (s *filmFakeStore) ChangeEpisode(_ context.Context, _, _, _, _ string, fn func(*models.FilmEpisode, *models.DirectorProject, *models.DirectorSeries, []models.FilmElement) ([]models.FilmJob, error)) (*models.FilmEpisode, error) {
	jobs, e := fn(&s.episode, &s.project, &s.series, s.elements)
	if e != nil {
		return nil, e
	}
	s.jobs = append(s.jobs, jobs...)
	return &s.episode, nil
}
func (s *filmFakeStore) ProgressEpisode(_ context.Context, _ *models.FilmJob, fn func(*models.FilmEpisode) error) error {
	return fn(&s.episode)
}
func (s *filmFakeStore) CompleteEpisode(_ context.Context, j *models.FilmJob, status string, refund bool, fn func(*models.FilmEpisode) error) error {
	s.finished[j.ID], s.refunded[j.ID] = status, refund
	return fn(&s.episode)
}

type filmFakeMedia struct{ uploads map[string][]byte }

func (m *filmFakeMedia) Upload(_ context.Context, key string, data []byte, _ string) error {
	m.uploads[key] = data
	return nil
}
func (m *filmFakeMedia) SignedURL(_ context.Context, key string, _ time.Duration) (string, time.Time, error) {
	return "https://media/" + key, time.Now().Add(time.Hour), nil
}

type filmFakeVoices struct{ spoken []string }

func (v *filmFakeVoices) ResolveCharacterVoice(context.Context, string, req.CharacterVoiceRequest) (*models.CharacterVoice, error) {
	return nil, nil
}
func (v *filmFakeVoices) SpeakLine(_ context.Context, _ string, cv models.CharacterVoice, text, _ string) ([]byte, error) {
	v.spoken = append(v.spoken, cv.VoiceID+":"+text)
	return []byte("mp3"), nil
}

type filmFakeMixer struct {
	videoprocessing.VideoProcessor
	merges int
}

func (m *filmFakeMixer) Merge(context.Context, videoprocessing.MergeRequest) ([]byte, error) {
	m.merges++
	return []byte("dubbed"), nil
}

func TestFilmRequestRedubQueuesOutdatedClipsOnly(t *testing.T) {
	sh, byID, hero := filmRedubFixture()
	stale := *sh
	stale.ID, stale.Action = "s2", "other"
	base := filmShotHash(sh, byID, "cinematic")
	voiced := filmLinesHash(filmVoiceLines(sh, byID))
	sh.Video = &filmVideo{Status: "succeeded", Hash: base, ClipKey: "clip1", RawKey: "raw1", VoiceHash: voiced, Silent: true}
	stale.Video = &filmVideo{Status: "succeeded", Hash: "old", ClipKey: "clip2", RawKey: "raw2", VoiceHash: voiced, Silent: true}
	doc := filmDocument{Shots: []filmShot{*sh, stale}}
	raw, _ := json.Marshal(doc)

	store := &filmFakeStore{elements: []models.FilmElement{*hero}, finished: map[uuid.UUID]string{}, refunded: map[uuid.UUID]bool{}}
	store.project.ID, store.series.ID, store.series.Style, store.series.AspectRatio, store.series.Resolution = uuid.New(), uuid.New(), "cinematic", "16:9", "720p"
	store.episode = models.FilmEpisode{ProjectID: store.project.ID, Document: raw}
	media := &filmFakeMedia{uploads: map[string][]byte{}}
	f := NewFilmFacade(store, media, nil, nil, nil, FilmOptions{Processor: &filmFakeMixer{}, VideoFetch: func(context.Context, string) ([]byte, string, error) { return []byte("raw"), "video/mp4", nil }})
	f.voices = &filmFakeVoices{}
	project := store.project.ID.String()

	var de *DirectorError
	if _, e := f.RequestRedub(context.Background(), project, uuid.NewString(), req.FilmRedubRequest{IdempotencyKey: uuid.NewString()}); !errors.As(e, &de) || de.Code != "NOTHING_TO_REDUB" {
		t.Fatalf("all voices current → NOTHING_TO_REDUB, got %v", e)
	}

	store.elements[0].Voice = models.EncodeCharacterVoice(&models.CharacterVoice{VoiceID: "tawan", VoiceName: "ตะวัน", Kind: "preset", Emotion: "ปกติ", Speed: 1})
	out, e := f.RequestRedub(context.Background(), project, uuid.NewString(), req.FilmRedubRequest{IdempotencyKey: uuid.NewString()})
	if e != nil {
		t.Fatal(e)
	}
	if len(store.jobs) != 1 || store.jobs[0].Kind != "shot_redub" || store.jobs[0].Cost != 1 {
		t.Fatalf("one re-dub job for the fresh clip only: %+v", store.jobs)
	}
	if out.Shots[0].Video.Voice != "updating" || out.Shots[1].Video.Voice != "outdated" || !out.Shots[1].Video.Stale {
		t.Fatalf("states after request: %+v / %+v", out.Shots[0].Video, out.Shots[1].Video)
	}
	if out.RedubShots != 0 {
		t.Fatalf("stale clips are not offered a re-dub: %d", out.RedubShots)
	}
	if !out.RedubAvailable {
		t.Fatal("re-dub is available with voices, processor, fetch and storage")
	}
}
