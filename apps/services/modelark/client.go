package modelark

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/volcengine/ark-runtime-go/arkruntime"
	"github.com/volcengine/ark-runtime-go/arkruntime/model/contentgeneration"
)

// Client is the interface consumed by facades.
// Keeping it narrow — only the operations VIOLET STUDIO needs.
type Client interface {
	CreateTask(ctx context.Context, req CreateTaskRequest) (providerTaskID string, err error)
	GetTask(ctx context.Context, providerTaskID string) (*TaskResult, error)
	CancelTask(ctx context.Context, providerTaskID string) error
}

type arkClient struct {
	sdk *arkruntime.Client
}

// NewClient builds a production client using the official BytePlus SDK.
// baseURL must include /api/v3 (e.g. https://ark.ap-southeast.bytepluses.com/api/v3).
func NewClient(apiKey, baseURL string) Client {
	if baseURL != "" && !strings.Contains(baseURL, "/api/v3") {
		log.Printf("[ark] WARNING: ARK_BASE_URL %q does not contain /api/v3 — appending it automatically", baseURL)
		baseURL = strings.TrimSuffix(baseURL, "/") + "/api/v3"
	}

	opts := []arkruntime.ConfigOption{}
	if baseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(baseURL))
	}
	c := arkruntime.NewByteplusClientWithApiKey(apiKey, opts...)
	return &arkClient{sdk: c}
}

func (a *arkClient) CreateTask(ctx context.Context, req CreateTaskRequest) (string, error) {
	sdkReq := &contentgeneration.CreateContentGenerationTaskRequest{
		Model:   req.Model,
		Content: toSDKContent(req.Content),
	}

	if req.Ratio != "" {
		sdkReq.Ratio = contentgeneration.OptString{Value: req.Ratio, Set: true}
	}
	if req.DurationSeconds > 0 {
		sdkReq.Duration = contentgeneration.OptInt64{Value: req.DurationSeconds, Set: true}
	}
	if req.Resolution != "" {
		sdkReq.Resolution = contentgeneration.OptString{Value: req.Resolution, Set: true}
	}
	if req.OptimizePrompt {
		sdkReq.OptimizePrompt = contentgeneration.OptBool{Value: true, Set: true}
	}
	if req.GenerateAudio {
		sdkReq.GenerateAudio = contentgeneration.OptBool{Value: true, Set: true}
	}
	sdkReq.Watermark = contentgeneration.OptBool{Value: req.Watermark, Set: true}
	if req.ReturnLastFrame {
		sdkReq.ReturnLastFrame = contentgeneration.OptBool{Value: true, Set: true}
	}
	if req.ExpiresAfterSeconds > 0 {
		sdkReq.ExecutionExpiresAfter = contentgeneration.OptInt64{Value: req.ExpiresAfterSeconds, Set: true}
	}

	res, err := a.sdk.CreateContentGenerationTask(ctx, sdkReq)
	if err != nil {
		return "", fmt.Errorf("%w: create task: %v", ErrProviderError, sanitizeProviderErr(err))
	}
	return res.ID, nil
}

func (a *arkClient) GetTask(ctx context.Context, providerTaskID string) (*TaskResult, error) {
	res, err := a.sdk.GetContentGenerationTask(ctx, providerTaskID)
	if err != nil {
		return nil, fmt.Errorf("%w: get task: %v", ErrProviderError, sanitizeProviderErr(err))
	}
	return fromSDKTask(&res.ContentGenerationTask), nil
}

func (a *arkClient) CancelTask(ctx context.Context, providerTaskID string) error {
	if err := a.sdk.DeleteContentGenerationTask(ctx, providerTaskID); err != nil {
		return fmt.Errorf("%w: cancel task: %v", ErrProviderError, sanitizeProviderErr(err))
	}
	return nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

func toSDKContent(items []ContentItem) []contentgeneration.ContentItem {
	out := make([]contentgeneration.ContentItem, 0, len(items))
	for _, item := range items {
		var ci contentgeneration.ContentItem

		switch item.Type {
		case ContentTypeText:
			ci.Type = contentgeneration.ContentTypeText
			ci.Text = contentgeneration.OptString{Value: item.Text, Set: true}
		case ContentTypeImageURL:
			ci.Type = contentgeneration.ContentTypeImageURL
			ci.ImageURL = contentgeneration.OptImageURL{
				Value: contentgeneration.ImageURL{URL: item.URL},
				Set:   true,
			}
		case ContentTypeVideoURL:
			ci.Type = contentgeneration.ContentTypeVideoURL
			ci.VideoURL = contentgeneration.OptVideoURL{
				Value: contentgeneration.VideoURL{URL: item.URL},
				Set:   true,
			}
		default:
			continue
		}

		if item.Role != "" {
			ci.Role = contentgeneration.OptString{Value: string(item.Role), Set: true}
		}
		out = append(out, ci)
	}
	return out
}

func fromSDKTask(t *contentgeneration.ContentGenerationTask) *TaskResult {
	r := &TaskResult{
		ProviderTaskID: t.ID,
		Status:         ProviderStatus(t.Status),
	}

	if t.RevisedPrompt.Set {
		r.RevisedPrompt = t.RevisedPrompt.Value
	}
	if t.Content.Set {
		if t.Content.Value.VideoURL.Set {
			r.VideoURL = t.Content.Value.VideoURL.Value
		}
		if t.Content.Value.LastFrameURL.Set {
			r.LastFrameURL = t.Content.Value.LastFrameURL.Value
		}
	}
	if t.Error.Set {
		r.ErrorCode = t.Error.Value.Code
		r.ErrorMessage = t.Error.Value.Message
	}
	if t.Duration.Set {
		r.DurationSeconds = t.Duration.Value
	}
	if t.Resolution.Set {
		r.Resolution = t.Resolution.Value
	}

	return r
}

// sanitizeProviderErr removes any value that might contain an API key from error messages.
// The raw error is never forwarded; only a sanitized version is stored.
func sanitizeProviderErr(err error) string {
	if err == nil {
		return ""
	}
	// err.Error() may contain partial response bodies; return a safe summary.
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}
