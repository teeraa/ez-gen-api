package editorai

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Audio struct {
	AssetID  string  `json:"assetId"`
	Path     string  `json:"path"`
	Duration float64 `json:"duration"`
}
type Transcriber interface {
	Available() bool
	Signature() string
	Transcribe(context.Context, []Audio) ([]models.EditorAITranscript, error)
}
type LocalASR struct {
	Python, Runner, Model, Version, Device, Compute, Language string
	once                                                      sync.Once
	ready                                                     bool
}

func (a *LocalASR) Signature() string {
	return "faster-whisper:1.2.1:" + a.Version + ":" + a.Model + ":" + a.Language + ":" + a.Device + ":" + a.Compute + ":vad1:word1"
}
func (a *LocalASR) Available() bool {
	a.once.Do(func() {
		if a.Python == "" || a.Runner == "" || a.Model == "" || a.Version == "" {
			return
		}
		if !filepath.IsAbs(a.Model) {
			return
		}
		if _, e := os.Stat(filepath.Join(a.Model, "model.bin")); e != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, a.Python, a.Runner, "--check")
		configureProcess(cmd)
		a.ready = cmd.Run() == nil
	})
	return a.ready
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, ErrTooLarge
	}
	return b.Buffer.Write(p)
}
func (a *LocalASR) Transcribe(ctx context.Context, audio []Audio) ([]models.EditorAITranscript, error) {
	if !a.Available() {
		return nil, ErrASR
	}
	input := struct {
		ModelPath string  `json:"modelPath"`
		Device    string  `json:"device"`
		Compute   string  `json:"computeType"`
		Language  string  `json:"language"`
		Files     []Audio `json:"files"`
	}{a.Model, a.Device, a.Compute, a.Language, audio}
	raw, e := json.Marshal(input)
	if e != nil || len(raw) > 64<<10 {
		return nil, ErrTooLarge
	}
	cmd := exec.CommandContext(ctx, a.Python, a.Runner)
	configureProcess(cmd)
	cmd.Stdin = bytes.NewReader(raw)
	out := &boundedBuffer{limit: 8 << 20}
	cmd.Stdout = out
	cmd.Stderr = &boundedBuffer{limit: 8 << 10}
	if cmd.Run() != nil {
		return nil, ErrASR
	}
	var data struct {
		Transcripts []models.EditorAITranscript `json:"transcripts"`
	}
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	return data.Transcripts, nil
}
