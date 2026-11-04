package facade_open_api

import (
	"context"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"sync/atomic"
	"testing"
	"time"
)

func TestAIClaimWatcherSurvivesFailedObservation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	var calls atomic.Int32
	observed := make(chan int, 3)
	go watchAIClaim(ctx, done, cancel, func(context.Context) (bool, error) {
		n := int(calls.Add(1))
		observed <- n
		if n == 1 {
			return false, errors.New("isolated query failure")
		}
		if n == 2 {
			return true, nil
		}
		return false, nil
	})
	for i := 1; i <= 2; i++ {
		select {
		case <-observed:
		case <-time.After(4 * time.Second):
			t.Fatal("missing observation")
		}
		if ctx.Err() != nil {
			t.Fatal("database failure or live claim cancelled work")
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("definite lost claim did not cancel")
	}
}
func TestAIStatusReadErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want error }{{nil, nil}, {repo.ErrNotFound, repo.ErrNotFound}, {&pgconn.PgError{Code: "42P01"}, editorai.ErrUnavailable}, {context.DeadlineExceeded, ErrAIStatusUnavailable}, {&pgconn.PgError{Code: "08006"}, ErrAIStatusUnavailable}} {
		if got := aiStatusReadError(tc.in); !errors.Is(got, tc.want) {
			t.Fatalf("classification got %v want %v", got, tc.want)
		}
	}
}

func TestAIGetExistingStatusSkipsSchemaIntrospection(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 user=isolated dbname=isolated sslmode=disable"}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) { tx.RowsAffected = 0 }); err != nil {
		t.Fatal(err)
	}
	id, user := uuid.New(), uuid.New()
	if err = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		j, ok := tx.Statement.Dest.(*models.VideoEditorAIJob)
		if !ok {
			tx.AddError(errors.New("unexpected schema inspection"))
			return
		}
		*j = models.VideoEditorAIJob{UserID: user, Status: "failed"}
		j.ID = id
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.Callback().Row().Replace("gorm:row", func(tx *gorm.DB) {
		t.Error("schema introspection must not run")
		tx.AddError(errors.New("unexpected schema inspection"))
	}); err != nil {
		t.Fatal(err)
	}
	f := &VideoEditorAIFacade{repo: repo.NewVideoEditorAIRepository(db), options: EditorAIOptions{Enabled: true}}
	got, err := f.Get(context.Background(), id.String(), user.String())
	if err != nil || got.ID != id.String() {
		t.Fatalf("Get failed: %v", err)
	}
	f.options.Enabled = false
	if _, err = f.Get(context.Background(), id.String(), user.String()); !errors.Is(err, editorai.ErrUnavailable) {
		t.Fatalf("disabled: %v", err)
	}
}
