package facade_open_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/videoeditor"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// Large uploads (above what storage accepts per object: 50 MB on the Supabase Free
// plan, videoeditor.MaxUploadBytes) arrive in chunks and are compressed on the API host
// before storing: video to H.264/AAC MP4 at a bitrate that fits, audio to AAC. The
// asset is reserved first, so the browser sees it "processing" and polls; compression
// and storing run in the background under a long lease.

const (
	veDefaultMaxSourceBytes int64 = 500 << 20
	veCompressTimeout             = 25 * time.Minute
	veCompressLease               = 30 * time.Minute
	veCompressFill                = .92 // aim below the limit: container overhead and rate control
	veMinVideoBitrate             = 150_000
	veMinAudioBitrate             = 48_000
)

// ErrVETooLong: even at the lowest usable bitrate the file would not fit storage.
var ErrVETooLong = fmt.Errorf("video editor file too long to compress into storage: %w", ErrVESize)

// MaxSourceBytes is the largest file the browser may send (chunked); EDITOR_MAX_SOURCE_MB.
func (f *VideoEditorFacade) MaxSourceBytes() int64 {
	if f.maxSource > 0 {
		return f.maxSource
	}
	return veDefaultMaxSourceBytes
}

// WithMaxSourceBytes sets the chunked-upload ceiling (0 keeps the default).
func (f *VideoEditorFacade) WithMaxSourceBytes(n int64) *VideoEditorFacade {
	if n > 0 {
		f.maxSource = n
	}
	return f
}

func (f *VideoEditorFacade) sessions() *veUploadSessions {
	f.uploadsOnce.Do(func() { f.uploads = newVEUploadSessions() })
	return f.uploads
}

func veCleanName(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
}

// StartUpload opens a chunked upload after the checks the direct upload makes.
func (f *VideoEditorFacade) StartUpload(ctx context.Context, id, user, uploadID, name, mime string, size int64) (*veUploadSession, error) {
	if !veID(id) || !veID(user) {
		return nil, ErrVEUploadSession
	}
	if _, e := f.repo.Project(ctx, id, user); e != nil {
		return nil, e
	}
	if !f.available {
		return nil, ErrVEMedia
	}
	if e := f.schemaReady(ctx); e != nil {
		return nil, e
	}
	name = veCleanName(name)
	if !veID(uploadID) || size <= 0 || !veTitle(name) {
		return nil, ErrVEInvalid
	}
	if !veUploadMIME[mime] {
		return nil, ErrVEType
	}
	// Images are stored as sent; only video and audio can be compressed to fit.
	if size > f.MaxSourceBytes() || strings.HasPrefix(mime, "image/") && size > videoeditor.MaxUploadBytes {
		return nil, ErrVESize
	}
	return f.sessions().start(user, id, uploadID, name, mime, size)
}

func (f *VideoEditorFacade) PutUploadChunk(id, user, session string, index int, body io.Reader, length int64) (int, error) {
	return f.sessions().put(session, user, id, index, body, length)
}

func (f *VideoEditorFacade) AbortUpload(id, user, session string) {
	f.sessions().abort(session, user, id)
}

// CompleteUpload stores a fully received chunked upload. Files that fit storage take the
// direct path; larger ones are reserved now and compressed in the background.
func (f *VideoEditorFacade) CompleteUpload(ctx context.Context, id, user, sessionID string) (dto.VideoEditorAssetDTO, bool, []int, error) {
	session, missing, e := f.sessions().take(sessionID, user, id)
	if e != nil {
		return dto.VideoEditorAssetDTO{}, false, missing, e
	}
	owned := true
	defer func() {
		if owned {
			_ = os.RemoveAll(session.dir)
		}
	}()
	head := make([]byte, 64)
	file, e := os.Open(session.path)
	if e != nil {
		return dto.VideoEditorAssetDTO{}, false, nil, ErrVEMedia
	}
	n, _ := io.ReadFull(file, head)
	_ = file.Close()
	if !videoeditor.Magic(head[:n]) {
		return dto.VideoEditorAssetDTO{}, false, nil, ErrVEType
	}
	if session.Size <= videoeditor.MaxUploadBytes {
		data, e := os.ReadFile(session.path)
		if e != nil {
			return dto.VideoEditorAssetDTO{}, false, nil, ErrVEMedia
		}
		d, created, e := f.Upload(ctx, id, user, session.ClientUploadID, session.Name, session.MIME, data)
		return d, created, nil, e
	}
	hash, e := veFileHash(session.path)
	if e != nil {
		return dto.VideoEditorAssetDTO{}, false, nil, ErrVEMedia
	}
	lease := time.Now().Add(veCompressLease)
	// size_bytes holds the STORED size and migration 021 caps it at 50 MiB, so the
	// reservation records that cap; the compressed file's real size replaces it on completion.
	a := models.VideoEditorAsset{UserID: uuid.MustParse(user), ProjectID: uuid.MustParse(id), ClientUploadID: uuid.MustParse(session.ClientUploadID), ContentHash: hash, Name: session.Name, Kind: veDeclaredKind(session.MIME), Status: "processing", SizeBytes: videoeditor.MaxUploadBytes, Frames: datatypes.JSON("[]"), AttemptToken: uuid.New(), LeaseUntil: &lease}
	reserved, e := f.repo.ReserveAsset(ctx, &a)
	if e != nil {
		log.Printf("video_editor: large upload reserve failed project=%s err=%v", id, e)
		return dto.VideoEditorAssetDTO{}, false, nil, e
	}
	if !reserved {
		d, e := f.assetDTO(ctx, &a)
		return d, false, nil, e
	}
	owned = false
	go f.compressAndStore(a, session)
	d, e := f.assetDTO(ctx, &a)
	return d, true, nil, e
}

// compressAndStore runs after CompleteUpload returned; it owns session.dir.
func (f *VideoEditorFacade) compressAndStore(a models.VideoEditorAsset, session *veUploadSession) {
	defer os.RemoveAll(session.dir)
	ctx, cancel := context.WithTimeout(context.Background(), veCompressTimeout)
	defer cancel()
	fail := func(err error) {
		code, message := veUploadFailure(err)
		switch {
		case errors.Is(err, ErrVETooLong):
			code, message = "FILE_TOO_LARGE", "คลิปยาวเกินกว่าจะบีบอัดให้ไม่เกิน 50 MB ได้ ลองตัดคลิปให้สั้นลงก่อนอัปโหลด"
		case errors.Is(err, ErrVESize):
			code, message = "FILE_TOO_LARGE", "ไฟล์ใหญ่เกินที่ระบบเก็บได้"
		}
		log.Printf("video_editor: large upload asset=%s failed code=%s err=%v", a.ID, code, err)
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = f.repo.FailAsset(cleanup, a.ID.String(), a.UserID.String(), a.AttemptToken, code, message)
	}
	started := time.Now()
	out, mime, e := veCompressForStorage(ctx, session.path, session.dir, videoeditor.MaxUploadBytes)
	if e != nil {
		fail(e)
		return
	}
	data, e := os.ReadFile(out)
	if e != nil {
		fail(ErrVEMedia)
		return
	}
	sum := sha256.Sum256(data)
	a.ContentHash, a.SizeBytes = hex.EncodeToString(sum[:]), int64(len(data))
	if e = f.storeAsset(ctx, &a, a.UserID.String(), a.ProjectID.String(), out, mime, data); e != nil {
		fail(e)
		return
	}
	log.Printf("video_editor: large upload asset=%s compressed %d -> %d bytes in %s", a.ID, session.Size, len(data), time.Since(started).Round(time.Second))
}

func veFileHash(path string) (string, error) {
	file, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer file.Close()
	h := sha256.New()
	if _, e = io.Copy(h, file); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// veVideoPlan picks bitrates and the longest output side so a video of duration seconds
// fits limit bytes. ok is false when even the floor bitrate would not fit.
func veVideoPlan(limit int64, duration float64, hasAudio bool) (video, audio, side int, ok bool) {
	total := float64(limit) * 8 * veCompressFill / duration
	if hasAudio {
		audio = 128_000
		if total < 600_000 {
			audio = 64_000
		}
	}
	video = int(total) - audio
	switch {
	case video < veMinVideoBitrate:
		return 0, 0, 0, false
	case video > 4_000_000:
		video = 4_000_000 // more does not help a 1280 px picture
	}
	switch {
	case video >= 1_500_000:
		side = 1280
	case video >= 700_000:
		side = 960
	case video >= 350_000:
		side = 854
	default:
		side = 640
	}
	return video, audio, side, true
}

// veCompressForStorage re-encodes path into dir so the result is at most limit bytes.
func veCompressForStorage(ctx context.Context, path, dir string, limit int64) (string, string, error) {
	media, e := videoeditor.Inspect(ctx, path)
	if e != nil {
		return "", "", ErrVEType
	}
	switch media.Kind {
	case "video":
		video, audio, side, ok := veVideoPlan(limit, media.Duration, media.HasAudio)
		if !ok {
			return "", "", ErrVETooLong
		}
		out := filepath.Join(dir, "compressed.mp4")
		for attempt := 0; attempt < 2; attempt++ {
			// Only ever scale down, keep the aspect ratio, even dimensions for yuv420p.
			scale := fmt.Sprintf("scale='if(gte(iw,ih),trunc(min(%d,iw)/2)*2,-2)':'if(gte(iw,ih),-2,trunc(min(%d,ih)/2)*2)'", side, side)
			args := []string{"-nostdin", "-v", "error", "-i", path, "-map", "0:v:0", "-vf", scale, "-c:v", "libx264", "-preset", "veryfast", "-b:v", strconv.Itoa(video), "-maxrate", strconv.Itoa(video * 3 / 2), "-bufsize", strconv.Itoa(video * 2), "-pix_fmt", "yuv420p", "-fpsmax", "30"}
			if audio > 0 {
				args = append(args, "-map", "0:a:0", "-c:a", "aac", "-b:a", strconv.Itoa(audio))
			} else {
				args = append(args, "-an")
			}
			args = append(args, "-movflags", "+faststart", "-y", out)
			if exec.CommandContext(ctx, "ffmpeg", args...).Run() != nil {
				return "", "", ErrVEType
			}
			if info, e := os.Stat(out); e == nil && info.Size() <= limit {
				return out, "video/mp4", nil
			}
			video = video * 7 / 10 // overshoot: one tighter pass
			if video < veMinVideoBitrate {
				break
			}
		}
		return "", "", ErrVETooLong
	case "audio":
		rate := int(float64(limit) * 8 * veCompressFill / media.Duration)
		if rate < veMinAudioBitrate {
			return "", "", ErrVETooLong
		}
		rate = min(rate, 192_000)
		out := filepath.Join(dir, "compressed.m4a")
		if exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", path, "-map", "0:a:0", "-vn", "-c:a", "aac", "-b:a", strconv.Itoa(rate), "-movflags", "+faststart", "-y", out).Run() != nil {
			return "", "", ErrVEType
		}
		if info, e := os.Stat(out); e != nil || info.Size() > limit {
			return "", "", ErrVETooLong
		}
		return out, "audio/mp4", nil
	}
	return "", "", ErrVESize
}
