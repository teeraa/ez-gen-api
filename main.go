package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/crons"
	"github.com/ez-gen/ez-gen-api/apps/db"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/ez-gen/ez-gen-api/middlewares"
	"github.com/ez-gen/ez-gen-api/pkgs/configs"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

const (
	maxOutputBytes = 512 << 20
	backfillBatch  = 500
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using environment variables")
	}

	cfg := configs.Load()

	// ── Database ──────────────────────────────────────────────────────────────
	database, err := db.Connect(cfg.DatabaseURL, cfg.DatabaseMaxOpenConns, cfg.DatabaseMaxIdleConns)
	if err != nil {
		log.Fatalf("database connect: %v", err)
	}

	// ── Repositories ──────────────────────────────────────────────────────────
	tagRepo := repository_open_api.NewTagRepository(database)
	characterRepo := repository_open_api.NewCharacterProfileRepository(database)
	styleRepo := repository_open_api.NewStyleProfileRepository(database)
	sceneRepo := repository_open_api.NewSceneRepository(database)
	jobRepo := repository_open_api.NewJobRepository(database)
	uploadRepo := repository_open_api.NewUploadRepository(database)

	// ── External services ─────────────────────────────────────────────────────
	arkClient := modelark.NewClient(cfg.ArkAPIKey, cfg.ArkBaseURL)

	if cfg.StorageProvider != "supabase" {
		log.Fatalf("storage: STORAGE_PROVIDER=%q is not supported (use supabase)", cfg.StorageProvider)
	}
	storageClient, err := storage.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, cfg.StorageBucket, nil)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	mediaFetcher := storage.NewHTTPFetcher(nil, storage.ProviderHostSuffixes, maxOutputBytes)

	// ── Facades ───────────────────────────────────────────────────────────────
	dnaFacade := facade_open_api.NewDNAFacade(tagRepo, characterRepo, styleRepo, sceneRepo)
	jobFacade := facade_open_api.NewJobFacade(
		jobRepo, uploadRepo, characterRepo, styleRepo, sceneRepo, tagRepo,
		arkClient, storageClient, mediaFetcher, cfg.MaxConcurrentJobsPerUser,
	)

	// ── HTTP application ──────────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		AppName:      "VIOLET STUDIO API",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		// Never forward framework error details to the client.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			rid, _ := c.Locals("requestID").(string)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"code":      "INTERNAL_ERROR",
				"message":   "An unexpected error occurred",
				"requestId": rid,
			})
		},
	})

	// ── Global middleware ─────────────────────────────────────────────────────
	app.Use(middlewares.RequestID())
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${method} ${path} ${status} ${latency} rid=${locals:requestID}\n",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: cfg.CORSAllowedOrigins,
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Authorization, X-Request-ID",
		MaxAge:       86400,
	}))

	// ── Health ────────────────────────────────────────────────────────────────
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// ── Open API v1 (/open/v1) ────────────────────────────────────────────────
	// Auth: disabled for dev — swap DevAuth for JWTAuth(cfg.JWTSecret) when ready
	openV1 := app.Group("/open/v1", middlewares.DevAuth(cfg.DevUserID))

	// Jobs
	jobHandler := handler_open_api.NewJobHandler(jobFacade)
	jobHandler.RegisterRoutes(openV1)

	// VIOLET DNA (/open/v1/dna)
	dnaGroup := openV1.Group("/dna")
	dnaHandler := handler_open_api.NewDNAHandler(dnaFacade)
	dnaHandler.RegisterRoutes(dnaGroup)
	jobHandler.RegisterSceneRoutes(dnaGroup)

	// ── Background workers ────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pollInterval := time.Duration(cfg.ArkPollIntervalSeconds) * time.Second
	poller := crons.NewJobPoller(jobRepo, jobFacade, pollInterval, 20)
	go poller.Start(ctx)

	go func() {
		n, err := jobFacade.BackfillOutputs(ctx, backfillBatch)
		if err != nil {
			log.Printf("[storage] backfill: %v", err)
			return
		}
		log.Printf("[storage] backfill stored %d job output(s)", n)
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		port := cfg.Port
		log.Printf("VIOLET STUDIO API listening on :%s", port)
		if err := app.Listen(":" + port); err != nil {
			log.Printf("server error: %v", err)
		}
	}()

	<-quit
	cancel() // stop background workers
	log.Println("shutting down…")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
