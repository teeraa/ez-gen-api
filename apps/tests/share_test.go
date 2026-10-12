package tests

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

type fakeShareRepo struct {
	mu          sync.Mutex
	shares      []*models.VideoShare
	jobs        *fakeJobRepo
	tokenLookup int
}

func (r *fakeShareRepo) Create(_ context.Context, s *models.VideoShare) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s.CreatedAt = time.Now()
	c := *s
	r.shares = append(r.shares, &c)
	return nil
}

func (r *fakeShareRepo) FindActiveByJob(_ context.Context, jobID, userID string) (*models.VideoShare, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.shares {
		if s.JobID.String() == jobID && s.UserID.String() == userID && s.RevokedAt == nil {
			c := *s
			return &c, nil
		}
	}
	return nil, repository_open_api.ErrNotFound
}

func (r *fakeShareRepo) FindActiveByToken(_ context.Context, token string) (*models.VideoShare, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokenLookup++
	for _, s := range r.shares {
		if s.Token == token && s.RevokedAt == nil {
			job := r.jobs.jobs[s.JobID.String()]
			if job == nil || job.Status != models.JobStatusSucceeded {
				break
			}
			c := *s
			jc := *job
			c.Job = &jc
			return &c, nil
		}
	}
	return nil, repository_open_api.ErrNotFound
}

func (r *fakeShareRepo) Revoke(_ context.Context, id, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.shares {
		if s.ID.String() == id && s.UserID.String() == userID && s.RevokedAt == nil {
			now := time.Now()
			s.RevokedAt = &now
			return nil
		}
	}
	return repository_open_api.ErrNotFound
}

func (r *fakeShareRepo) IncrementViews(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.shares {
		if s.ID.String() == id {
			s.ViewCount++
		}
	}
	return nil
}

func shareFixture(t *testing.T, status models.JobStatus, mediaKey string) (facade_open_api.ShareFacade, *fakeShareRepo, *models.VideoGenerationJob) {
	t.Helper()
	job := newOutputJob(status, time.Minute)
	job.AspectRatio, job.DurationSeconds = "9:16", 5
	if mediaKey != "" {
		job.OutputMediaKey = &mediaKey
		thumb := "thumbs/u/j.png"
		job.OutputThumbKey = &thumb
	}
	jobs := newFakeJobRepo(job)
	shares := &fakeShareRepo{jobs: jobs}
	f := facade_open_api.NewShareFacade(shares, jobs, storage.NewMemoryClient(), "https://violetstudio.app/")
	return f, shares, job
}

func tokenFromURL(t *testing.T, u string) string {
	t.Helper()
	const prefix = "https://violetstudio.app/s/"
	if !strings.HasPrefix(u, prefix) {
		t.Fatalf("share url %q does not start with %q", u, prefix)
	}
	return strings.TrimPrefix(u, prefix)
}

func TestCreateShare_IsIdempotentPerJob(t *testing.T) {
	t.Parallel()
	f, shares, job := shareFixture(t, models.JobStatusSucceeded, "outputs/u/j.mp4")

	first, created, err := f.CreateShare(t.Context(), job.UserID.String(), job.ID.String())
	if err != nil || !created {
		t.Fatalf("first create = %v, %v", created, err)
	}
	token := tokenFromURL(t, first.URL)
	if len(token) != 43 {
		t.Errorf("token length = %d, want 43 (256-bit base64url)", len(token))
	}
	second, created, err := f.CreateShare(t.Context(), job.UserID.String(), job.ID.String())
	if err != nil || created || second.URL != first.URL {
		t.Errorf("second create should return the same link: created=%v url=%q err=%v", created, second.URL, err)
	}
	if len(shares.shares) != 1 {
		t.Errorf("expected one share row, got %d", len(shares.shares))
	}
}

func TestCreateShare_Rejections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   models.JobStatus
		mediaKey string
		otherUsr bool
		want     error
	}{
		{"still processing", models.JobStatusProcessing, "", false, facade_open_api.ErrNotShareable},
		{"output not yet in storage", models.JobStatusSucceeded, "https://ark.tos-ap-southeast-1.volces.com/x.mp4", false, facade_open_api.ErrNotShareable},
		{"another user's job", models.JobStatusSucceeded, "outputs/u/j.mp4", true, facade_open_api.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, shares, job := shareFixture(t, tc.status, tc.mediaKey)
			userID := job.UserID.String()
			if tc.otherUsr {
				userID = uuid.NewString()
			}
			_, _, err := f.CreateShare(t.Context(), userID, job.ID.String())
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if len(shares.shares) != 0 {
				t.Error("no share may be created")
			}
		})
	}
}

func TestGetPublicShare_ServesSignedURLsAndCountsViews(t *testing.T) {
	t.Parallel()
	f, shares, job := shareFixture(t, models.JobStatusSucceeded, "outputs/u/j.mp4")
	share, _, _ := f.CreateShare(t.Context(), job.UserID.String(), job.ID.String())

	dto, err := f.GetPublicShare(t.Context(), tokenFromURL(t, share.URL))
	if err != nil {
		t.Fatalf("public share: %v", err)
	}
	if dto.MediaURL != "https://storage.test/signed/outputs/u/j.mp4" {
		t.Errorf("mediaUrl = %q", dto.MediaURL)
	}
	if !strings.Contains(dto.DownloadURL, "download=violet-studio-"+job.ID.String()[:8]+".mp4") {
		t.Errorf("downloadUrl = %q", dto.DownloadURL)
	}
	if dto.ThumbnailURL == nil || dto.AspectRatio != "9:16" || dto.Duration != 5 {
		t.Errorf("unexpected dto: %+v", dto)
	}
	if shares.shares[0].ViewCount != 1 {
		t.Errorf("view count = %d, want 1", shares.shares[0].ViewCount)
	}
}

func TestGetPublicShare_NotFoundCases(t *testing.T) {
	t.Parallel()
	f, shares, job := shareFixture(t, models.JobStatusSucceeded, "outputs/u/j.mp4")
	share, _, _ := f.CreateShare(t.Context(), job.UserID.String(), job.ID.String())
	token := tokenFromURL(t, share.URL)

	for _, bad := range []string{"", "short", strings.Repeat("a", 42) + "!", "../../etc/passwd"} {
		if _, err := f.GetPublicShare(t.Context(), bad); !errors.Is(err, facade_open_api.ErrNotFound) {
			t.Errorf("token %q: err = %v, want ErrNotFound", bad, err)
		}
	}
	if shares.tokenLookup != 0 {
		t.Errorf("malformed tokens must not reach the database, got %d lookups", shares.tokenLookup)
	}

	if err := f.RevokeShare(t.Context(), uuid.NewString(), share.ShareID); !errors.Is(err, facade_open_api.ErrNotFound) {
		t.Errorf("another user revoking: err = %v, want ErrNotFound", err)
	}
	if err := f.RevokeShare(t.Context(), job.UserID.String(), share.ShareID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.GetPublicShare(t.Context(), token); !errors.Is(err, facade_open_api.ErrNotFound) {
		t.Errorf("revoked share: err = %v, want ErrNotFound", err)
	}

	again, created, err := f.CreateShare(t.Context(), job.UserID.String(), job.ID.String())
	if err != nil || !created || again.URL == share.URL {
		t.Errorf("sharing again after revoke should mint a new link: created=%v err=%v", created, err)
	}
}

func TestGetDownloadURL(t *testing.T) {
	t.Parallel()
	f, _, job := shareFixture(t, models.JobStatusSucceeded, "outputs/u/j.mp4")
	dto, err := f.GetDownloadURL(t.Context(), job.UserID.String(), job.ID.String())
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if dto.Filename != "violet-studio-"+job.ID.String()[:8]+".mp4" || !strings.Contains(dto.URL, "download="+dto.Filename) || dto.ExpiresAt == "" {
		t.Errorf("unexpected download dto: %+v", dto)
	}
	if _, err := f.GetDownloadURL(t.Context(), uuid.NewString(), job.ID.String()); !errors.Is(err, facade_open_api.ErrNotFound) {
		t.Errorf("other user: err = %v, want ErrNotFound", err)
	}
}
