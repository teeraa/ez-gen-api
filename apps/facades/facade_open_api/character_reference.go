package facade_open_api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

type CharacterReferenceSnapshot struct {
	Model                    string                  `json:"model"`
	Category                 string                  `json:"category"`
	ImageGenerationMode      string                  `json:"imageGenerationMode"`
	ReferenceStrategy        string                  `json:"referenceStrategy"`
	ReferenceAnalysisModel   string                  `json:"referenceAnalysisModel"`
	ReferenceAnalysisVersion string                  `json:"referenceAnalysisVersion"`
	ReferenceDescription     string                  `json:"referenceDescription"`
	CharacterDesign          characterdesign.Options `json:"characterDesign"`
}

type imagePreparationRepository interface {
	PrepareImage(context.Context, string, string, []byte) error
}

type ReferencePreparationError struct{ Stage string }

func (e *ReferencePreparationError) Error() string { return "reference preparation failed" }

// Freeze the final text before generating so subsequent config changes cannot alter the recorded input.
func PrepareCharacterReference(ctx context.Context, analyzer modelark.ReferenceAnalyzer, repository imagePreparationRepository, job *models.VideoGenerationJob, snapshot CharacterReferenceSnapshot, url string) (string, error) {
	if analyzer == nil || snapshot.Category != "character" || snapshot.ImageGenerationMode != "text_to_image" || snapshot.ReferenceStrategy != characterdesign.ReferenceStrategy || strings.TrimSpace(snapshot.ReferenceAnalysisModel) == "" || (snapshot.ReferenceAnalysisVersion != characterdesign.ReferenceAnalysisVersion && snapshot.ReferenceAnalysisVersion != characterdesign.LegacyReferenceAnalysisVersion) {
		return "", ErrCharacterReferenceUnavailable
	}
	analyze := analyzer.AnalyzeReference
	if snapshot.ReferenceAnalysisVersion == characterdesign.LegacyReferenceAnalysisVersion {
		if legacy, ok := analyzer.(interface {
			AnalyzeReferenceLegacy(context.Context, string, string) (string, error)
		}); ok {
			analyze = legacy.AnalyzeReferenceLegacy
		}
	}
	description, err := analyze(ctx, snapshot.ReferenceAnalysisModel, url)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	compile := characterdesign.CompileDescription
	if snapshot.ReferenceAnalysisVersion == characterdesign.LegacyReferenceAnalysisVersion {
		compile = characterdesign.CompileLegacyDescription
	}
	prompt, err := compile(job.Prompt, snapshot.CharacterDesign, description)
	if err != nil {
		return "", &ReferencePreparationError{Stage: "compile"}
	}
	var fields map[string]any
	if err := json.Unmarshal(job.EffectiveInputs, &fields); err != nil {
		return "", &ReferencePreparationError{Stage: "snapshot"}
	}
	fields["referenceDescription"] = strings.TrimSpace(description)
	prepared, err := json.Marshal(fields)
	if err != nil {
		return "", &ReferencePreparationError{Stage: "snapshot"}
	}
	if err := repository.PrepareImage(ctx, job.ID.String(), prompt, prepared); err != nil {
		return "", &ReferencePreparationError{Stage: "persistence"}
	}
	return prompt, nil
}
