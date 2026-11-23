package main

import (
	"context"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/models"
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
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/ez-gen/ez-gen-api/apps/services/gemini"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/seedaudio"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/ez-gen/ez-gen-api/middlewares"
	"github.com/ez-gen/ez-gen-api/pkgs/configs"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

const (
	apiPrefix    = "/api/v1"
	publicPrefix = apiPrefix + "/public"

	maxOutputBytes = 512 << 20
	backfillBatch  = 500
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using environment variables")
	}

	cfg := configs.Load()
	if len(cfg.JWTSecret) < 32 || cfg.JWTSecret == "replace-with-a-long-random-secret" || cfg.JWTExpiryHours < 1 || cfg.JWTExpiryHours > 168 {
		log.Fatal("JWT_SECRET must be a random secret of at least 32 bytes")
	}

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
	shareRepo := repository_open_api.NewShareRepository(database)
	showcaseRepo := repository_open_api.NewShowcaseRepository(database)
	exportRepo := repository_open_api.NewExportRepository(database)

	// ── External services ─────────────────────────────────────────────────────
	usageRepo := repository_open_api.NewAIUsageRepository(database)
	recordUsage := func(ctx context.Context, event modelark.UsageEvent) error {
		var taskID *string
		if event.ProviderTaskID != "" {
			taskID = &event.ProviderTaskID
		}
		return usageRepo.Record(ctx, &models.AIUsage{ID: event.AttemptID, UserID: event.UserID, JobID: event.JobID, Provider: event.Provider, Model: event.Model, Operation: event.Operation, ProviderTaskID: taskID, Status: event.Status, StartedAt: event.StartedAt, CompletedAt: event.CompletedAt, LatencyMS: event.LatencyMS, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens, TotalTokens: event.TotalTokens, GeneratedImages: event.GeneratedImages, DurationSeconds: event.DurationSeconds})
	}
	arkClient := modelark.NewClientWithUsage(cfg.ArkAPIKey, cfg.ArkBaseURL, recordUsage)
	modelRegistry, err := modelark.NewRegistry(cfg.ArkDefaultModel)
	if err != nil {
		log.Fatalf("ARK_DEFAULT_MODEL: %v", err)
	}
	for id, provider := range modelRegistry.Routes() {
		log.Printf("[ark] model %s -> %s", id, provider)
	}

	if cfg.StorageProvider != "supabase" {
		log.Fatalf("storage: STORAGE_PROVIDER=%q is not supported (use supabase)", cfg.StorageProvider)
	}
	legacyStorage, err := storage.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, cfg.StorageBucket, nil)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	imageStorage, err := storage.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, cfg.StorageImageBucket, nil)
	if err != nil {
		log.Fatal("storage: character image bucket configuration is required")
	}
	storageClient := storage.NewBucketRouter(legacyStorage, imageStorage)
	mediaFetcher := storage.NewHTTPFetcher(nil, storage.ProviderHostSuffixes, maxOutputBytes)

	// ── Facades ───────────────────────────────────────────────────────────────
	dnaFacade := facade_open_api.NewDNAFacade(tagRepo, characterRepo, styleRepo, sceneRepo, jobRepo, storageClient)
	jobFacade := facade_open_api.NewJobFacade(
		jobRepo, uploadRepo, characterRepo, styleRepo, sceneRepo, tagRepo,
		arkClient, modelRegistry, storageClient, mediaFetcher, cfg.MaxConcurrentJobsPerUser,
	)

	shareFacade := facade_open_api.NewShareFacade(shareRepo, jobRepo, storageClient, cfg.PublicWebBaseURL)

	videoProcessor := videoprocessing.NewFFmpegProcessor(cfg.FFmpegFontFile)
	exportFacade := facade_open_api.NewExportFacade(exportRepo, jobRepo, uploadRepo, sceneRepo, storageClient, videoProcessor)
	uploadFacade := facade_open_api.NewUploadFacade(uploadRepo, storageClient)

	// ── AI point charging (shared wallet with Director/Film) ──────────────────
	if cfg.AIPointsEnabled {
		facade_open_api.ConfigurePoints(repository_open_api.NewPointWallet(database), facade_open_api.PointPrices{
			ChatMessage:    cfg.AIChatMessagePoints,
			Video:          cfg.AIVideoPoints,
			VideoDraft:     cfg.AIVideoDraftPoints,
			Image:          cfg.AIImagePoints,
			StoryAnalysis:  cfg.AIStoryAnalysisPoints,
			EditorAnalysis: cfg.AIEditorAnalysisPoints,
			EditorEdit:     cfg.AIEditorEditPoints,
			Voice:          cfg.AIVoicePoints,
			TextSuggestion: cfg.AITextSuggestionPoints,
		})
	}

	// ── HTTP application ──────────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		DisablePreParseMultipartForm: true,
		AppName:                      "VIOLET STUDIO API",
		BodyLimit:                    55 << 20, // Existing upload contract allows 50 MiB audio plus multipart overhead.
		ReadTimeout:                  60 * time.Second,
		WriteTimeout:                 150 * time.Second,
		// Never forward framework error details to the client.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var fe *fiber.Error
			if errors.As(err, &fe) {
				switch fe.Code {
				case fiber.StatusRequestEntityTooLarge:
					return utils.Error(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "file exceeds size limit", nil)
				case fiber.StatusNotFound:
					return utils.Error(c, fiber.StatusNotFound, "NOT_FOUND", "Resource not found", nil)
				case fiber.StatusMethodNotAllowed:
					return utils.Error(c, fiber.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
				}
			}
			return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
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
		return utils.Success(c, fiber.StatusOK, fiber.Map{"status": "ok"})
	})

	// ── Public API (/api/v1/public) — no auth, rate limited per client IP ───────
	shareHandler := handler_open_api.NewShareHandler(shareFacade)
	publicV1 := app.Group(publicPrefix, limiter.New(limiter.Config{
		Max:        cfg.PublicRateLimitPerMinute,
		Expiration: time.Minute,
		LimitReached: func(c *fiber.Ctx) error {
			return utils.Error(c, fiber.StatusTooManyRequests, "RATE_LIMITED", "Too many requests", nil)
		},
	}))
	shareHandler.RegisterPublicRoutes(publicV1)
	handler_open_api.NewShowcaseHandler(facade_open_api.NewShowcaseFacade(showcaseRepo, storageClient)).RegisterPublicRoutes(publicV1)

	// ── API v1 (/api/v1) ──────────────────────────────────────────────────────
	// Public routes above are registered first, so they answer before JWTAuth runs.
	// Local-account JWT authentication is required for all owned resources.
	studioRepo := repository_open_api.NewStudioRepository(database)
	imageEnabled := cfg.ArkImageAPIKey != "" && cfg.ArkImageModel != ""
	imageModel := ""
	if imageEnabled {
		imageModel = cfg.ArkImageModel
	}
	studioFacade := facade_open_api.NewStudioFacade(studioRepo, jobRepo, uploadRepo, storageClient, cfg.JWTSecret, time.Duration(cfg.JWTExpiryHours)*time.Hour, imageModel, cfg.MaxConcurrentJobsPerUser).WithReferenceAnalysisModel(cfg.ArkReferenceAnalysisModel)
	studioHandler := handler_open_api.NewStudioHandler(studioFacade, cfg.RateLimitJobsPerMinute)
	studioHandler.RegisterAuth(app.Group(apiPrefix))
	apiV1 := app.Group(apiPrefix, middlewares.JWTAuth(cfg.JWTSecret))
	studioHandler.RegisterRoutes(apiV1)
	facade_open_api.ConfigureJobPipeline(jobFacade, studioRepo)
	pipelineRepo := repository_open_api.NewPipelineRepository(database, cfg.MaxConcurrentJobsPerUser)
	pipelineFacade := facade_open_api.NewPipelineFacade(pipelineRepo, studioFacade, jobFacade, jobRepo, studioRepo, uploadRepo, modelRegistry, storageClient, cfg.ArkAPIKey != "")
	handler_open_api.NewPipelineHandler(pipelineFacade, cfg.RateLimitJobsPerMinute).RegisterRoutes(apiV1)
	productionRepo := repository_open_api.NewProductionRepository(database)
	productionFacade := facade_open_api.NewProductionFacade(productionRepo, sceneRepo, jobRepo, uploadRepo, dnaFacade, jobFacade, modelRegistry, cfg.ArkAPIKey != "")
	facade_open_api.ConfigureDNAProduction(dnaFacade, productionFacade)
	facade_open_api.ConfigureJobProduction(jobFacade, productionFacade)
	facade_open_api.ConfigureExportProduction(exportFacade, productionRepo)
	productionFacade.ConfigureProcessor(videoProcessor)
	handler_open_api.NewProductionHandler(productionFacade).RegisterRoutes(apiV1)
	voiceClient := seedaudio.New(cfg.VoiceAPIKey, cfg.VoiceBaseURL)
	videoEditorFacade := facade_open_api.NewVideoEditorFacade(repository_open_api.NewVideoEditorRepository(database), storageClient, videoProcessor).WithMaxSourceBytes(cfg.EditorMaxSourceMB << 20).WithVoice(voiceClient)
	handler_open_api.NewVideoEditorHandler(videoEditorFacade).RegisterRoutes(apiV1)
	// Voice menu (/api/v1/voice): text-to-speech with seed-audio-1.0 and a saved history.
	voiceFacade := facade_open_api.NewVoiceFacade(repository_open_api.NewVoiceRepository(database), storageClient, voiceClient)
	handler_open_api.NewVoiceHandler(voiceFacade, 10).RegisterRoutes(apiV1)
	// Characters (library assets, film elements) can be bound to a Voice-menu voice.
	studioFacade.WithCharacterVoices(voiceFacade)
	editorTranscriber := editorai.SelectTranscriber(cfg.EditorASRProvider, &editorai.LocalASR{Python: cfg.WhisperPython, Runner: cfg.WhisperRunner, Model: cfg.WhisperModelPath, Version: cfg.WhisperModelVersion, Device: cfg.WhisperDevice, Compute: cfg.WhisperComputeType, Language: cfg.WhisperLanguage}, &editorai.TyphoonASR{Python: cfg.TyphoonPython, Runner: cfg.TyphoonRunner, Model: cfg.TyphoonModelPath, Version: cfg.TyphoonModelVersion})
	editorAIFacade := facade_open_api.NewVideoEditorAIFacade(repository_open_api.NewVideoEditorAIRepository(database), videoEditorFacade, editorai.NewGemini(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.GeminiModelVersion), editorTranscriber, facade_open_api.EditorAIOptions{Enabled: cfg.EditorAIEnabled})
	handler_open_api.NewVideoEditorAIHandler(editorAIFacade).RegisterRoutes(apiV1)
	chatCaller := editorai.SelectToolCaller(cfg.EditorAIChatProvider, editorai.NewGemini(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.GeminiModelVersion), editorai.NewClaude(cfg.AnthropicAPIKey, cfg.ClaudeModel))
	// EDITOR_AI_CHAT_MODELS lets the user pick a model per message; without it the single
	// provider above is the only choice. That provider stays the default when listed.
	chatCallers := editorai.ParseChatModels(cfg.EditorAIChatModels, cfg.GeminiAPIKey, cfg.GeminiModelVersion, cfg.AnthropicAPIKey)
	handler_open_api.NewVideoEditorAIChatHandler(facade_open_api.NewVideoEditorAIChatFacade(editorAIFacade, repository_open_api.NewVideoEditorAIChatRepository(database), chatCallers, chatCaller, cfg.EditorAIChatEnabled)).RegisterRoutes(apiV1)
	storyFacade := facade_open_api.NewStoryFlowFacade(repository_open_api.NewStoryFlowRepository(database, cfg.MaxConcurrentJobsPerUser), studioRepo, modelRegistry, modelark.NewImageClient(cfg.ArkImageAPIKey, cfg.ArkImageBaseURL, cfg.ArkImageModel), modelark.NewStoryFlowAnalyzer(cfg.ArkStoryAnalysisAPIKey, cfg.ArkStoryAnalysisBaseURL), modelark.NewClient(cfg.ArkAPIKey, cfg.ArkBaseURL), storageClient, mediaFetcher, videoProcessor, facade_open_api.StoryFlowOptions{AnalysisModel: cfg.ArkStoryAnalysisModel, ImageModel: imageModel, Enabled: cfg.ArkAPIKey != "" && cfg.ArkStoryAnalysisAPIKey != "", ExportAvailable: productionFacade.Capabilities().Export.Available})
	handler_open_api.NewStoryFlowHandler(storyFacade, cfg.RateLimitJobsPerMinute).RegisterRoutes(apiV1)
	directorAnalyzer, directorAnalysisModel, directorStoryReady := directorStoryPlanner(cfg)
	directorFacade := facade_open_api.NewDirectorFacade(repository_open_api.NewDirectorRepository(database, cfg.MaxConcurrentJobsPerUser).SetMaxParallel(cfg.DirectorProjectParallel), studioRepo, storageClient, directorAnalyzer, modelark.NewImageClient(cfg.ArkImageAPIKey, cfg.ArkImageBaseURL, cfg.ArkImageModel), modelRegistry, modelark.NewClient(cfg.ArkAPIKey, cfg.ArkBaseURL), mediaFetcher, facade_open_api.DirectorOptions{Enabled: imageEnabled && directorStoryReady, AnalysisModel: directorAnalysisModel, ImageModel: imageModel, StoryPoints: cfg.DirectorStoryPoints, ImagePoints: cfg.DirectorImagePoints, VideoPoints: cfg.DirectorVideoPoints, DraftPoints: cfg.DirectorDraftPoints, MaxImageReferences: cfg.DirectorImageRefs, VideoHumanFaceMode: cfg.DirectorFaceMode, SampleBaseURL: cfg.DirectorSampleBaseURL, VideoEnabled: cfg.ArkAPIKey != "", ReferenceCheck: storage.Reachable, LookDescriber: directorLookDescriber(directorAnalyzer), Processor: videoProcessor, VideoFetch: func(c context.Context, u string) ([]byte, string, error) { return storage.FetchVideo(c, u, 300<<20) }, ImageFetch: func(c context.Context, u string) ([]byte, string, error) { return storage.FetchImage(c, u, 7<<20) }})
	handler_open_api.NewDirectorHandler(directorFacade, cfg.RateLimitJobsPerMinute).RegisterRoutes(apiV1)
	// ภาพยนตร์ (film menu): separate tables, routes and worker; shares the point wallet and prices.
	filmFacade := facade_open_api.NewFilmFacade(repository_open_api.NewFilmRepository(database), storageClient, modelark.NewImageClient(cfg.ArkImageAPIKey, cfg.ArkImageBaseURL, cfg.ArkImageModel), modelark.NewClient(cfg.ArkAPIKey, cfg.ArkBaseURL), mediaFetcher, facade_open_api.FilmOptions{Enabled: imageEnabled, ImageModel: imageModel, ImagePoints: cfg.DirectorImagePoints, VideoPoints: cfg.DirectorVideoPoints, DraftPoints: cfg.DirectorDraftPoints, VideoEnabled: cfg.ArkAPIKey != "", ReferenceCheck: storage.Reachable, Processor: videoProcessor, Analyzer: directorAnalyzer, AnalysisModel: directorAnalysisModel, PlanPoints: cfg.DirectorStoryPoints, VoiceFirst: true, VideoFetch: func(c context.Context, u string) ([]byte, string, error) { return storage.FetchVideo(c, u, 300<<20) }, MusicFetch: func(c context.Context, u string) ([]byte, string, error) {
		return storage.FetchAudio(c, u, facade_open_api.FilmMusicMaxBytes)
	}})
	filmFacade.WithCharacterVoices(voiceFacade)
	handler_open_api.NewFilmHandler(filmFacade, cfg.RateLimitJobsPerMinute).RegisterRoutes(apiV1)

	// Jobs
	jobHandler := handler_open_api.NewJobHandler(jobFacade)
	jobHandler.RegisterRoutes(apiV1)
	shareHandler.RegisterRoutes(apiV1)

	// Uploads
	uploadHandler := handler_open_api.NewUploadHandler(uploadFacade)
	uploadHandler.RegisterRoutes(apiV1)

	// Storyboard exports
	exportHandler := handler_open_api.NewExportHandler(exportFacade)
	exportHandler.RegisterRoutes(apiV1)

	// VIOLET DNA (/api/v1/dna)
	dnaGroup := apiV1.Group("/dna")
	dnaHandler := handler_open_api.NewDNAHandler(dnaFacade)
	dnaHandler.RegisterRoutes(dnaGroup)
	jobHandler.RegisterSceneRoutes(dnaGroup)

	// Prompt library (/api/v1/prompts) and Skills (/api/v1/skills) menus
	handler_open_api.NewLibraryHandler(facade_open_api.NewLibraryFacade(repository_open_api.NewLibraryRepository(database))).RegisterRoutes(apiV1)

	// Marketplace menu (/api/v1/marketplace). Manual phase: posts and sales are logged by the user,
	// no platform API is called. AI suggestions reuse the Director story planner's text model.
	marketplaceOptions := facade_open_api.MarketplaceOptions{}
	if directorStoryReady {
		marketplaceOptions = facade_open_api.MarketplaceOptions{Analyzer: directorAnalyzer, AnalysisModel: directorAnalysisModel}
	}
	handler_open_api.NewMarketplaceHandler(facade_open_api.NewMarketplaceFacade(repository_open_api.NewMarketplaceRepository(database), uploadRepo, storageClient, marketplaceOptions), 6).RegisterRoutes(apiV1)

	// ── Background workers ────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go crons.NewVideoEditorWorker(videoEditorFacade, 2*time.Second).Start(ctx)
	go crons.NewVideoEditorAIWorker(editorAIFacade, 2*time.Second).Start(ctx)
	if imageEnabled {
		imageWorker := crons.NewImageWorker(studioRepo, uploadRepo, modelark.NewImageClientWithUsage(cfg.ArkImageAPIKey, cfg.ArkImageBaseURL, cfg.ArkImageModel, recordUsage), storageClient).WithReferenceAnalyzer(modelark.NewReferenceAnalyzerWithUsage(cfg.ArkReferenceAnalysisAPIKey, cfg.ArkReferenceAnalysisBaseURL, recordUsage))
		go imageWorker.Start(ctx)
	}

	pollInterval := time.Duration(cfg.ArkPollIntervalSeconds) * time.Second
	go crons.NewPipelineWorker(pipelineFacade).Start(ctx)
	go crons.NewStoryFlowWorker(storyFacade).Start(ctx)
	go crons.NewDirectorWorker(directorFacade, cfg.DirectorWorkers).Start(ctx)
	go crons.NewFilmWorker(filmFacade, 2).Start(ctx)
	poller := crons.NewJobPoller(jobRepo, jobFacade, pollInterval, 20)
	go poller.Start(ctx)

	exportWorker := crons.NewExportWorker(exportRepo, exportFacade, 10*time.Second, 5)
	go exportWorker.Start(ctx)

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

// Gemini is used when explicitly selected, or when no ModelArk text model is configured
// but a Gemini key is, so a development environment can plan stories without an Ark text endpoint.
func directorStoryPlanner(cfg *configs.Config) (modelark.StoryFlowAnalyzer, string, bool) {
	provider := cfg.DirectorStoryProvider
	if provider == "" && cfg.ArkStoryAnalysisModel == "" && cfg.GeminiAPIKey != "" {
		provider = "gemini"
	}
	if provider == "gemini" {
		log.Printf("director: story planner provider=gemini model=%s", cfg.GeminiModel)
		return gemini.NewStoryAnalyzer(cfg.GeminiAPIKey), cfg.GeminiModel, cfg.GeminiAPIKey != "" && cfg.GeminiModel != ""
	}
	log.Printf("director: story planner provider=modelark model=%s", cfg.ArkStoryAnalysisModel)
	return modelark.NewStoryFlowAnalyzer(cfg.ArkStoryAnalysisAPIKey, cfg.ArkStoryAnalysisBaseURL), cfg.ArkStoryAnalysisModel, cfg.ArkStoryAnalysisAPIKey != "" && cfg.ArkStoryAnalysisModel != ""
}

// Only the Gemini planner can read images; with the ModelArk text planner, looks fall back to library text.
func directorLookDescriber(a modelark.StoryFlowAnalyzer) facade_open_api.DirectorLookDescriber {
	if g, ok := a.(*gemini.StoryAnalyzer); ok {
		return g
	}
	return nil
}
