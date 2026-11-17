package repository_open_api

import (
	"context"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type VideoEditorAIChatRepository struct{ db *gorm.DB }

func NewVideoEditorAIChatRepository(db *gorm.DB) *VideoEditorAIChatRepository {
	return &VideoEditorAIChatRepository{db: db}
}
func (r *VideoEditorAIChatRepository) Ready(ctx context.Context) bool {
	return r != nil && r.db != nil && r.db.WithContext(ctx).Migrator().HasTable(&models.VideoEditorAIChatMessage{})
}

// VideoEditorAIChatUsageRow is one (model, day, project) bucket of assistant replies.
type VideoEditorAIChatUsageRow struct {
	Provider     string
	Model        string
	Day          string
	ProjectID    string
	ProjectTitle string
	Replies      int64
	Steps        int64
	InputTokens  int64
	OutputTokens int64
}

// Usage sums the token usage stored in assistant replies since a time, including
// messages cleared from the thread (soft-deleted) and replies of deleted projects,
// because those tokens were still billed. Days are Asia/Bangkok calendar days.
func (r *VideoEditorAIChatRepository) Usage(ctx context.Context, user string, since time.Time) ([]VideoEditorAIChatUsageRow, error) {
	rows := []VideoEditorAIChatUsageRow{}
	e := r.db.WithContext(ctx).Raw(`SELECT COALESCE(m.meta->>'provider','') AS provider, COALESCE(m.meta->>'model','') AS model,
 to_char(m.created_at AT TIME ZONE 'Asia/Bangkok','YYYY-MM-DD') AS day, m.project_id::text AS project_id, COALESCE(p.title,'') AS project_title,
 count(*) AS replies, COALESCE(sum((m.meta->>'steps')::bigint),0) AS steps,
 COALESCE(sum((m.meta->'usage'->>'inputTokens')::bigint),0) AS input_tokens, COALESCE(sum((m.meta->'usage'->>'outputTokens')::bigint),0) AS output_tokens
 FROM video_editor_ai_chat_messages m LEFT JOIN video_editor_projects p ON p.id=m.project_id AND p.user_id=m.user_id
 WHERE m.user_id=? AND m.role='assistant' AND m.created_at>=?
 GROUP BY 1,2,3,4,5 ORDER BY 3 DESC,1,2`, user, since).Scan(&rows).Error
	return rows, e
}

// Recent returns the newest messages of a project thread, oldest first.
func (r *VideoEditorAIChatRepository) Recent(ctx context.Context, project, user string, limit int) ([]models.VideoEditorAIChatMessage, error) {
	rows := []models.VideoEditorAIChatMessage{}
	e := r.db.WithContext(ctx).Where("project_id=? AND user_id=?", project, user).Order("created_at DESC,id DESC").Limit(limit).Find(&rows).Error
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, veError(e)
}

// Clear soft-deletes every message of a project thread (deleted_at), so a reopened
// project starts a new conversation.
func (r *VideoEditorAIChatRepository) Clear(ctx context.Context, project, user string) error {
	return veError(r.db.WithContext(ctx).Where("project_id=? AND user_id=?", project, user).Delete(&models.VideoEditorAIChatMessage{}).Error)
}
func (r *VideoEditorAIChatRepository) Create(ctx context.Context, m *models.VideoEditorAIChatMessage) error {
	return veError(r.db.WithContext(ctx).Create(m).Error)
}
func (r *VideoEditorAIChatRepository) Get(ctx context.Context, project, id, user string) (*models.VideoEditorAIChatMessage, error) {
	var m models.VideoEditorAIChatMessage
	e := r.db.WithContext(ctx).Where("id=? AND project_id=? AND user_id=?", id, project, user).First(&m).Error
	return &m, veError(e)
}

// UpdateMeta replaces meta only while it still equals the version the caller read,
// so two tabs cannot both apply one proposal.
func (r *VideoEditorAIChatRepository) UpdateMeta(ctx context.Context, m *models.VideoEditorAIChatMessage, previous, meta datatypes.JSON) (bool, error) {
	res := r.db.WithContext(ctx).Model(&models.VideoEditorAIChatMessage{}).Where("id=? AND user_id=? AND meta=?::jsonb", m.ID, m.UserID, string(previous)).Update("meta", meta)
	if res.Error != nil {
		return false, veError(res.Error)
	}
	if res.RowsAffected == 1 {
		m.Meta = meta
	}
	return res.RowsAffected == 1, nil
}
