package models

import "github.com/google/uuid"

// PromptTemplate is a reusable prompt with {variable} placeholders. A nil UserID marks a built-in.
type PromptTemplate struct {
	Base
	UserID    *uuid.UUID `gorm:"type:uuid;index"`
	Title     string     `gorm:"not null"`
	Category  string     `gorm:"not null"`
	Body      string     `gorm:"not null"`
	SortOrder int        `gorm:"not null;default:0"`
}

func (PromptTemplate) TableName() string { return "prompt_templates" }

// Skill is a named, reusable workflow. A nil UserID marks a built-in.
type Skill struct {
	Base
	UserID       *uuid.UUID `gorm:"type:uuid;index"`
	ForkedFromID *uuid.UUID `gorm:"type:uuid"`
	Name         string     `gorm:"not null"`
	Description  string     `gorm:"not null;default:''"`
	Icon         string     `gorm:"not null;default:'wand'"`
	Steps        []byte     `gorm:"type:jsonb;not null"`
	Inputs       []byte     `gorm:"type:jsonb;not null"`
	Output       string     `gorm:"not null"`
	// Directive is the English guidance a film skill (Output "film") gives the film planner.
	Directive string `gorm:"not null;default:''"`
	// Incremented on each update so a future skill run can snapshot the version it used.
	Version   int `gorm:"not null;default:1"`
	SortOrder int `gorm:"not null;default:0"`
}

func (Skill) TableName() string { return "skills" }
