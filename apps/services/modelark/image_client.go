package modelark

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/volcengine/ark-runtime-go/arkruntime"
	"github.com/volcengine/ark-runtime-go/arkruntime/model/images"
	"io"
	"net/http"
	"strings"
	"time"
)

type ImageRequest struct {
	Model        string
	Prompt       string
	Size         string
	ReferenceURL string
	// ReferenceURLs sends several references in one request (e.g. every character in a shot).
	// The prompt refers to them as Image 1, Image 2, ... in this order.
	ReferenceURLs []string
}
type ImageGenerator interface {
	GenerateImage(context.Context, ImageRequest) ([]byte, error)
}
type imageClient struct {
	sdk      *arkruntime.Client
	model    string
	recorder UsageRecorder
}

func NewImageClient(key, base, model string) ImageGenerator {
	return NewImageClientWithUsage(key, base, model, nil)
}

func NewImageClientWithUsage(key, base, model string, recorder UsageRecorder) ImageGenerator {
	if !strings.Contains(base, "/api/v3") {
		base = strings.TrimRight(base, "/") + "/api/v3"
	}
	return &imageClient{newSDKClient(key, arkruntime.WithBaseUrl(base), arkruntime.WithTimeout(5*time.Minute), arkruntime.WithRetryTimes(0), arkruntime.WithHTTPClient(&http.Client{Timeout: 5 * time.Minute, Transport: imageTransport{ownedSDKTransport()}})), model, recorder}
}
func (a *imageClient) GenerateImage(c context.Context, r ImageRequest) ([]byte, error) {
	model := r.Model
	if model == "" {
		model = a.model
	}
	event, err := beginUsage(c, a.recorder, model, "image_generation")
	if err != nil {
		return nil, err
	}
	event.Status = "failed"
	defer func() { finishUsage(c, a.recorder, event) }()
	req := &images.CreateImageGenerationRequest{Model: model, Prompt: images.NewOptString(r.Prompt), Size: images.NewOptString(r.Size), ResponseFormat: images.NewOptResponseFormat(images.ResponseFormatB64JSON), Watermark: images.NewOptBool(false)}
	refs := r.ReferenceURLs
	if len(refs) == 0 && r.ReferenceURL != "" {
		refs = []string{r.ReferenceURL}
	}
	switch len(refs) {
	case 0:
	case 1:
		req.Image = images.NewOptCreateImageGenerationRequestImage(images.NewStringCreateImageGenerationRequestImage(refs[0]))
	default:
		req.Image = images.NewOptCreateImageGenerationRequestImage(images.NewStringArrayCreateImageGenerationRequestImage(refs))
	}
	res, e := a.sdk.GenerateImages(c, req)
	if e != nil {
		return nil, classifyImageError(e)
	}
	if res.Usage.Set {
		usage := res.Usage.Value
		if usage.OutputTokens.Set {
			event.OutputTokens = nonnegativeToken(usage.OutputTokens.Value)
		}
		if usage.TotalTokens.Set {
			event.TotalTokens = nonnegativeToken(usage.TotalTokens.Value)
		}
		event.GeneratedImages = nonnegativeToken(usage.GeneratedImages)
	}
	if res.Error.Set {
		return nil, imageFailure(res.Error.Value.Code, 0, nil)
	}
	if len(res.Data) == 1 && res.Data[0].Error.Set {
		return nil, imageFailure(res.Data[0].Error.Value.Code, 0, nil)
	}
	if len(res.Data) != 1 || !res.Data[0].B64JSON.Set {
		return nil, &ImageError{Code: "INVALID_OUTPUT", Message: "บริการสร้างภาพไม่ส่งคืนภาพที่ใช้งานได้ กรุณาลองเปลี่ยนคำอธิบายภาพ"}
	}
	b64 := res.Data[0].B64JSON.Value
	if len(b64) > 32<<20 {
		return nil, errors.New("image output exceeds limit")
	}
	data, e := base64.StdEncoding.DecodeString(b64)
	if e != nil {
		return nil, errors.New("image provider returned invalid data")
	}
	event.Status = "succeeded"
	return data, nil
}

type imageTransport struct{ base http.RoundTripper }
type limitedImageBody struct {
	io.Reader
	io.Closer
}

func (t imageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, e := t.base.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	res.Body = limitedImageBody{io.LimitReader(res.Body, 40<<20), res.Body}
	return res, nil
}
