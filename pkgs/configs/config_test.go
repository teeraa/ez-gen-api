package configs

import "testing"

func TestEditorASRProviderConfiguration(t *testing.T) {
	t.Setenv("EDITOR_ASR_PROVIDER", "")
	c := Load()
	if c.EditorASRProvider != "whisper" || c.TyphoonRunner != "scripts/typhoon/transcribe.py" {
		t.Fatal("existing ASR default changed")
	}
	t.Setenv("EDITOR_ASR_PROVIDER", " TYPHOON ")
	t.Setenv("TYPHOON_PYTHON", "/test/python")
	t.Setenv("TYPHOON_MODEL_PATH", "/test/model")
	t.Setenv("TYPHOON_MODEL_VERSION", "reviewed-revision")
	t.Setenv("WHISPER_LANGUAGE", "th")
	c = Load()
	if c.EditorASRProvider != "typhoon" || c.TyphoonPython != "/test/python" || c.TyphoonModelPath != "/test/model" || c.TyphoonModelVersion != "reviewed-revision" || c.WhisperLanguage != "th" {
		t.Fatal("independent Typhoon configuration ignored")
	}
}

func TestIndependentImageConfiguration(t *testing.T) {
	t.Setenv("ARK_API_KEY", "shared-test-key")
	t.Setenv("ARK_BASE_URL", "https://shared.invalid/api/v3")
	t.Setenv("ARK_DEFAULT_MODEL", "video-model")
	t.Setenv("ARK_IMAGE_API_KEY", "")
	t.Setenv("ARK_IMAGE_BASE_URL", "")
	t.Setenv("ARK_IMAGE_MODEL", "")
	t.Setenv("STORAGE_BUCKET", "existing-video")
	t.Setenv("STORAGE_IMAGE_BUCKET", "")
	c := Load()
	if c.ArkImageModel != "seedream-5-0-260128" || c.ArkImageAPIKey != c.ArkAPIKey || c.ArkImageBaseURL != c.ArkBaseURL || c.StorageImageBucket != "violet-characters" {
		t.Fatal("image defaults/fallbacks incorrect")
	}
	t.Setenv("ARK_IMAGE_API_KEY", "image-test-key")
	t.Setenv("ARK_IMAGE_BASE_URL", "https://images.invalid/api/v3")
	t.Setenv("ARK_IMAGE_MODEL", "custom-image")
	t.Setenv("STORAGE_IMAGE_BUCKET", "custom-characters")
	c = Load()
	if c.ArkImageAPIKey != "image-test-key" || c.ArkImageBaseURL != "https://images.invalid/api/v3" || c.ArkImageModel != "custom-image" || c.StorageImageBucket != "custom-characters" {
		t.Fatal("image overrides ignored")
	}
	if c.ArkAPIKey != "shared-test-key" || c.ArkDefaultModel != "video-model" || c.StorageBucket != "existing-video" {
		t.Fatal("image overrides changed video configuration")
	}
}

func TestReferenceAnalysisExplicitModelAndFallbacks(t *testing.T) {
	t.Setenv("ARK_REFERENCE_ANALYSIS_MODEL", "")
	t.Setenv("ARK_REFERENCE_ANALYSIS_API_KEY", "")
	t.Setenv("ARK_REFERENCE_ANALYSIS_BASE_URL", "")
	t.Setenv("ARK_IMAGE_API_KEY", "image-fixture-key")
	t.Setenv("ARK_IMAGE_BASE_URL", "https://image.invalid/api/v3")
	cfg := Load()
	if cfg.ArkReferenceAnalysisModel != "" || cfg.ArkReferenceAnalysisAPIKey != "image-fixture-key" || cfg.ArkReferenceAnalysisBaseURL != "https://image.invalid/api/v3" {
		t.Fatal("analysis defaults/fallbacks wrong")
	}
	t.Setenv("ARK_REFERENCE_ANALYSIS_MODEL", "vision-fixture")
	t.Setenv("ARK_REFERENCE_ANALYSIS_API_KEY", "vision-fixture-key")
	t.Setenv("ARK_REFERENCE_ANALYSIS_BASE_URL", "https://vision.invalid/api/v3")
	cfg = Load()
	if cfg.ArkReferenceAnalysisModel != "vision-fixture" || cfg.ArkReferenceAnalysisAPIKey != "vision-fixture-key" || cfg.ArkReferenceAnalysisBaseURL != "https://vision.invalid/api/v3" || cfg.ArkImageAPIKey != "image-fixture-key" {
		t.Fatal("analysis overrides not isolated")
	}
}
