// Package seedaudio calls BytePlus Seed Audio (non-streaming text-to-speech / audio generation).
// POST {base}/api/v3/tts/create with an X-Api-Key header; the response carries the audio as
// base64 and/or a URL that expires after two hours, so callers must store the bytes themselves.
package seedaudio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	Model           = "seed-audio-1.0"
	MaxPromptRunes  = 3000
	MaxOutputSecond = 120
	maxAudioBytes   = 50 << 20
)

var (
	// ErrUnavailable means no API key is configured.
	ErrUnavailable = errors.New("seed audio not configured")
	// ErrRejected is a definitive refusal (bad input, content policy); retrying the same input fails again.
	ErrRejected = errors.New("seed audio rejected the request")
	// ErrNotGranted is a 401/403: the key is wrong or the account has not been granted the
	// seed-audio service. Fixed in the BytePlus console, not by changing the input.
	ErrNotGranted = errors.New("seed audio access not granted")
)

type Client struct {
	key  string
	base string
	http *http.Client
}

// New returns nil when apiKey is empty, so callers can treat a nil client as "feature off".
func New(apiKey, baseURL string) *Client {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = "https://voice.ap-southeast-1.bytepluses.com"
	}
	return &Client{key: apiKey, base: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 150 * time.Second}}
}

// Reference is one entry of "references": exactly one field is set.
type Reference struct {
	Speaker   string `json:"speaker,omitempty"`
	AudioURL  string `json:"audio_url,omitempty"`
	AudioData string `json:"audio_data,omitempty"`
	ImageURL  string `json:"image_url,omitempty"`
	ImageData string `json:"image_data,omitempty"`
}

type AudioConfig struct {
	Format         string `json:"format,omitempty"`
	SampleRate     int    `json:"sample_rate,omitempty"`
	SpeechRate     int    `json:"speech_rate,omitempty"`
	LoudnessRate   int    `json:"loudness_rate,omitempty"`
	PitchRate      int    `json:"pitch_rate,omitempty"`
	EnableSubtitle bool   `json:"enable_subtitle,omitempty"`
}

type Request struct {
	TextPrompt string      `json:"text_prompt"`
	References []Reference `json:"references,omitempty"`
	Audio      AudioConfig `json:"audio_config"`
	Watermark  *Watermark  `json:"watermark,omitempty"`
}

// Watermark marks synthesized audio as AI-generated. AIGCWatermark adds an audible
// marker at the end; AIGCMetadata only writes header metadata.
type Watermark struct {
	AIGCWatermark bool          `json:"aigc_watermark,omitempty"`
	AIGCMetadata  *AIGCMetadata `json:"aigc_metadata,omitempty"`
}

type AIGCMetadata struct {
	Enable          bool   `json:"enable"`
	ContentProducer string `json:"content_producer,omitempty"`
	ProduceID       string `json:"produce_id,omitempty"`
}

type Word struct {
	StartMS int    `json:"start_time"`
	EndMS   int    `json:"end_time"`
	Text    string `json:"text"`
}

type Sentence struct {
	StartMS int    `json:"start_time"`
	EndMS   int    `json:"end_time"`
	Text    string `json:"text"`
	Words   []Word `json:"words"`
}

type Result struct {
	Audio            []byte
	Format           string
	Duration         float64
	OriginalDuration float64
	Sentences        []Sentence
}

type response struct {
	Code             *int    `json:"code"`
	Message          string  `json:"message"`
	Audio            string  `json:"audio"`
	URL              string  `json:"url"`
	Duration         float64 `json:"duration"`
	OriginalDuration float64 `json:"original_duration"`
	Subtitle         *struct {
		Sentences []Sentence `json:"sentences"`
	} `json:"subtitle"`
}

// Generate synthesizes audio and returns its bytes (downloading the URL when no inline audio is sent).
func (c *Client) Generate(ctx context.Context, in Request) (*Result, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if in.Audio.Format == "" {
		in.Audio.Format = "mp3"
	}
	body := struct {
		Model string `json:"model"`
		Request
	}{Model, in}
	raw, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v3/tts/create", bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.key)
	req.Header.Set("X-Api-Request-Id", uuid.NewString())
	res, e := c.http.Do(req)
	if e != nil {
		return nil, fmt.Errorf("seed audio request: %w", e)
	}
	defer res.Body.Close()
	data, e := io.ReadAll(io.LimitReader(res.Body, maxAudioBytes*2))
	if e != nil {
		return nil, fmt.Errorf("seed audio read: %w", e)
	}
	var out response
	if e = json.Unmarshal(data, &out); e != nil {
		return nil, fmt.Errorf("seed audio http %d: invalid response", res.StatusCode)
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: http %d: %s", ErrNotGranted, res.StatusCode, out.Message)
	}
	if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: http %d: %s", ErrRejected, res.StatusCode, out.Message)
	}
	if res.StatusCode != http.StatusOK || out.Audio == "" && out.URL == "" {
		code := 0
		if out.Code != nil {
			code = *out.Code
		}
		return nil, fmt.Errorf("seed audio http %d code %d: %s", res.StatusCode, code, out.Message)
	}
	result := &Result{Format: in.Audio.Format, Duration: out.Duration, OriginalDuration: out.OriginalDuration}
	if out.Subtitle != nil {
		result.Sentences = out.Subtitle.Sentences
	}
	if out.Audio != "" {
		if result.Audio, e = base64.StdEncoding.DecodeString(out.Audio); e != nil {
			return nil, fmt.Errorf("seed audio base64: %w", e)
		}
		return result, nil
	}
	if result.Audio, e = c.download(ctx, out.URL); e != nil {
		return nil, e
	}
	return result, nil
}

func (c *Client) download(ctx context.Context, url string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	res, e := c.http.Do(req)
	if e != nil {
		return nil, fmt.Errorf("seed audio download: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("seed audio download http %d", res.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, maxAudioBytes+1))
	if e != nil || len(data) > maxAudioBytes {
		return nil, fmt.Errorf("seed audio download too large or failed: %v", e)
	}
	return data, nil
}
