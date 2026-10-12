package facade_open_api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

// ShareFacade manages public share links and download URLs for finished videos.
type ShareFacade interface {
	// CreateShare returns the job's active link, creating one if needed; created
	// reports whether a new link was made.
	CreateShare(ctx context.Context, userID, jobID string) (share *dto_open_api.ShareDTO, created bool, err error)
	RevokeShare(ctx context.Context, userID, shareID string) error
	GetPublicShare(ctx context.Context, token string) (*dto_open_api.PublicShareDTO, error)
	GetDownloadURL(ctx context.Context, userID, jobID string) (*dto_open_api.DownloadDTO, error)
}

type shareStorage interface {
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
	SignedDownloadURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, time.Time, error)
}

// 32 random bytes in unpadded base64url.
const shareTokenLength = 43

type shareFacade struct {
	shares     repository_open_api.ShareRepository
	jobs       repository_open_api.JobRepository
	storage    shareStorage
	webBaseURL string
}

func NewShareFacade(
	shares repository_open_api.ShareRepository,
	jobs repository_open_api.JobRepository,
	storage shareStorage,
	webBaseURL string,
) ShareFacade {
	return &shareFacade{
		shares:     shares,
		jobs:       jobs,
		storage:    storage,
		webBaseURL: strings.TrimRight(webBaseURL, "/"),
	}
}

func (f *shareFacade) CreateShare(ctx context.Context, userID, jobID string) (*dto_open_api.ShareDTO, bool, error) {
	job, err := f.shareableJob(ctx, userID, jobID)
	if err != nil {
		return nil, false, err
	}

	existing, err := f.shares.FindActiveByJob(ctx, jobID, userID)
	if err == nil {
		return f.mapShare(existing), false, nil
	}
	if !errors.Is(err, repository_open_api.ErrNotFound) {
		return nil, false, fmt.Errorf("find share: %w", err)
	}

	token, err := newShareToken()
	if err != nil {
		return nil, false, fmt.Errorf("generate share token: %w", err)
	}
	share := &models.VideoShare{ID: uuid.New(), JobID: job.ID, UserID: job.UserID, Token: token}
	if err := f.shares.Create(ctx, share); err != nil {
		// A concurrent request may have created the link first; the partial unique
		// index on active links rejects ours, so return theirs.
		if existing, findErr := f.shares.FindActiveByJob(ctx, jobID, userID); findErr == nil {
			return f.mapShare(existing), false, nil
		}
		return nil, false, fmt.Errorf("create share: %w", err)
	}
	return f.mapShare(share), true, nil
}

func (f *shareFacade) RevokeShare(ctx context.Context, userID, shareID string) error {
	if _, err := uuid.Parse(shareID); err != nil {
		return ErrNotFound
	}
	if err := f.shares.Revoke(ctx, shareID, userID); err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("revoke share: %w", err)
	}
	return nil
}

func (f *shareFacade) GetPublicShare(ctx context.Context, token string) (*dto_open_api.PublicShareDTO, error) {
	if !validShareToken(token) {
		return nil, ErrNotFound
	}
	share, err := f.shares.FindActiveByToken(ctx, token)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find share: %w", err)
	}
	job := share.Job
	if job == nil || job.OutputMediaKey == nil || isProviderURL(*job.OutputMediaKey) {
		return nil, ErrNotFound
	}

	mediaURL, expiresAt, err := f.storage.SignedURL(ctx, *job.OutputMediaKey, signedURLTTL)
	if err != nil {
		return nil, fmt.Errorf("sign shared media: %w", err)
	}
	downloadURL, _, err := f.storage.SignedDownloadURL(ctx, *job.OutputMediaKey, signedURLTTL, downloadFilename(job))
	if err != nil {
		return nil, fmt.Errorf("sign shared download: %w", err)
	}
	dto := &dto_open_api.PublicShareDTO{
		MediaURL:    mediaURL,
		DownloadURL: downloadURL,
		MediaType:   strValOrDefault(job.OutputMediaType, "video"),
		AspectRatio: job.AspectRatio,
		Duration:    job.DurationSeconds,
		ExpiresAt:   expiresAt.UTC().Format(time.RFC3339),
		CreatedAt:   share.CreatedAt.UTC().Format(time.RFC3339),
	}
	if job.OutputThumbKey != nil && !isProviderURL(*job.OutputThumbKey) {
		if thumb, _, err := f.storage.SignedURL(ctx, *job.OutputThumbKey, signedURLTTL); err == nil {
			dto.ThumbnailURL = &thumb
		}
	}

	if err := f.shares.IncrementViews(ctx, share.ID.String()); err != nil {
		log.Printf("[share] count view share=%s: %v", share.ID, err)
	}
	return dto, nil
}

func (f *shareFacade) GetDownloadURL(ctx context.Context, userID, jobID string) (*dto_open_api.DownloadDTO, error) {
	job, err := f.shareableJob(ctx, userID, jobID)
	if err != nil {
		return nil, err
	}
	filename := downloadFilename(job)
	u, expiresAt, err := f.storage.SignedDownloadURL(ctx, *job.OutputMediaKey, signedURLTTL, filename)
	if err != nil {
		return nil, fmt.Errorf("sign download: %w", err)
	}
	return &dto_open_api.DownloadDTO{
		URL:       u,
		Filename:  filename,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// shareableJob requires a succeeded job whose output is already in our storage; rows
// still holding a provider URL would leak a BytePlus link that expires within a day.
func (f *shareFacade) shareableJob(ctx context.Context, userID, jobID string) (*models.VideoGenerationJob, error) {
	if _, err := uuid.Parse(jobID); err != nil {
		return nil, ErrNotFound
	}
	job, err := f.jobs.FindByID(ctx, jobID, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find job: %w", err)
	}
	if job.Status != models.JobStatusSucceeded || job.OutputMediaKey == nil || isProviderURL(*job.OutputMediaKey) {
		return nil, ErrNotShareable
	}
	return job, nil
}

func (f *shareFacade) mapShare(s *models.VideoShare) *dto_open_api.ShareDTO {
	return &dto_open_api.ShareDTO{
		ShareID:   s.ID.String(),
		JobID:     s.JobID.String(),
		URL:       f.webBaseURL + "/s/" + s.Token,
		ViewCount: s.ViewCount,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func newShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// validShareToken rejects malformed tokens before they reach the database.
func validShareToken(token string) bool {
	if len(token) != shareTokenLength {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func downloadFilename(job *models.VideoGenerationJob) string {
	ext := strings.TrimPrefix(path.Ext(*job.OutputMediaKey), ".")
	if ext == "" {
		ext = "mp4"
	}
	return fmt.Sprintf("violet-studio-%s.%s", job.ID.String()[:8], ext)
}
