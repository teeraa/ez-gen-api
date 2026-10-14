package facade_open_api

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

const (
	maxImageBytes = 8 << 20  // 8 MB
	maxAudioBytes = 50 << 20 // 50 MB
)

// UploadFacade handles reference image and audio uploads.
type UploadFacade interface {
	CreateUpload(ctx context.Context, userID string, in CreateUploadInput) (*dto_open_api.UploadDTO, error)
}

type CreateUploadInput struct {
	Filename string
	Data     []byte
	MimeType string // declared by client; validated against magic bytes
}

type uploadFacade struct {
	uploads repository_open_api.UploadRepository
	storage uploadStorage
}

type uploadStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
}

func NewUploadFacade(
	uploads repository_open_api.UploadRepository,
	stor *storage.SupabaseClient,
) UploadFacade {
	return &uploadFacade{uploads: uploads, storage: stor}
}

func (f *uploadFacade) CreateUpload(ctx context.Context, userID string, in CreateUploadInput) (*dto_open_api.UploadDTO, error) {
	mimeType, err := validateUploadFile(in.Data, in.MimeType)
	if err != nil {
		return nil, err
	}

	maxBytes := maxImageBytes
	if strings.HasPrefix(mimeType, "audio/") {
		maxBytes = maxAudioBytes
	}
	if len(in.Data) > maxBytes {
		return nil, ErrFileTooLarge
	}

	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}

	uploadID := uuid.New()
	ext := extForMime(mimeType)
	var key string
	if strings.HasPrefix(mimeType, "audio/") {
		key = storage.KeyForAudioUpload(userID, uploadID.String(), ext)
	} else {
		key = storage.KeyForUpload(userID, uploadID.String(), ext)
	}

	if err := f.storage.Upload(ctx, key, in.Data, mimeType); err != nil {
		return nil, fmt.Errorf("upload file: %w", err)
	}

	record := &models.Upload{
		UserID:     uid,
		StorageKey: key,
		Filename:   in.Filename,
		MimeType:   mimeType,
		SizeBytes:  int64(len(in.Data)),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	record.ID = uploadID
	if err := f.uploads.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("save upload record: %w", err)
	}

	return &dto_open_api.UploadDTO{
		UploadID:  record.ID.String(),
		MimeType:  mimeType,
		SizeBytes: record.SizeBytes,
		ExpiresAt: record.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// validateUploadFile checks magic bytes and returns the canonical MIME type.
func validateUploadFile(data []byte, declared string) (string, error) {
	if len(data) < 12 {
		return "", ErrInvalidFileType
	}

	switch {
	// JPEG: FF D8 FF
	case data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg", nil
	// PNG: 89 50 4E 47 0D 0A 1A 0A
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png", nil
	// WebP: RIFF????WEBP
	case bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", nil
	// MP3: ID3 tag or sync word FF Fx or FF Ex
	case bytes.HasPrefix(data, []byte("ID3")) || (data[0] == 0xFF && (data[1]&0xE0) == 0xE0):
		return "audio/mpeg", nil
	// AAC: ADTS sync 0xFF 0xF0/0xF1
	case data[0] == 0xFF && (data[1] == 0xF0 || data[1] == 0xF1):
		return "audio/aac", nil
	// WAV: RIFF????WAVE
	case bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")):
		return "audio/wav", nil
	// M4A/AAC in MP4 container: ftyp box
	case len(data) >= 8 && bytes.Equal(data[4:8], []byte("ftyp")):
		return "audio/mp4", nil
	default:
		return "", ErrInvalidFileType
	}
}

func extForMime(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "audio/mpeg":
		return "mp3"
	case "audio/aac":
		return "aac"
	case "audio/wav":
		return "wav"
	case "audio/mp4":
		return "m4a"
	default:
		return "bin"
	}
}
