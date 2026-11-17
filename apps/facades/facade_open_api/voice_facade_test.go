package facade_open_api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/seedaudio"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

type voiceRepoFake struct {
	mu       sync.Mutex
	rows     []models.VoiceGeneration
	profiles []models.VoiceProfile
}

func (r *voiceRepoFake) ListProfiles(_ context.Context, user uuid.UUID) ([]models.VoiceProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.VoiceProfile{}
	for _, p := range r.profiles {
		if p.UserID == user {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *voiceRepoFake) CountProfiles(ctx context.Context, user uuid.UUID) (int64, error) {
	rows, _ := r.ListProfiles(ctx, user)
	return int64(len(rows)), nil
}
func (r *voiceRepoFake) FindProfile(_ context.Context, id, user uuid.UUID) (*models.VoiceProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.profiles {
		if p.ID == id && p.UserID == user {
			row := p
			return &row, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (r *voiceRepoFake) CreateProfile(_ context.Context, p *models.VoiceProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.CreatedAt = time.Now()
	r.profiles = append(r.profiles, *p)
	return nil
}
func (r *voiceRepoFake) DeleteProfile(_ context.Context, id, user uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.profiles {
		if p.ID == id && p.UserID == user {
			r.profiles = append(r.profiles[:i], r.profiles[i+1:]...)
			return nil
		}
	}
	return repo.ErrNotFound
}

// memoryTransport serves signed URLs of a storage.MemoryClient so fetch() can read stored clips.
type memoryTransport struct{ store *storage.MemoryClient }

func (m memoryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	key := strings.TrimPrefix(r.URL.Path, "/signed/")
	obj, ok := m.store.Object(key)
	if !ok {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(obj.Data)), Header: http.Header{}, Request: r}, nil
}

func (r *voiceRepoFake) FindByRequest(_ context.Context, user, rid uuid.UUID) (*models.VoiceGeneration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].UserID == user && r.rows[i].RequestID == rid {
			row := r.rows[i]
			return &row, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (r *voiceRepoFake) Create(_ context.Context, row *models.VoiceGeneration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	row.CreatedAt = time.Now()
	r.rows = append(r.rows, *row)
	return nil
}
func (r *voiceRepoFake) List(_ context.Context, user uuid.UUID, limit, offset int) ([]models.VoiceGeneration, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.VoiceGeneration{}
	for i := len(r.rows) - 1; i >= 0; i-- {
		if r.rows[i].UserID == user {
			out = append(out, r.rows[i])
		}
	}
	return out, int64(len(out)), nil
}
func (r *voiceRepoFake) Find(_ context.Context, id, user uuid.UUID) (*models.VoiceGeneration, error) {
	return nil, repo.ErrNotFound
}
func (r *voiceRepoFake) Delete(_ context.Context, id, user uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].ID == id && r.rows[i].UserID == user {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return nil
		}
	}
	return repo.ErrNotFound
}

func voiceServer(t *testing.T, status int, calls *int, body *map[string]any) *seedaudio.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		_ = json.NewDecoder(r.Body).Decode(body)
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"code":40000,"message":"bad"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"audio":"` + base64.StdEncoding.EncodeToString([]byte("mp3-bytes")) + `","duration":2.5,"original_duration":2.5,"subtitle":{"sentences":[{"start_time":0,"end_time":2500,"text":"สวัสดีครับ"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return seedaudio.New("key", srv.URL)
}

func TestVoiceGenerateSavesAndReplays(t *testing.T) {
	calls, body := 0, map[string]any{}
	store, rows := storage.NewMemoryClient(), &voiceRepoFake{}
	f := NewVoiceFacade(rows, store, voiceServer(t, http.StatusOK, &calls, &body))
	user := uuid.NewString()
	in := VoiceInput{RequestID: uuid.NewString(), Text: " สวัสดีครับ ", VoiceID: "krit", Emotion: "ตื่นเต้น", Speed: 1.5, Pitch: 2}

	d, created, e := f.Generate(context.Background(), user, in)
	if e != nil || !created {
		t.Fatalf("generate: created=%v err=%v", created, e)
	}
	if d.VoiceName != "กฤต" || d.Duration != 2.5 || len(d.Sentences) != 1 || d.Sentences[0].End != 2.5 || !strings.Contains(d.AudioURL, "voice/"+user+"/") || d.DownloadURL == "" {
		t.Fatalf("dto: %+v", d)
	}
	if obj, ok := store.Object("voice/" + user + "/" + d.ID + ".mp3"); !ok || string(obj.Data) != "mp3-bytes" || obj.ContentType != "audio/mpeg" {
		t.Fatalf("stored object: %+v %v", obj, ok)
	}
	prompt, _ := body["text_prompt"].(string)
	if !strings.HasSuffix(prompt, "\nสวัสดีครับ") || !strings.Contains(prompt, "mood: excited") || !strings.Contains(prompt, "Do not read") {
		t.Fatalf("prompt: %q", prompt)
	}
	cfg, _ := body["audio_config"].(map[string]any)
	if body["model"] != seedaudio.Model || cfg["speech_rate"] != float64(50) || cfg["pitch_rate"] != float64(2) || cfg["enable_subtitle"] != true || cfg["format"] != "mp3" {
		t.Fatalf("body: %+v", body)
	}

	// Same requestId: no second provider call.
	again, created, e := f.Generate(context.Background(), user, in)
	if e != nil || created || again.ID != d.ID || calls != 1 {
		t.Fatalf("replay: created=%v err=%v calls=%d", created, e, calls)
	}
	changed := in
	changed.Text = "อย่างอื่น"
	if _, _, e := f.Generate(context.Background(), user, changed); !errors.Is(e, ErrConflict) {
		t.Fatalf("changed body must conflict: %v", e)
	}

	list, e := f.List(context.Background(), user, 1, 20)
	if e != nil || list.Total != 1 || list.Items[0].ID != d.ID {
		t.Fatalf("list: %+v %v", list, e)
	}
	if e := f.Delete(context.Background(), uuid.NewString(), d.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("other user delete: %v", e)
	}
	if e := f.Delete(context.Background(), user, d.ID); e != nil {
		t.Fatalf("delete: %v", e)
	}
}

func TestVoiceGenerateValidationAndErrors(t *testing.T) {
	user := uuid.NewString()
	ok := VoiceInput{RequestID: uuid.NewString(), Text: "ทดสอบ", VoiceID: "nara"}
	if _, _, e := NewVoiceFacade(&voiceRepoFake{}, storage.NewMemoryClient(), nil).Generate(context.Background(), user, ok); !errors.Is(e, ErrVoiceUnavailable) {
		t.Fatalf("no client: %v", e)
	}
	calls, body := 0, map[string]any{}
	f := NewVoiceFacade(&voiceRepoFake{}, storage.NewMemoryClient(), voiceServer(t, http.StatusBadRequest, &calls, &body))
	for _, bad := range []VoiceInput{
		{RequestID: "x", Text: "a", VoiceID: "nara"},
		{RequestID: ok.RequestID, Text: "   ", VoiceID: "nara"},
		{RequestID: ok.RequestID, Text: strings.Repeat("ก", VoiceMaxTextRunes+1), VoiceID: "nara"},
		{RequestID: ok.RequestID, Text: "a", VoiceID: "unknown"},
		{RequestID: ok.RequestID, Text: "a", VoiceID: "nara", Emotion: "โกรธ"},
		{RequestID: ok.RequestID, Text: "a", VoiceID: "nara", Speed: 2.5},
		{RequestID: ok.RequestID, Text: "a", VoiceID: "nara", Pitch: 13},
	} {
		if _, _, e := f.Generate(context.Background(), user, bad); !errors.Is(e, ErrValidation) {
			t.Fatalf("accepted %+v: %v", bad, e)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid input reached the provider %d times", calls)
	}
	if _, _, e := f.Generate(context.Background(), user, ok); !errors.Is(e, ErrVoiceRejected) {
		t.Fatalf("provider 400 must be rejected: %v", e)
	}
	g := NewVoiceFacade(&voiceRepoFake{}, storage.NewMemoryClient(), voiceServer(t, http.StatusForbidden, &calls, &body))
	if _, _, e := g.Generate(context.Background(), user, VoiceInput{RequestID: uuid.NewString(), Text: "ทดสอบ", VoiceID: "nara"}); !errors.Is(e, ErrVoiceNotGranted) {
		t.Fatalf("provider 403 must be not-granted: %v", e)
	}
	if got := voiceSpeechRate(0.5); got != -50 {
		t.Fatalf("0.5x = %d", got)
	}
	if got := voiceSpeechRate(2); got != 100 {
		t.Fatalf("2x = %d", got)
	}
	caps := f.Capabilities()
	if !caps.Available || caps.MaxChars != VoiceMaxTextRunes || len(caps.Voices) != 6 || caps.Emotions[0] != "ปกติ" {
		t.Fatalf("caps: %+v", caps)
	}
}

func TestVoiceProfilesDesignAndReference(t *testing.T) {
	calls, body := 0, map[string]any{}
	store, rows := storage.NewMemoryClient(), &voiceRepoFake{}
	f := NewVoiceFacade(rows, store, voiceServer(t, http.StatusOK, &calls, &body))
	f.refsReady = func() bool { return true }
	f.prepare = func(_ context.Context, data []byte) ([]byte, float64, error) {
		if string(data) == "short" {
			return nil, 0, seedaudio.ErrReferenceShort
		}
		return []byte("ref-mp3"), 12.34, nil
	}
	f.client = &http.Client{Transport: memoryTransport{store}}
	ctx, user := context.Background(), uuid.NewString()

	if _, e := f.CreateDesignProfile(ctx, user, "ลุงเหนือ", "สั้น"); !errors.Is(e, ErrValidation) {
		t.Fatalf("short description: %v", e)
	}
	design, e := f.CreateDesignProfile(ctx, user, "  ลุงเหนือ  ", "ผู้ชายอายุ 50 เสียงแหบ พูดช้า สำเนียงเหนือ")
	if e != nil || design.Name != "ลุงเหนือ" || design.Kind != "design" || design.SampleURL != "" {
		t.Fatalf("design: %+v %v", design, e)
	}
	if _, e := f.CreateReferenceProfile(ctx, user, "เสียงฉัน", false, []byte("x")); !errors.Is(e, ErrVoiceConsent) {
		t.Fatalf("no consent: %v", e)
	}
	if _, e := f.CreateReferenceProfile(ctx, user, "เสียงฉัน", true, []byte("short")); !errors.Is(e, ErrVoiceReferenceShort) {
		t.Fatalf("short clip: %v", e)
	}
	ref, e := f.CreateReferenceProfile(ctx, user, "เสียงฉัน", true, []byte("recording"))
	if e != nil || ref.Kind != "reference" || ref.AudioSeconds != 12.3 || !strings.Contains(ref.SampleURL, "voice-profiles/"+user+"/") {
		t.Fatalf("reference: %+v %v", ref, e)
	}

	// Design voice: the user's description is the voice prompt.
	d, _, e := f.Generate(ctx, user, VoiceInput{RequestID: uuid.NewString(), Text: "สวัสดี", VoiceID: design.ID})
	if p, _ := body["text_prompt"].(string); e != nil || d.VoiceName != "ลุงเหนือ" || d.ProfileID == nil || !strings.Contains(p, "สำเนียงเหนือ") || body["references"] != nil {
		t.Fatalf("design generate: %+v %v body=%+v", d, e, body)
	}
	// Reference voice: the stored clip goes as @Audio1 with AIGC metadata.
	d, _, e = f.Generate(ctx, user, VoiceInput{RequestID: uuid.NewString(), Text: "สวัสดี", VoiceID: ref.ID, Emotion: "อบอุ่น"})
	refs, _ := body["references"].([]any)
	first := map[string]any{}
	if len(refs) > 0 {
		first, _ = refs[0].(map[string]any)
	}
	wm, _ := body["watermark"].(map[string]any)
	if p, _ := body["text_prompt"].(string); e != nil || d.VoiceName != "เสียงฉัน" || len(refs) != 1 || first["audio_data"] != base64.StdEncoding.EncodeToString([]byte("ref-mp3")) ||
		!strings.HasPrefix(p, "@Audio1") || !strings.Contains(p, "warm") || wm["aigc_metadata"] == nil {
		t.Fatalf("reference generate: %+v %v body=%+v", d, e, body)
	}
	// Another user's voice id is unknown, not a leak.
	if _, _, e := f.Generate(ctx, uuid.NewString(), VoiceInput{RequestID: uuid.NewString(), Text: "สวัสดี", VoiceID: ref.ID}); !errors.Is(e, ErrValidation) {
		t.Fatalf("other user's profile: %v", e)
	}
	list, e := f.ListProfiles(ctx, user)
	if e != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, e)
	}
	if e := f.DeleteProfile(ctx, user, design.ID); e != nil {
		t.Fatalf("delete: %v", e)
	}
	// History keeps the name after the profile is gone.
	hist, _ := f.List(ctx, user, 1, 20)
	if hist.Items[len(hist.Items)-1].VoiceName != "ลุงเหนือ" {
		t.Fatalf("history name: %+v", hist.Items)
	}
	for n, _ := rows.CountProfiles(ctx, uuid.MustParse(user)); n < maxVoiceProfiles; n++ {
		_, _ = f.CreateDesignProfile(ctx, user, "v", "เสียงทดสอบเพิ่ม")
	}
	if _, e := f.CreateDesignProfile(ctx, user, "เกิน", "เสียงทดสอบเพิ่ม"); !errors.Is(e, ErrVoiceProfileLimit) {
		t.Fatalf("limit: %v", e)
	}
}
