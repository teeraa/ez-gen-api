package crons

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"strings"
	"time"
)

type imageStorage interface {
	Upload(context.Context, string, []byte, string) error
	SignedURL(context.Context, string, time.Duration) (string, time.Time, error)
}
type ImageWorker struct {
	repo      repo.StudioRepository
	uploads   repo.UploadRepository
	generator modelark.ImageGenerator
	storage   imageStorage
	analyzer  modelark.ReferenceAnalyzer
}

func NewImageWorker(r repo.StudioRepository, u repo.UploadRepository, g modelark.ImageGenerator, s imageStorage) *ImageWorker {
	return &ImageWorker{repo: r, uploads: u, generator: g, storage: s}
}
func (w *ImageWorker) WithReferenceAnalyzer(a modelark.ReferenceAnalyzer) *ImageWorker {
	w.analyzer = a
	return w
}
func (w *ImageWorker) Start(c context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Done():
			return
		case <-ticker.C:
			if e := w.RunOnce(c); e != nil {
				log.Print("image worker operation failed")
			}
		}
	}
}
func (w *ImageWorker) RunOnce(parent context.Context) error {
	j, e := w.repo.ClaimImage(parent)
	if e != nil {
		if e == repo.ErrNotFound {
			return nil
		}
		return e
	}
	c, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	fail := func(code, msg string) error {
		return w.repo.FinishImage(parent, j.ID.String(), models.JobStatusFailed, map[string]any{"failure_code": code, "failure_message": msg})
	}
	sizes := map[string]string{"1:1": "2048x2048", "3:4": "1728x2304", "4:3": "2304x1728", "16:9": "2560x1440", "9:16": "1440x2560"}
	var snapshot facade.CharacterReferenceSnapshot
	if e := json.Unmarshal(j.EffectiveInputs, &snapshot); e != nil || snapshot.Model == "" {
		return fail("INVALID_INPUT", "ไม่พบการตั้งค่าโมเดลของงานนี้ กรุณาส่งคำขอใหม่")
	}
	if snapshot.ImageGenerationMode != "" && snapshot.ImageGenerationMode != "text_to_image" && snapshot.ImageGenerationMode != "image_to_image" {
		return fail("INVALID_INPUT", "ไม่รองรับรูปแบบงานภาพนี้ กรุณาสร้างงานใหม่")
	}
	if snapshot.ReferenceStrategy != "" && (snapshot.ReferenceStrategy != "descriptive_text_v1" || j.UploadID == nil || snapshot.Category != "character" || snapshot.ImageGenerationMode != "text_to_image") {
		return fail("INVALID_INPUT", "ไม่รองรับรูปแบบภาพอ้างอิงนี้ กรุณาสร้างงานใหม่")
	}
	if snapshot.ReferenceStrategy != "" && snapshot.ReferenceAnalysisVersion != characterdesign.ReferenceAnalysisVersion && snapshot.ReferenceAnalysisVersion != characterdesign.LegacyReferenceAnalysisVersion {
		return fail("INVALID_INPUT", "ไม่รองรับเวอร์ชันภาพอ้างอิงนี้ กรุณาสร้างงานใหม่")
	}
	prepared := snapshot.ReferenceStrategy != "" && strings.TrimSpace(snapshot.ReferenceDescription) != "" && j.CompiledPrompt != nil && strings.TrimSpace(*j.CompiledPrompt) != ""

	prompt := j.Prompt
	if j.CompiledPrompt != nil && strings.TrimSpace(*j.CompiledPrompt) != "" {
		prompt = *j.CompiledPrompt
	}
	in := modelark.ImageRequest{Model: snapshot.Model, Prompt: prompt, Size: sizes[j.AspectRatio]}
	if in.Size == "" {
		return fail("INVALID_INPUT", "ยังไม่รองรับขนาดภาพที่เลือก")
	}
	if j.UploadID != nil && !prepared {
		u, e := w.uploads.FindByID(c, j.UploadID.String(), j.UserID.String())
		if e != nil || u.ExpiresAt.Before(time.Now()) {
			return fail("IMAGE_EXPIRED", "กรุณาอัปโหลดภาพอ้างอิงใหม่แล้วลองอีกครั้ง")
		}
		url, _, e := w.storage.SignedURL(c, u.StorageKey, 10*time.Minute)
		if e != nil {
			return fail("STORAGE_ERROR", "เปิดภาพอ้างอิงไม่ได้ กรุณาลองอีกครั้งภายหลัง")
		}
		if snapshot.ReferenceStrategy != "" {
			prompt, err := facade.PrepareCharacterReference(modelark.WithUsageOwner(c, j.UserID, j.ID), w.analyzer, w.repo, j, snapshot, url)
			if err != nil {
				var preparation *facade.ReferencePreparationError
				if errors.As(err, &preparation) {
					log.Printf("reference preparation failed job=%s stage=%q", j.ID.String(), imageLogValue(preparation.Stage))
					return fail("REFERENCE_PREPARATION_FAILED", "เตรียมหรือบันทึกคำอธิบายภาพไม่ได้ กรุณาตรวจสถานะระบบแล้วลองใหม่")
				}
				code, message, d := modelark.ReferenceFailure(err)
				log.Printf("reference analysis failed job=%s model=%q code=%s stage=%q status=%d provider_code=%q param=%q finish_reason=%q content_bytes=%d", j.ID.String(), imageLogValue(snapshot.ReferenceAnalysisModel), code, d.Stage, d.HTTPStatus, d.ProviderCode, d.Param, d.FinishReason, d.ContentBytes)
				return fail(code, message)
			}
			in.Prompt = prompt
		} else if snapshot.ImageGenerationMode == "text_to_image" {
			return fail("INVALID_INPUT", "ไม่พบวิธีวิเคราะห์ภาพอ้างอิงของงานนี้ กรุณาสร้างงานใหม่")
		} else {
			in.ReferenceURL = url
		}
	}
	if c.Err() != nil {
		return fail("REFERENCE_ANALYSIS_FAILED", "การสร้างภาพถูกยกเลิก กรุณาลองใหม่")
	}
	data, e := w.generator.GenerateImage(modelark.WithUsageOwner(c, j.UserID, j.ID), in)
	if e != nil {
		d := modelark.ImageFailureDiagnostic(e)
		log.Printf("image generation failed job=%s model=%q size=%q has_reference=%t status=%d provider_code=%q param=%q", j.ID.String(), imageLogValue(in.Model), imageLogValue(in.Size), in.ReferenceURL != "", d.HTTPStatus, d.ProviderCode, d.Param)
		var providerErr *modelark.ImageError
		if errors.As(e, &providerErr) {
			return fail(providerErr.Code, providerErr.Message)
		}
		return fail("PROVIDER_ERROR", "สร้างภาพไม่สำเร็จ กรุณาตรวจสอบสถานะบริการแล้วลองอีกครั้ง")
	}
	kind, e := ValidateCharacterImage(data)
	if e != nil {
		return fail("INVALID_OUTPUT", "ภาพที่ได้รับจากบริการไม่สมบูรณ์ กรุณาลองสร้างใหม่")
	}
	ext := "png"
	if kind == "image/jpeg" {
		ext = "jpg"
	}
	key := storage.KeyForCharacter(j.UserID.String(), j.ID.String(), ext)
	if e = w.storage.Upload(c, key, data, kind); e != nil {
		return fail("STORAGE_ERROR", "บันทึกภาพที่สร้างแล้วไม่สำเร็จ กรุณาลองใหม่เมื่อระบบจัดเก็บกลับมาพร้อมใช้งาน")
	}
	return w.repo.FinishImage(parent, j.ID.String(), models.JobStatusSucceeded, map[string]any{"output_media_key": key, "output_media_type": "image", "output_thumb_key": key, "progress": 100, "current_step": "ภาพพร้อมใช้งาน"})
}
func ValidateCharacterImage(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 24<<20 {
		return "", image.ErrFormat
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return "", image.ErrFormat
	}
	if format != "png" && format != "jpeg" {
		return "", image.ErrFormat
	}
	if _, _, e = image.Decode(bytes.NewReader(data)); e != nil {
		return "", e
	}
	if format == "jpeg" {
		return "image/jpeg", nil
	}
	return "image/png", nil
}

// Bound snapshot fields and exclude characters that could carry URLs or log controls.
func imageLogValue(value string) string {
	if len(value) > 128 {
		return "unknown"
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return "unknown"
		}
	}
	return value
}
