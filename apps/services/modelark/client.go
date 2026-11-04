package modelark

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

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
	sdk      *arkruntime.Client
	recorder UsageRecorder
}

// NewClient builds a production client using the official BytePlus SDK.
// baseURL must include /api/v3 (e.g. https://ark.ap-southeast.bytepluses.com/api/v3).
func NewClient(apiKey, baseURL string) Client { return NewClientWithUsage(apiKey, baseURL, nil) }

func NewClientWithUsage(apiKey, baseURL string, recorder UsageRecorder) Client {
	if baseURL != "" && !strings.Contains(baseURL, "/api/v3") {
		log.Printf("[ark] WARNING: ARK_BASE_URL %q does not contain /api/v3 — appending it automatically", baseURL)
		baseURL = strings.TrimSuffix(baseURL, "/") + "/api/v3"
	}

	opts := []arkruntime.ConfigOption{arkruntime.WithRetryTimes(0), arkruntime.WithTimeout(20 * time.Second), arkruntime.WithHTTPClient(&http.Client{Timeout: 20 * time.Second, Transport: ownedSDKTransport()})}
	if baseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(baseURL))
	}
	c := newSDKClient(apiKey, opts...)
	return &arkClient{sdk: c, recorder: recorder}
}

func (a *arkClient) CreateTask(ctx context.Context, req CreateTaskRequest) (string, error) {
	event, err := beginUsage(ctx, a.recorder, req.Model, "video_generation")
	if err != nil {
		return "", err
	}
	event.Status = "failed"
	defer func() { finishUsage(ctx, a.recorder, event) }()
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
	if req.Draft {
		sdkReq.Draft = contentgeneration.OptBool{Value: true, Set: true}
	}

	res, err := a.sdk.CreateContentGenerationTask(ctx, sdkReq)
	if err != nil {
		return "", classifyVideoError(err)
	}
	event.Status = "submitted"
	event.ProviderTaskID = res.ID
	return res.ID, nil
}

func (a *arkClient) GetTask(ctx context.Context, providerTaskID string) (*TaskResult, error) {
	res, err := a.sdk.GetContentGenerationTask(ctx, providerTaskID)
	if err != nil {
		return nil, classifyVideoError(err)
	}
	task := &res.ContentGenerationTask
	if a.recorder != nil && (task.Status == "succeeded" || task.Status == "failed" || task.Status == "cancelled" || task.Status == "expired") {
		owner, ok := ctx.Value(usageOwnerKey{}).(usageOwner)
		if !ok {
			return nil, errors.New("missing AI usage owner")
		}
		now := time.Now().UTC()
		event := UsageEvent{UserID: owner.userID, JobID: owner.jobID, Provider: "byteplus", ProviderTaskID: providerTaskID, Status: string(task.Status), CompletedAt: &now}
		if task.Usage.Set {
			event.OutputTokens = nonnegativeToken(task.Usage.Value.CompletionTokens)
			event.TotalTokens = nonnegativeToken(task.Usage.Value.TotalTokens)
		}
		if task.Duration.Set {
			event.DurationSeconds = nonnegativeToken(task.Duration.Value)
		}
		if err := recordUsage(ctx, a.recorder, event); err != nil {
			return nil, errors.New("cannot record AI usage result")
		}
	}
	return fromSDKTask(task), nil
}

func (a *arkClient) CancelTask(ctx context.Context, providerTaskID string) error {
	if err := a.sdk.DeleteContentGenerationTask(ctx, providerTaskID); err != nil {
		return classifyVideoError(err)
	}
	if a.recorder != nil {
		owner, ok := ctx.Value(usageOwnerKey{}).(usageOwner)
		if ok {
			now := time.Now().UTC()
			event := UsageEvent{UserID: owner.userID, JobID: owner.jobID, Provider: "byteplus", ProviderTaskID: providerTaskID, Status: "cancelled", CompletedAt: &now}
			if err := recordUsage(ctx, a.recorder, event); err != nil {
				log.Print("AI usage cancellation persistence failed")
			}
		}
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
		case ContentTypeDraftTask:
			ci.Type = contentgeneration.ContentTypeDraftTask
			ci.DraftTask = contentgeneration.NewOptDraftTask(contentgeneration.DraftTask{ID: item.DraftTaskID})
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
		safe := videoFailure(t.Error.Value.Code, 0)
		r.ErrorCode = safe.Code
		r.ErrorMessage = safe.Message
	}
	if r.Status == ProviderStatusFailed {
		providerCode := ""
		if t.Error.Set {
			providerCode = t.Error.Value.Code
		}
		if safeVideoCode(providerCode) == "" || r.ErrorCode == "" || r.ErrorCode == "PROVIDER_ERROR" {
			r.ErrorCode = "PROVIDER_GENERATION_FAILED"
			r.ErrorMessage = "ผู้ให้บริการยืนยันว่าการสร้างวิดีโอไม่สำเร็จ ภาพที่บันทึกแล้วจะยังอยู่ในคลัง"
		}
		r.FailureDiagnostic = GenerationFailureDetails(providerCode)
	}
	if t.Duration.Set {
		r.DurationSeconds = t.Duration.Value
	}
	if t.Resolution.Set {
		r.Resolution = t.Resolution.Value
	}

	return r
}
