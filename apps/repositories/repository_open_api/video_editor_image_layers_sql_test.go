package repository_open_api

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// image_layers exists only after migration 028: creating a project must never name it,
// while SaveProject's Updates map must write it (a read-only tag silently dropped it).
func TestVideoEditorImageLayersColumnPermissions(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	p := models.VideoEditorProject{UserID: uuid.New(), Title: "t", ImageLayers: datatypes.JSON("[]")}
	create := db.ToSQL(func(tx *gorm.DB) *gorm.DB { return tx.Create(&p) })
	if strings.Contains(create, "image_layers") {
		t.Fatalf("create names image_layers before migration 028 can be assumed: %s", create)
	}
	update := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&models.VideoEditorProject{}).Where("id=?", uuid.New()).Updates(map[string]any{"title": "x", "image_layers": datatypes.JSON("[1]")})
	})
	if !strings.Contains(update, `"image_layers"='[1]'`) {
		t.Fatalf("save dropped image_layers: %s", update)
	}
}
