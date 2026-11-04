package handler_open_api

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type projectErrorConnector struct{ err error }

func (c projectErrorConnector) Connect(context.Context) (driver.Conn, error) {
	return projectErrorConn{c.err}, nil
}
func (c projectErrorConnector) Driver() driver.Driver { return projectErrorDriver{} }

type projectErrorDriver struct{}

func (projectErrorDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type projectErrorConn struct{ err error }

func (c projectErrorConn) Prepare(string) (driver.Stmt, error) { return nil, c.err }
func (c projectErrorConn) Close() error                        { return nil }
func (c projectErrorConn) Begin() (driver.Tx, error)           { return projectErrorTx{}, nil }
func (c projectErrorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, c.err
}
func (c projectErrorConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, c.err
}

type projectErrorTx struct{}

func (projectErrorTx) Commit() error   { return nil }
func (projectErrorTx) Rollback() error { return nil }

func TestProjectErrorsProduceSafeDiagnostic(t *testing.T) {
	for _, tc := range []struct{ code, category string }{{"42P01", "database"}, {"42703", "database"}, {"42501", "database"}, {"08006", "connection"}, {"bad\nstate", "internal"}} {
		for _, method := range []string{"GET", "POST"} {
			t.Run(tc.code+method, func(t *testing.T) {
				injected := &pgconn.PgError{Code: tc.code, Message: "secret-password", Detail: "private prompt", InternalQuery: "SELECT secret FROM users"}
				conn := sql.OpenDB(projectErrorConnector{fmt.Errorf("wrapped unsafe DSN: %w", injected)})
				defer conn.Close()
				db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
				if err != nil {
					t.Fatal(err)
				}
				registry, _ := modelark.NewRegistry("")
				h := NewProductionHandler(f.NewProductionFacade(repo.NewProductionRepository(db), nil, nil, nil, nil, nil, registry, false))
				app := fiber.New()
				app.Use(func(c *fiber.Ctx) error {
					c.Locals("userID", "11111111-1111-4111-8111-111111111111")
					c.Locals("requestID", "2270ed06-863e-4054-abd2-3e050c06af6e")
					return c.Next()
				})
				h.RegisterRoutes(app)
				var captured bytes.Buffer
				oldWriter, oldFlags := log.Writer(), log.Flags()
				log.SetOutput(&captured)
				log.SetFlags(0)
				defer func() { log.SetOutput(oldWriter); log.SetFlags(oldFlags) }()
				request := httptest.NewRequest(method, "/projects", strings.NewReader(`{"brief":"test project"}`))
				request.Header.Set("Content-Type", "application/json")
				response, err := app.Test(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, _ := io.ReadAll(response.Body)
				if response.StatusCode != 500 || !bytes.Contains(body, []byte(`"code":"INTERNAL_ERROR"`)) {
					t.Fatalf("response %d %s", response.StatusCode, body)
				}
				operation := "projects.list"
				if method == "POST" {
					operation = "projects.create"
				}
				for _, want := range []string{"operation=" + operation, "category=" + tc.category, `requestId="2270ed06-863e-4054-abd2-3e050c06af6e"`} {
					if !strings.Contains(captured.String(), want) {
						t.Errorf("missing %q diagnostic: %s", want, captured.String())
					}
				}
				if tc.category != "internal" && !strings.Contains(captured.String(), "sqlstate="+tc.code) {
					t.Errorf("missing SQLSTATE: %s", captured.String())
				}
				for _, unsafe := range []string{"secret-password", "private prompt", "SELECT secret", "unsafe DSN", "bad\nstate"} {
					if strings.Contains(captured.String(), unsafe) || strings.Contains(string(body), unsafe) {
						t.Errorf("unsafe content disclosed: %q", unsafe)
					}
				}
			})
		}
	}
}

func TestProductionDiagnosticBoundsAndQuotesRequestID(t *testing.T) {
	var captured bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&captured)
	defer log.SetOutput(previous)
	app := fiber.New()
	app.Get("/projects", func(c *fiber.Ctx) error {
		c.Locals("requestID", "\nforged-entry="+strings.Repeat("x", 200))
		reportProductionError(c, "projects.list", errors.New("secret-password"))
		return c.SendStatus(500)
	})
	response, err := app.Test(httptest.NewRequest("GET", "/projects", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if strings.Count(captured.String(), "\n") != 1 || strings.Contains(captured.String(), strings.Repeat("x", 65)) || strings.Contains(captured.String(), "secret-password") {
		t.Fatalf("unsafe diagnostic: %q", captured.String())
	}
}
func TestProductionErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err      error
		category string
	}{{context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"}, {sql.ErrConnDone, "connection"}} {
		category, state := productionErrorClassification(fmt.Errorf("private detail: %w", tc.err))
		if category != tc.category || state != "none" {
			t.Errorf("got %s/%s, want %s/none", category, state, tc.category)
		}
	}
}
