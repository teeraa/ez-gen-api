package facade_open_api

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

// Chunked ("batch") uploads let the browser send files larger than one request body
// (the API and the web proxy cap bodies near 55 MB). The browser sends fixed-size chunks
// in any order; the API writes each at its offset in one file under the OS temp dir and,
// once every chunk has arrived, hands the file to UploadLarge. Sessions live in process
// memory: one API instance, lost on restart (the browser then starts again).

const (
	veChunkBytes      = 5 << 20 // 5 MiB per chunk: under every body limit, and sent within the 60 s read timeout at ~1 Mbit/s
	veSessionTTL      = 2 * time.Hour
	veSessionsPerUser = 2
)

var (
	ErrVEUploadSession = errors.New("video editor upload session not found or expired")
	ErrVEUploadChunk   = fmt.Errorf("video editor upload chunk invalid: %w", ErrVEInvalid)
	ErrVEUploadMissing = fmt.Errorf("video editor upload incomplete: %w", ErrVEInvalid)
)

type veUploadSession struct {
	ID, User, Project, ClientUploadID, Name, MIME string
	Size                                          int64
	Chunks                                        int
	dir, path                                     string
	received                                      []bool
	expires                                       time.Time
	completing                                    bool
}

type veUploadSessions struct {
	mu       sync.Mutex
	sessions map[string]*veUploadSession
	now      func() time.Time
	root     string
}

func newVEUploadSessions() *veUploadSessions {
	return &veUploadSessions{sessions: map[string]*veUploadSession{}, now: time.Now, root: filepath.Join(os.TempDir(), "ez-gen-uploads")}
}

// sweep removes expired sessions and their files; the caller holds mu.
func (s *veUploadSessions) sweep() {
	now := s.now()
	for id, session := range s.sessions {
		if now.After(session.expires) && !session.completing {
			_ = os.RemoveAll(session.dir)
			delete(s.sessions, id)
		}
	}
}

// start opens a session for a file of size bytes split into veChunkBytes chunks.
func (s *veUploadSessions) start(user, project, clientUploadID, name, mime string, size int64) (*veUploadSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	open := 0
	for _, session := range s.sessions {
		if session.User == user {
			open++
		}
	}
	if open >= veSessionsPerUser {
		return nil, repo.ErrVEUploadBusy
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return nil, ErrVEMedia
	}
	dir, err := os.MkdirTemp(s.root, "upload-*")
	if err != nil {
		return nil, ErrVEMedia
	}
	path := filepath.Join(dir, "source")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err == nil {
		err = file.Truncate(size)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, ErrVEMedia
	}
	chunks := int((size + veChunkBytes - 1) / veChunkBytes)
	session := &veUploadSession{ID: uuid.NewString(), User: user, Project: project, ClientUploadID: clientUploadID, Name: name, MIME: mime, Size: size, Chunks: chunks, dir: dir, path: path, received: make([]bool, chunks), expires: s.now().Add(veSessionTTL)}
	s.sessions[session.ID] = session
	return session, nil
}

func (s *veUploadSessions) get(id, user, project string) (*veUploadSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	session, ok := s.sessions[id]
	if !ok || session.User != user || session.Project != project {
		return nil, ErrVEUploadSession
	}
	return session, nil
}

// ChunkBytes is the length of every chunk but the last.
func (session *veUploadSession) ChunkBytes() int64 { return veChunkBytes }

// chunkSize is the exact length chunk index must have.
func (session *veUploadSession) chunkSize(index int) int64 {
	if index == session.Chunks-1 {
		return session.Size - int64(index)*veChunkBytes
	}
	return veChunkBytes
}

// put writes one chunk at its offset. Re-sending a chunk overwrites it (safe retries).
func (s *veUploadSessions) put(id, user, project string, index int, body io.Reader, length int64) (int, error) {
	session, err := s.get(id, user, project)
	if err != nil {
		return 0, err
	}
	if index < 0 || index >= session.Chunks || length != session.chunkSize(index) {
		return 0, ErrVEUploadChunk
	}
	file, err := os.OpenFile(session.path, os.O_WRONLY, 0600)
	if err != nil {
		return 0, ErrVEUploadSession
	}
	written, err := io.Copy(io.NewOffsetWriter(file, int64(index)*veChunkBytes), io.LimitReader(body, length+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil || written != length {
		return 0, ErrVEUploadChunk
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session.received[index] = true
	session.expires = s.now().Add(veSessionTTL)
	done := 0
	for _, ok := range session.received {
		if ok {
			done++
		}
	}
	return done, nil
}

// take hands a complete session's file over to the caller, who then owns dir and must
// remove it. Missing chunks are listed so the client can resend them.
func (s *veUploadSessions) take(id, user, project string) (*veUploadSession, []int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok || session.User != user || session.Project != project || session.completing {
		return nil, nil, ErrVEUploadSession
	}
	missing := []int{}
	for i, ok := range session.received {
		if !ok {
			missing = append(missing, i)
		}
	}
	if len(missing) > 0 {
		return nil, missing, ErrVEUploadMissing
	}
	session.completing = true
	delete(s.sessions, id)
	return session, nil, nil
}

// abort drops a session and its file (the user cancelled).
func (s *veUploadSessions) abort(id, user, project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok && session.User == user && session.Project == project {
		_ = os.RemoveAll(session.dir)
		delete(s.sessions, id)
	}
}
