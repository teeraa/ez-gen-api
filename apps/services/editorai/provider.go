package editorai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid AI result")
var ErrUnavailable = errors.New("AI provider unavailable")
var ErrTooLarge = errors.New("AI input too large")
var ErrASR = errors.New("ASR unavailable")

type RateError struct{ RetryAt time.Time }

func (e *RateError) Error() string { return "AI quota exceeded" }

type Image struct {
	AssetID string
	Time    float64
	Data    []byte
}
type Provider interface {
	Available() bool
	Signature() string
	Generate(context.Context, string, string, []Image, json.RawMessage) ([]byte, error)
}
type Gemini struct {
	key, model, version, endpoint string
	client                        *http.Client
}

func NewGemini(key, model, version string) *Gemini {
	return NewGeminiWithEndpoint(key, model, version, "https://generativelanguage.googleapis.com/v1beta", &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

// The alternate endpoint is dependency injection for an isolated fixture, never a client-supplied URL.
func NewGeminiWithEndpoint(key, model, version, endpoint string, client *http.Client) *Gemini {
	return &Gemini{key: key, model: model, version: version, endpoint: strings.TrimRight(endpoint, "/"), client: client}
}
func (g *Gemini) Available() bool {
	return g != nil && g.key != "" && g.model != "" && g.version != "" && g.client != nil
}
func (g *Gemini) Signature() string {
	return "gemini:" + g.model + ":" + g.version + ":editor-prompts-v3:frames512x3-v1"
}

type inlineData struct {
	MIME string `json:"mimeType"`
	Data string `json:"data"`
}
type geminiPart struct {
	Text   string      `json:"text,omitempty"`
	Inline *inlineData `json:"inlineData,omitempty"`
}
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}
type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
}

func (g *Gemini) Generate(ctx context.Context, system, prompt string, images []Image, schema json.RawMessage) ([]byte, error) {
	if !g.Available() {
		return nil, ErrUnavailable
	}
	if len(prompt) > 96<<10 || len(images) > 24 {
		return nil, ErrTooLarge
	}
	parts := []geminiPart{{Text: prompt}}
	n := 0
	for _, image := range images {
		n += len(image.Data)
		if n > 3<<20 {
			return nil, ErrTooLarge
		}
		label, _ := json.Marshal(struct {
			AssetID string  `json:"assetId"`
			Time    float64 `json:"time"`
		}{image.AssetID, image.Time})
		parts = append(parts, geminiPart{Text: string(label)}, geminiPart{Inline: &inlineData{MIME: "image/jpeg", Data: base64.StdEncoding.EncodeToString(image.Data)}})
	}
	body := struct {
		System     geminiContent   `json:"systemInstruction"`
		Contents   []geminiContent `json:"contents"`
		Generation struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
			ResponseFormat  struct {
				Text struct {
					MIME   string          `json:"mimeType"`
					Schema json.RawMessage `json:"schema"`
				} `json:"text"`
			} `json:"responseFormat"`
		} `json:"generationConfig"`
	}{System: geminiContent{Parts: []geminiPart{{Text: system}}}, Contents: []geminiContent{{Role: "user", Parts: parts}}}
	body.Generation.MaxOutputTokens = 8192
	body.Generation.ResponseFormat.Text.MIME = "APPLICATION_JSON"
	body.Generation.ResponseFormat.Text.Schema = schema
	raw, e := json.Marshal(body)
	if e != nil {
		return nil, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint+"/models/"+url.PathEscape(g.model)+":generateContent", bytes.NewReader(raw))
	if e != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("x-goog-api-key", g.key)
	req.Header.Set("Content-Type", "application/json")
	res, e := g.client.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		delay := 60 * time.Second
		if seconds, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		} else if date, e := http.ParseTime(res.Header.Get("Retry-After")); e == nil && time.Until(date) > 0 {
			delay = time.Until(date)
		}
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		return nil, &RateError{RetryAt: time.Now().UTC().Add(delay)}
	}
	if res.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil {
		return nil, ErrUnavailable
	}
	if len(data) > 2<<20 {
		return nil, ErrInvalid
	}
	var out geminiResponse
	if json.Unmarshal(data, &out) != nil || len(out.Candidates) != 1 || out.Candidates[0].FinishReason != "STOP" {
		return nil, ErrInvalid
	}
	text := ""
	for _, part := range out.Candidates[0].Content.Parts {
		text += part.Text
	}
	if text == "" || !json.Valid([]byte(text)) {
		return nil, ErrInvalid
	}
	return []byte(text), nil
}
