package models

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// VideoEditorAIChatMessage is one message of the experimental AI chat editor.
// Meta is the facade's JSON (tool trail, proposal, story job); see migration 031.
type VideoEditorAIChatMessage struct {
	Base
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID uuid.UUID `gorm:"type:uuid;not null"`
	Role      string
	Content   string
	Meta      datatypes.JSON `gorm:"type:jsonb"`
}

func (VideoEditorAIChatMessage) TableName() string { return "video_editor_ai_chat_messages" }
