package repository_open_api

import (
	"context"
	"errors"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAIClaimObservationDistinguishesFailureFromCancellation(t *testing.T) {
	t.Parallel()
	failure := errors.New("isolated query failure")
	for _, tc := range []struct {
		name     string
		count    int64
		queryErr error
	}{
		{"live", 1, nil}, {"cancelled", 0, nil}, {"database unavailable", 0, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 user=isolated dbname=isolated sslmode=disable"}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				if tc.queryErr != nil {
					tx.AddError(tc.queryErr)
					return
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Error("unexpected count destination")
					return
				}
				*count = tc.count
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			repo := NewVideoEditorAIRepository(db)
			live, err := repo.ClaimLive(context.Background(), &models.VideoEditorAIJob{})
			if live != (tc.count == 1) || !errors.Is(err, tc.queryErr) {
				t.Fatalf("live=%v error=%v", live, err)
			}
			if tc.queryErr != nil && repo.Live(context.Background(), &models.VideoEditorAIJob{}) {
				t.Fatal("legacy fencing must remain fail closed")
			}
		})
	}
}
