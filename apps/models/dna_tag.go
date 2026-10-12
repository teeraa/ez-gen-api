package models

type TagCategory string

const (
	TagCategoryCharacter          TagCategory = "character"
	TagCategoryVisualStyle        TagCategory = "visual_style"
	TagCategoryEnvironment        TagCategory = "environment"
	TagCategoryLightingColor      TagCategory = "lighting_color"
	TagCategoryCamera             TagCategory = "camera"
	TagCategoryMotion             TagCategory = "motion"
	TagCategoryMood               TagCategory = "mood"
	TagCategoryAudio              TagCategory = "audio"
	TagCategoryQuality            TagCategory = "quality"
	TagCategoryNegativeConstraint TagCategory = "negative_constraint"
)

var AllTagCategories = []TagCategory{
	TagCategoryCharacter,
	TagCategoryVisualStyle,
	TagCategoryEnvironment,
	TagCategoryLightingColor,
	TagCategoryCamera,
	TagCategoryMotion,
	TagCategoryMood,
	TagCategoryAudio,
	TagCategoryQuality,
	TagCategoryNegativeConstraint,
}

type DNATag struct {
	Base
	Slug        string      `gorm:"uniqueIndex;not null"`
	Name        string      `gorm:"not null"`
	Category    TagCategory `gorm:"not null;index"`
	Description *string
	IsActive    bool `gorm:"not null;default:true"`
	SortOrder   int  `gorm:"not null;default:0"`
}

func (DNATag) TableName() string { return "dna_tags" }
