package editorai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type geminiCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}
type geminiFunctionResponse struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}
type geminiChatPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
	Inline           *inlineData             `json:"inlineData,omitempty"`
	FunctionCall     *geminiCall             `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}
type geminiChatContent struct {
	Role  string           `json:"role"`
	Parts []geminiChatPart `json:"parts"`
}
type geminiFunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func (g *Gemini) Name() string { return "gemini" }
func (g *Gemini) Model() string {
	if g == nil {
		return ""
	}
	return g.model
}

func geminiImageParts(images []Image) []geminiChatPart {
	parts := []geminiChatPart{}
	for _, image := range images {
		label, _ := json.Marshal(struct {
			AssetID string  `json:"assetId"`
			Time    float64 `json:"time"`
		}{image.AssetID, image.Time})
		parts = append(parts, geminiChatPart{Text: string(label)}, geminiChatPart{Inline: &inlineData{MIME: "image/jpeg", Data: base64.StdEncoding.EncodeToString(image.Data)}})
	}
	return parts
}

// geminiContents maps neutral turns to Gemini contents. A Gemini assistant turn is
// replayed verbatim so its thought signatures survive.
func geminiContents(turns []ChatTurn) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	for _, turn := range turns {
		if turn.Role == "assistant" && turn.NativeBy == "gemini" && len(turn.Native) > 0 {
			out = append(out, turn.Native)
			continue
		}
		content := geminiChatContent{Role: "user"}
		if turn.Role == "assistant" {
			content.Role = "model"
			if turn.Text != "" {
				content.Parts = append(content.Parts, geminiChatPart{Text: turn.Text})
			}
			for _, call := range turn.ToolCalls {
				content.Parts = append(content.Parts, geminiChatPart{FunctionCall: &geminiCall{ID: call.ID, Name: call.Name, Args: chatJSONObject(call.Args)}})
			}
		} else {
			for _, result := range turn.ToolResults {
				id := result.CallID
				if len(id) > 5 && id[:5] == "local" {
					id = "" // ids we minted are not Gemini's
				}
				content.Parts = append(content.Parts, geminiChatPart{FunctionResponse: &geminiFunctionResponse{ID: id, Name: result.Name, Response: chatJSONObject(result.Content)}})
			}
			for _, result := range turn.ToolResults {
				content.Parts = append(content.Parts, geminiImageParts(result.Images)...)
			}
			if turn.Text != "" {
				content.Parts = append(content.Parts, geminiChatPart{Text: turn.Text})
			}
			content.Parts = append(content.Parts, geminiImageParts(turn.Images)...)
		}
		if len(content.Parts) == 0 {
			continue
		}
		raw, e := json.Marshal(content)
		if e != nil {
			return nil, ErrInvalid
		}
		out = append(out, raw)
	}
	return out, nil
}

func (g *Gemini) Chat(ctx context.Context, in ChatRequest) (ChatReply, error) {
	if !g.Available() {
		return ChatReply{}, ErrUnavailable
	}
	contents, e := geminiContents(in.Turns)
	if e != nil {
		return ChatReply{}, e
	}
	declarations := make([]geminiFunctionDeclaration, 0, len(in.Tools))
	for _, tool := range in.Tools {
		declarations = append(declarations, geminiFunctionDeclaration{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters})
	}
	body := map[string]any{
		"systemInstruction": geminiContent{Parts: []geminiPart{{Text: in.System}}},
		"contents":          contents,
		"generationConfig":  map[string]any{"maxOutputTokens": chatOutputTokens(in.MaxOutputTokens)},
	}
	if len(declarations) > 0 {
		body["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
		body["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return ChatReply{}, ErrInvalid
	}
	if len(raw) > chatMaxRequestBytes+(4<<20) {
		return ChatReply{}, ErrTooLarge
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint+"/models/"+url.PathEscape(g.model)+":generateContent", bytes.NewReader(raw))
	if e != nil {
		return ChatReply{}, ErrUnavailable
	}
	req.Header.Set("x-goog-api-key", g.key)
	req.Header.Set("Content-Type", "application/json")
	res, e := g.client.Do(req)
	if e != nil {
		return ChatReply{}, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return ChatReply{}, &RateError{RetryAt: time.Now().UTC().Add(retryAfter(res.Header.Get("Retry-After")))}
	}
	if res.StatusCode != 200 {
		log.Printf("editorai: gemini chat model=%s http=%d %s", g.model, res.StatusCode, geminiErrorSummary(res.Body))
		return ChatReply{}, ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil {
		return ChatReply{}, ErrUnavailable
	}
	if len(data) > 2<<20 {
		return ChatReply{}, ErrInvalid
	}
	var out struct {
		Candidates []struct {
			Content      json.RawMessage `json:"content"`
			FinishReason string          `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Prompt     int `json:"promptTokenCount"`
			Candidates int `json:"candidatesTokenCount"`
			Thoughts   int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Candidates) != 1 || out.Candidates[0].FinishReason != "STOP" {
		return ChatReply{}, ErrInvalid
	}
	var content geminiChatContent
	if json.Unmarshal(out.Candidates[0].Content, &content) != nil {
		return ChatReply{}, ErrInvalid
	}
	reply := ChatReply{Native: out.Candidates[0].Content, NativeBy: "gemini", Usage: ChatUsage{InputTokens: out.Usage.Prompt, OutputTokens: out.Usage.Candidates + out.Usage.Thoughts}}
	for _, part := range content.Parts {
		switch {
		case part.FunctionCall != nil:
			id := part.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("local-%d", len(reply.ToolCalls)+1)
			}
			reply.ToolCalls = append(reply.ToolCalls, ToolCall{ID: id, Name: part.FunctionCall.Name, Args: chatJSONObject(part.FunctionCall.Args)})
		case part.Text != "" && !part.Thought:
			reply.Text += part.Text
		}
	}
	if reply.Text == "" && len(reply.ToolCalls) == 0 {
		return ChatReply{}, ErrInvalid
	}
	return reply, nil
}

// retryAfter reads a Retry-After header (seconds or HTTP date), defaulting to 60 s and capped at a day.
func retryAfter(header string) time.Duration {
	delay := 60 * time.Second
	if seconds, e := strconv.Atoi(header); e == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if date, e := http.ParseTime(header); e == nil && time.Until(date) > 0 {
		delay = time.Until(date)
	}
	return min(delay, 24*time.Hour)
}
