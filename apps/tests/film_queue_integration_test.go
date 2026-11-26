package tests

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// filmQueueDB opens a throwaway schema on the Director test database with the film job tables
// (migrations 029–044 that film_jobs needs); foreign keys to Director rows are dropped so jobs
// can be inserted directly.
func filmQueueDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DIRECTOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("DIRECTOR_TEST_DATABASE_URL unset; disposable loopback fixture only")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Port() != "55439" || u.User == nil || u.User.Username() != "director_test" || u.Path != "/postgres" {
		t.Fatal("refusing nonfixture Director database")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "film_queue_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
		if e := base.Exec("DROP SCHEMA " + schema + " CASCADE").Error; e != nil {
			t.Errorf("cleanup: %v", e)
		}
		s, _ := base.DB()
		_ = s.Close()
	})
	if err = db.Exec("CREATE TABLE users(id uuid PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"029_director.sql", "030_director_series.sql", "033_film.sql", "034_film_plan.sql",
		"035_film_export.sql", "041_film_redub.sql", "043_film_job_attempts.sql", "044_film_queue_indexes.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(string(migration)).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err = db.Exec(`ALTER TABLE film_jobs DROP CONSTRAINT IF EXISTS film_jobs_series_id_fkey,
		DROP CONSTRAINT IF EXISTS film_jobs_project_id_fkey`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func filmQueueUser(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if e := db.Exec("INSERT INTO users(id) VALUES (?)", id).Error; e != nil {
		t.Fatal(e)
	}
	return id
}

func filmQueueJob(t *testing.T, db *gorm.DB, user uuid.UUID, status string, created time.Time, lease *time.Time) models.FilmJob {
	t.Helper()
	j := models.FilmJob{ID: uuid.New(), UserID: user, SeriesID: uuid.New(), Kind: "plan", TargetID: "t", Status: status,
		Snapshot: []byte(`{"x":1}`), LeaseUntil: lease, CreatedAt: created, UpdatedAt: created}
	if status == "submitting" {
		token := uuid.New()
		j.LeaseToken = &token
	}
	if e := db.Create(&j).Error; e != nil {
		t.Fatal(e)
	}
	return j
}

func TestFilmClaimServesIdleUsersBeforeBusyOnes(t *testing.T) {
	db := filmQueueDB(t)
	store, now := repo.NewFilmRepository(db), time.Now()
	heavy, light := filmQueueUser(t, db), filmQueueUser(t, db)
	live := now.Add(10 * time.Minute)
	filmQueueJob(t, db, heavy, "submitting", now.Add(-time.Hour), &live) // heavy already has a worker
	oldest := filmQueueJob(t, db, heavy, "queued", now.Add(-50*time.Minute), nil)
	filmQueueJob(t, db, heavy, "queued", now.Add(-40*time.Minute), nil)
	newer := filmQueueJob(t, db, light, "queued", now.Add(-time.Minute), nil)

	j, e := store.Claim(context.Background())
	if e != nil || j.ID != newer.ID {
		t.Fatalf("idle user first: got %v %v, want %s", j.ID, e, newer.ID)
	}
	if j, e = store.Claim(context.Background()); e != nil || j.ID != oldest.ID {
		t.Fatalf("tie on running jobs → oldest: got %v %v, want %s", j.ID, e, oldest.ID)
	}
}

func TestFilmClaimNeverHandsOutAJobTwice(t *testing.T) {
	db := filmQueueDB(t)
	store, now := repo.NewFilmRepository(db), time.Now()
	const n = 12
	for i := 0; i < n; i++ {
		filmQueueJob(t, db, filmQueueUser(t, db), "queued", now.Add(time.Duration(i-n)*time.Second), nil)
	}
	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				j, e := store.Claim(context.Background())
				if errors.Is(e, repo.ErrNotFound) {
					return
				}
				if e != nil {
					t.Error(e)
					return
				}
				mu.Lock()
				seen[j.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("every job claimed: %d of %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("job %s claimed %d times", id, c)
		}
	}
}

func TestFilmClaimCountsCrashesAndWaitResetsThem(t *testing.T) {
	db := filmQueueDB(t)
	store, now := repo.NewFilmRepository(db), time.Now()
	expired := now.Add(-time.Minute)
	job := filmQueueJob(t, db, filmQueueUser(t, db), "submitting", now.Add(-time.Hour), &expired)

	j, e := store.Claim(context.Background())
	if e != nil || j.ID != job.ID || j.Attempts != 1 || j.Status != "submitting" {
		t.Fatalf("expired lease is one attempt: %+v %v", j, e)
	}
	if e = store.Release(context.Background(), j, "panic: boom", now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if j, e = store.Claim(context.Background()); e != nil || j.Attempts != 2 || j.LastError == nil || *j.LastError != "panic: boom" {
		t.Fatalf("released job comes back with a second attempt and its cause: %+v %v", j, e)
	}
	if e = store.Wait(context.Background(), j, nil, now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if j, e = store.Claim(context.Background()); e != nil || j.Attempts != 0 || j.Status != "waiting" {
		t.Fatalf("a parked job starts its count again: %+v %v", j, e)
	}
}

func TestFilmCleanupKeepsRecentAndActiveRows(t *testing.T) {
	db := filmQueueDB(t)
	store, now := repo.NewFilmRepository(db), time.Now()
	user := filmQueueUser(t, db)
	old, recent := now.Add(-40*24*time.Hour), now.Add(-time.Hour)
	for key, at := range map[string]time.Time{"old": old, "recent": recent} {
		if e := db.Create(&models.FilmReceipt{UserID: user, Key: key, Hash: "h", TargetID: "t", CreatedAt: at}).Error; e != nil {
			t.Fatal(e)
		}
	}
	oldDone := filmQueueJob(t, db, user, "succeeded", old, nil)
	newDone := filmQueueJob(t, db, user, "failed", recent, nil)
	oldQueued := filmQueueJob(t, db, user, "queued", old, nil)

	n, e := store.Cleanup(context.Background(), now.Add(-30*24*time.Hour))
	if e != nil || n != 2 {
		t.Fatalf("one receipt and one snapshot: n=%d %v", n, e)
	}
	var keys []string
	db.Model(&models.FilmReceipt{}).Order("key").Pluck("key", &keys)
	if strings.Join(keys, ",") != "recent" {
		t.Fatalf("receipts left: %v", keys)
	}
	for id, want := range map[uuid.UUID]string{oldDone.ID: "{}", newDone.ID: `{"x": 1}`, oldQueued.ID: `{"x": 1}`} {
		var got string
		db.Raw("SELECT snapshot::text FROM film_jobs WHERE id = ?", id).Scan(&got)
		if got != want {
			t.Fatalf("job %s snapshot %s, want %s", id, got, want)
		}
	}
}
