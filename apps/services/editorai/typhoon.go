package editorai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
)

type TyphoonASR struct {
	Python, Runner, Model, Version string
	once                           sync.Once
	ready                          bool
}

func (a *TyphoonASR) Signature() string {
	return "typhoon-asr-realtime:onnx-cpu:context-windows-v1:" + a.Version + ":" + a.Model
}

// CacheSignature versions decoding boundaries without invalidating stored job identity.
func (a *TyphoonASR) CacheSignature() string {
	return a.Signature() + ":pause-segments-v2"
}

func (a *TyphoonASR) Available() bool {
	a.once.Do(func() {
		if a.Python == "" || a.Runner == "" || a.Version == "" || !filepath.IsAbs(a.Model) {
			return
		}
		for _, name := range []string{"encoder-fastconformer-quran-ar.onnx", "decoder_joint-fastconformer-quran-ar.onnx", "vocab.json"} {
			info, err := os.Stat(filepath.Join(a.Model, name))
			if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, a.Python, a.Runner, "--check")
		configureProcess(cmd)
		a.ready = cmd.Run() == nil
	})
	return a.ready
}

func (a *TyphoonASR) Transcribe(ctx context.Context, audio []Audio) ([]models.EditorAITranscript, error) {
	if !a.Available() {
		return nil, ErrASR
	}
	if len(audio) == 0 || len(audio) > 8 {
		return nil, ErrTooLarge
	}
	raw, err := json.Marshal(struct {
		ModelPath string  `json:"modelPath"`
		Files     []Audio `json:"files"`
	}{a.Model, audio})
	if err != nil || len(raw) > 64<<10 {
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

// Unknown provider names fail closed instead of silently using a different model.
func SelectTranscriber(provider string, whisper *LocalASR, typhoon *TyphoonASR) Transcriber {
	switch provider {
	case "", "whisper":
		return whisper
	case "typhoon":
		return typhoon
	default:
		return &TyphoonASR{}
	}
}
