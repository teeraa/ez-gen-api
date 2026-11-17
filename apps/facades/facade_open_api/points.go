package facade_open_api

import (
	"context"
	"errors"
	"log"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
)

// Point charging for AI features outside Director and Film, which keep their own job
// ledgers. Every AI call a user triggers is charged against the shared wallet
// (director_point_accounts) before the provider is called, and refunded when the
// provider rejects or fails the work.

// PointWallet is implemented by repository_open_api.PointWallet.
type PointWallet interface {
	Charge(ctx context.Context, user, feature, ref string, amount int) (bool, error)
	Refund(ctx context.Context, user, feature, ref string) error
}

// PointPrices is the price in points of one AI use per feature.
type PointPrices struct {
	ChatMessage    int // one message to the AI chat editor
	Video          int // one video generation
	VideoDraft     int // one draft (lower quality) video generation
	Image          int // one image generation
	StoryAnalysis  int // one story / shot-plan analysis by the text model
	EditorAnalysis int // one AI editor analysis job (transcription + planning)
	EditorEdit     int // one AI instruction-edit preview
	Voice          int // one text-to-speech generation
	TextSuggestion int // one marketplace AI suggestion
}

// Feature names stored in ai_point_entries.feature.
const (
	pointFeatureChat           = "ai_chat"
	pointFeatureVideo          = "video_job"
	pointFeatureImage          = "image_job"
	pointFeatureStoryAnalysis  = "story_analysis"
	pointFeatureStoryImage     = "story_image"
	pointFeatureStoryVideo     = "story_video"
	pointFeatureEditorAnalysis = "editor_ai_job"
	pointFeatureEditorEdit     = "editor_ai_edit"
	pointFeatureVoice          = "voice"
	pointFeatureEditorVoice    = "editor_voice"
	pointFeatureMarketplace    = "marketplace_ai"
)

var (
	points      PointWallet
	pointPrices PointPrices
)

// ConfigurePoints turns on charging. Without it (tests, tools) AI features are free.
func ConfigurePoints(w PointWallet, p PointPrices) {
	points, pointPrices = w, p
}

// IsInsufficientPoints reports whether e is a failed charge for lack of points.
func IsInsufficientPoints(e error) (*repo.DirectorInsufficient, bool) {
	var d *repo.DirectorInsufficient
	if errors.As(e, &d) {
		return d, true
	}
	return nil, false
}

// chargePoints takes amount points for (feature, ref); see repo.PointWallet.Charge.
// Use it with a ref that names exactly one AI use.
func chargePoints(ctx context.Context, user, feature, ref string, amount int) error {
	_, e := chargePointsNew(ctx, user, feature, ref, amount)
	return e
}

// chargePointsNew is chargePoints that also reports whether this call took the points
// (false when the ref was already charged), for callers that must undo only their own charge.
func chargePointsNew(ctx context.Context, user, feature, ref string, amount int) (bool, error) {
	if points == nil || amount <= 0 {
		return false, nil
	}
	return points.Charge(ctx, user, feature, ref, amount)
}

// refundPoints gives back the charge for (feature, ref). It survives a cancelled request
// context and only logs failures: the work it follows has already failed.
func refundPoints(ctx context.Context, user, feature, ref string) {
	if points == nil {
		return
	}
	if e := points.Refund(context.WithoutCancel(ctx), user, feature, ref); e != nil {
		log.Printf("[points] refund feature=%s ref=%s: %v", feature, ref, e)
	}
}

const insufficientPointsMessage = "พอยต์ไม่เพียงพอ กรุณาเติมพอยต์ก่อนลองใหม่"

func imagePointRef(user, idempotencyKey string) string { return user + ":" + idempotencyKey }

// RefundImageJob returns the points charged for an image job the worker could not finish.
func RefundImageJob(ctx context.Context, j *models.VideoGenerationJob) {
	if j == nil || j.IdempotencyKey == nil {
		return
	}
	user := j.UserID.String()
	refundPoints(ctx, user, pointFeatureImage, imagePointRef(user, *j.IdempotencyKey))
}
