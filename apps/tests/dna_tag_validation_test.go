package tests

import (
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
)

func TestTagCategories_AllPresent(t *testing.T) {
	t.Parallel()
	expected := []models.TagCategory{
		models.TagCategoryCharacter,
		models.TagCategoryVisualStyle,
		models.TagCategoryEnvironment,
		models.TagCategoryLightingColor,
		models.TagCategoryCamera,
		models.TagCategoryMotion,
		models.TagCategoryMood,
		models.TagCategoryAudio,
		models.TagCategoryQuality,
		models.TagCategoryNegativeConstraint,
	}
	if len(models.AllTagCategories) != len(expected) {
		t.Errorf("expected %d tag categories, got %d", len(expected), len(models.AllTagCategories))
	}
	catSet := make(map[models.TagCategory]bool)
	for _, c := range models.AllTagCategories {
		catSet[c] = true
	}
	for _, c := range expected {
		if !catSet[c] {
			t.Errorf("missing tag category: %q", c)
		}
	}
}

func TestTagCategory_NegativeConstraint_IsDistinct(t *testing.T) {
	t.Parallel()
	neg := models.TagCategoryNegativeConstraint
	for _, c := range models.AllTagCategories {
		if c == neg {
			return
		}
	}
	t.Error("negative_constraint category not found in AllTagCategories")
}
