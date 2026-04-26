package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/hecate/agent-runtime/internal/api"
	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/bootstrap"
	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/catalog"
	"github.com/hecate/agent-runtime/internal/chatstate"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/orchestrator"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/retention"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/secrets"
	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/internal/taskstate"
	"github.com/hecate/agent-runtime/internal/telemetry"
)

func main() {
	cfg := config.LoadFromEnv()

	// Resolve the auto-generated bootstrap secrets (control-plane encryption
	// key and admin bearer token). Env values win when set; otherwise the
	// values are loaded from the bootstrap file under DataDir, generating
	// fresh ones on first run. We do this before logger init so we can
	// loud-log the admin token through the same structured logger that the
	// rest of startup uses.
	bootstrapPath := cfg.Server.BootstrapFile
	if bootstrapPath == "" {
		bootstrapPath = filepath.Join(cfg.Server.DataDir, "hecate.bootstrap.json")
	}
	boot, printAdminToken, err := bootstrap.Resolve(bootstrapPath, cfg.Server.ControlPlaneSecretKey, cfg.Server.AuthToken)
	if err != nil {
		slog.Error("bootstrap secrets init failed", slog.String("path", bootstrapPath), slog.Any("error", err))
		os.Exit(1)
	}
	cfg.Server.ControlPlaneSecretKey = boot.ControlPlaneSecretKey
	cfg.Server.AuthToken = boot.AdminToken

	otelResource, err := telemetry.BuildResource(context.Background(), telemetry.ResourceOptions{
		ServiceName:       cfg.OTel.ServiceName,
		ServiceVersion:    cfg.OTel.ServiceVersion,
		ServiceInstanceID: cfg.OTel.ServiceInstanceID,
		DeploymentEnv:     cfg.OTel.DeploymentEnvironment,
	})
	if err != nil {
		slog.Error("otel resource init failed", slog.Any("error", err))
		os.Exit(1)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger, shutdownLogs, err := telemetry.NewLoggerWithOTLP(context.Background(), cfg.LogLevel, telemetry.OTelLogOptions{
		Enabled:  cfg.OTel.Logs.Enabled,
		Endpoint: firstNonEmpty(cfg.OTel.Logs.Endpoint, cfg.OTel.Traces.Endpoint),
		Headers:  firstNonEmptyMap(cfg.OTel.Logs.Headers, cfg.OTel.Traces.Headers),
		Resource: otelResource,
		Timeout:  firstNonZeroDuration(cfg.OTel.Logs.Timeout, cfg.OTel.Traces.Timeout),
	})
	if err != nil {
		slog.Error("otel logger init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownLogs(shutdownCtx); err != nil {
			logger.Warn("otel logger shutdown failed", slog.Any("error", err))
		}
	}()
	meterProvider, shutdownMetrics, err := telemetry.NewMeterProvider(context.Background(), telemetry.OTelMetricOptions{
		Enabled:  cfg.OTel.Metrics.Enabled,
		Endpoint: cfg.OTel.Metrics.Endpoint,
		Headers:  cfg.OTel.Metrics.Headers,
		Resource: otelResource,
		Timeout:  cfg.OTel.Metrics.Timeout,
		Interval: cfg.OTel.MetricsInterval,
	})
	if err != nil {
		slog.Error("otel meter provider init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownMetrics(shutdownCtx); err != nil {
			logger.Warn("otel meter provider shutdown failed", slog.Any("error", err))
		}
	}()
	otel.SetMeterProvider(meterProvider)
	metrics, err := telemetry.NewMetricsWithMeterProvider(meterProvider)
	if err != nil {
		logger.Error("otel metrics init failed", slog.Any("error", err))
		os.Exit(1)
	}
	postgresClient := buildPostgresClient(cfg, logger)
	if postgresClient != nil {
		defer func() {
			if err := postgresClient.Close(); err != nil {
				logger.Warn("postgres close failed", slog.Any("error", err))
			}
		}()
	}

	controlPlaneStore := buildControlPlaneStore(cfg, logger, postgresClient)
	var secretCipher secrets.Cipher
	if strings.TrimSpace(cfg.Server.ControlPlaneSecretKey) != "" {
		cipherImpl, err := secrets.NewAESGCMCipher(cfg.Server.ControlPlaneSecretKey)
		if err != nil {
			logger.Error("control plane secret cipher init failed", slog.Any("error", err))
			os.Exit(1)
		}
		secretCipher = cipherImpl
	}

	providerRuntime := providers.NewControlPlaneRuntimeManager(logger, cfg.Providers.OpenAICompatible, controlPlaneStore, secretCipher)
	if err := providerRuntime.Reload(context.Background()); err != nil {
		logger.Error("provider runtime reload failed", slog.Any("error", err))
		os.Exit(1)
	}
	providerRegistry := providerRuntime.Registry()
	healthTracker := providers.NewMemoryHealthTracker(cfg.Provider.HealthThreshold, cfg.Provider.HealthCooldown)

	staticPricebook := billing.NewStaticPricebook(cfg.Providers, cfg.Pricebook)
	pricebook := billing.NewRegistryAwarePricebook(billing.NewControlPlanePricebook(staticPricebook, controlPlaneStore), providerRegistry)
	otelProvider, err := profiler.NewTracerProvider(context.Background(), profiler.TracerProviderOptions{
		Enabled:  cfg.OTel.Traces.Enabled,
		Endpoint: cfg.OTel.Traces.Endpoint,
		Headers:  cfg.OTel.Traces.Headers,
		Timeout:  cfg.OTel.Traces.Timeout,
		Resource: otelResource,
		Sampler:  telemetry.BuildSampler(cfg.OTel.TracesSampler, cfg.OTel.TracesSamplerArg),
	})
	if err != nil {
		logger.Error("otel tracer provider init failed", slog.Any("error", err))
		os.Exit(1)
	}
	otel.SetTracerProvider(otelProvider)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := otelProvider.Shutdown(shutdownCtx); err != nil {
			logger.Warn("otel tracer provider shutdown failed", slog.Any("error", err))
		}
	}()
	tracer := profiler.NewInMemoryTracer(profiler.NewOTelTracer(otelProvider))
	cacheStore := buildCacheStore(cfg, logger, postgresClient)
	semanticStore := buildSemanticStore(cfg, logger, postgresClient)
	budgetStore := buildBudgetStore(cfg, logger, postgresClient)
	chatSessionStore := buildChatSessionStore(cfg, logger, postgresClient)
	retentionHistoryStore := buildRetentionHistoryStore(cfg, logger, postgresClient)
	retentionManager := retention.NewManager(
		logger,
		cfg.Retention,
		tracer,
		tracer,
		budgetStore,
		controlPlaneStore,
		pruneableExactCache(cacheStore),
		pruneableSemanticCache(semanticStore),
		retentionHistoryStore,
	)
	providerCatalog := catalog.NewRegistryCatalogWithSelfAddr(providerRegistry, healthTracker, cfg.Server.Address)
	routerEngine := router.NewRuleRouter(
		cfg.Router.DefaultModel,
		providerCatalog,
	)
	governorEngine := governor.NewControlPlaneGovernor(cfg.Governor, budgetStore, budgetStore, controlPlaneStore)

	service := gateway.NewService(buildGatewayDependencies(
		cfg,
		logger,
		cacheStore,
		semanticStore,
		routerEngine,
		providerCatalog,
		governorEngine,
		providerRegistry,
		healthTracker,
		pricebook,
		tracer,
		metrics,
		retentionManager,
		chatSessionStore,
	))

	retentionCtx, retentionCancel := context.WithCancel(context.Background())
	defer retentionCancel()
	go retentionManager.RunLoop(retentionCtx)

	taskStore := buildTaskStore(cfg, logger, postgresClient)
	taskQueue := buildTaskQueue(cfg, logger, postgresClient)
	handler := api.NewHandler(cfg, logger, service, controlPlaneStore, taskStore, taskQueue, providerRuntime)
	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           api.NewServer(logger, handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		// Loud-print the admin token on first run. Subsequent runs (token
		// loaded from disk) skip this — operators read it from the
		// bootstrap file. We bypass the structured logger here so the token
		// shows up on a plain TTY/`docker compose logs` even when the
		// JSON-shaped logger output would otherwise hide it from skimming.
		if printAdminToken {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "============================================================")
			fmt.Fprintln(os.Stderr, "  Hecate first-run setup — admin bearer token generated.")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "    "+cfg.Server.AuthToken)
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "  Saved to "+bootstrapPath+" (mode 0600).")
			fmt.Fprintln(os.Stderr, "  Use it as a Bearer token on /admin endpoints, or paste it")
			fmt.Fprintln(os.Stderr, "  into the UI on first load.")
			fmt.Fprintln(os.Stderr, "============================================================")
			fmt.Fprintln(os.Stderr, "")
		}
		logger.Info("gateway starting",
			slog.String("addr", cfg.Server.Address),
			slog.String("default_model", cfg.Router.DefaultModel),
			slog.String("cache_backend", cfg.Cache.Backend),
			slog.Bool("semantic_cache_enabled", cfg.Cache.Semantic.Enabled),
			slog.String("semantic_cache_backend", cfg.Cache.Semantic.Backend),
			slog.Int("provider_max_attempts", cfg.Provider.MaxAttempts),
			slog.Bool("provider_failover_enabled", cfg.Provider.FailoverEnabled),
			slog.Int("provider_health_failure_threshold", cfg.Provider.HealthThreshold),
			slog.Duration("provider_health_cooldown", cfg.Provider.HealthCooldown),
			slog.Bool("retention_enabled", cfg.Retention.Enabled),
			slog.Duration("retention_interval", cfg.Retention.Interval),
			slog.Bool("otel_traces_enabled", cfg.OTel.Traces.Enabled),
			slog.String("otel_traces_endpoint", cfg.OTel.Traces.Endpoint),
			slog.Bool("otel_metrics_enabled", cfg.OTel.Metrics.Enabled),
			slog.String("otel_metrics_endpoint", cfg.OTel.Metrics.Endpoint),
			slog.Bool("otel_logs_enabled", cfg.OTel.Logs.Enabled),
			slog.String("otel_logs_endpoint", firstNonEmpty(cfg.OTel.Logs.Endpoint, cfg.OTel.Traces.Endpoint)),
			slog.Int("provider_count", len(cfg.Providers.OpenAICompatible)),
		)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("gateway stopped unexpectedly", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("gateway shutting down")
	retentionCancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyMap(values ...map[string]string) map[string]string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		cloned := make(map[string]string, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned
	}
	return nil
}

func firstNonZeroDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func buildGatewayDependencies(
	cfg config.Config,
	logger *slog.Logger,
	cacheStore cache.Store,
	semanticStore cache.SemanticStore,
	routerEngine router.Router,
	providerCatalog catalog.Catalog,
	governorEngine governor.Governor,
	providerRegistry providers.Registry,
	healthTracker providers.HealthTracker,
	pricebook billing.Pricebook,
	tracer profiler.Tracer,
	metrics *telemetry.Metrics,
	retentionManager *retention.Manager,
	chatSessionStore chatstate.Store,
) gateway.Dependencies {
	return gateway.Dependencies{
		Logger:   logger,
		Cache:    cacheStore,
		Semantic: semanticStore,
		SemanticOptions: gateway.SemanticOptions{
			Enabled:       cfg.Cache.Semantic.Enabled,
			MinSimilarity: cfg.Cache.Semantic.MinSimilarity,
			MaxTextChars:  cfg.Cache.Semantic.MaxTextChars,
		},
		Resilience: gateway.ResilienceOptions{
			MaxAttempts:     cfg.Provider.MaxAttempts,
			RetryBackoff:    cfg.Provider.RetryBackoff,
			FailoverEnabled: cfg.Provider.FailoverEnabled,
		},
		Router:            routerEngine,
		Catalog:           providerCatalog,
		Governor:          governorEngine,
		Providers:         providerRegistry,
		HealthTracker:     healthTracker,
		Pricebook:         pricebook,
		Tracer:            tracer,
		Metrics:           metrics,
		Retention:         retentionManager,
		ChatSessions:      chatSessionStore,
		TraceBodyCapture:  cfg.Server.TraceBodyCapture,
		TraceBodyMaxBytes: cfg.Server.TraceBodyMaxBytes,
	}
}

func pruneableExactCache(store cache.Store) retention.CachePruner {
	pruner, _ := store.(retention.CachePruner)
	return pruner
}

func pruneableSemanticCache(store cache.SemanticStore) retention.CachePruner {
	pruner, _ := store.(retention.CachePruner)
	return pruner
}

func buildControlPlaneStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) controlplane.Store {
	// Both "memory" (the documented default) and "none" (legacy synonym)
	// fall through to the default branch and produce a MemoryStore.
	// Anything unrecognized does the same — same lenient shape every
	// other backend selector uses today.
	switch cfg.Server.ControlPlaneBackend {
	case "redis":
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Redis.Address,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Timeout:  cfg.Redis.Timeout,
		})
		store, err := controlplane.NewRedisStore(client, cfg.Redis.Prefix, cfg.Server.ControlPlaneKey)
		if err != nil {
			logger.Error("control plane store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	case "postgres":
		store, err := controlplane.NewPostgresStore(context.Background(), postgresClient, cfg.Server.ControlPlaneKey)
		if err != nil {
			logger.Error("control plane store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	default:
		return controlplane.NewMemoryStore()
	}
}

func buildCacheStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) cache.Store {
	if cfg.Cache.Backend == "redis" {
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Redis.Address,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Timeout:  cfg.Redis.Timeout,
		})
		return cache.NewRedisStore(client, cfg.Redis.Prefix, cfg.Cache.DefaultTTL)
	}
	if cfg.Cache.Backend == "postgres" {
		store, err := cache.NewPostgresStore(context.Background(), postgresClient, cfg.Cache.DefaultTTL)
		if err != nil {
			logger.Error("exact cache store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	}
	return cache.NewMemoryStore(cfg.Cache.DefaultTTL)
}

func buildRetentionHistoryStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) retention.HistoryStore {
	switch strings.ToLower(strings.TrimSpace(cfg.Retention.HistoryBackend)) {
	case "redis":
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Redis.Address,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Timeout:  cfg.Redis.Timeout,
		})
		store, err := retention.NewRedisHistoryStore(client, cfg.Redis.Prefix, retentionHistoryKey(cfg.Server.ControlPlaneKey))
		if err != nil {
			logger.Error("retention history store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	case "postgres":
		store, err := retention.NewPostgresHistoryStore(context.Background(), postgresClient, "retention_runs")
		if err != nil {
			logger.Error("retention history store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	default:
		return retention.NewMemoryHistoryStore()
	}
}

func buildSemanticStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) cache.SemanticStore {
	if !cfg.Cache.Semantic.Enabled {
		return cache.NoopSemanticStore{}
	}
	embedder := buildSemanticEmbedder(cfg)
	switch cfg.Cache.Semantic.Backend {
	case "memory":
		return cache.NewMemorySemanticStore(cfg.Cache.Semantic.DefaultTTL, cfg.Cache.Semantic.MaxEntries, embedder)
	case "postgres":
		store, err := cache.NewPostgresSemanticStore(
			context.Background(),
			postgresClient,
			cfg.Cache.Semantic.DefaultTTL,
			cfg.Cache.Semantic.MaxEntries,
			embedder,
			cache.PostgresSemanticOptions{
				VectorMode:         cfg.Cache.Semantic.PostgresVectorMode,
				VectorCandidates:   cfg.Cache.Semantic.PostgresVectorCandidates,
				IndexMode:          cfg.Cache.Semantic.PostgresVectorIndexMode,
				IndexType:          cfg.Cache.Semantic.PostgresVectorIndexType,
				HNSWM:              cfg.Cache.Semantic.PostgresVectorHNSWM,
				HNSWEfConstruction: cfg.Cache.Semantic.PostgresVectorHNSWEfConstruction,
				IVFFlatLists:       cfg.Cache.Semantic.PostgresVectorIVFFlatLists,
				SearchEf:           cfg.Cache.Semantic.PostgresVectorSearchEf,
				SearchProbes:       cfg.Cache.Semantic.PostgresVectorSearchProbes,
			},
		)
		if err != nil {
			logger.Error("semantic cache store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	default:
		return cache.NoopSemanticStore{}
	}
}

func buildSemanticEmbedder(cfg config.Config) cache.Embedder {
	switch cfg.Cache.Semantic.Embedder {
	case "", "local_simple":
		return cache.LocalSimpleEmbedder{
			MaxTextChars: cfg.Cache.Semantic.MaxTextChars,
		}
	case "openai_compatible":
		embedderCfg := cache.OpenAICompatibleEmbedderConfig{
			Name:    "semantic_openai_compatible",
			BaseURL: cfg.Cache.Semantic.EmbedderBaseURL,
			APIKey:  cfg.Cache.Semantic.EmbedderAPIKey,
			Model:   cfg.Cache.Semantic.EmbedderModel,
			Timeout: cfg.Cache.Semantic.EmbedderTimeout,
		}
		if providerCfg, ok := findProviderConfig(cfg.Providers, cfg.Cache.Semantic.EmbedderProvider); ok {
			if embedderCfg.BaseURL == "" {
				embedderCfg.BaseURL = providerCfg.BaseURL
			}
			if embedderCfg.APIKey == "" {
				embedderCfg.APIKey = providerCfg.APIKey
			}
			if embedderCfg.Model == "" {
				embedderCfg.Model = providerCfg.DefaultModel
			}
			if embedderCfg.Timeout == 0 {
				embedderCfg.Timeout = providerCfg.Timeout
			}
			if embedderCfg.Name == "" {
				embedderCfg.Name = providerCfg.Name
			}
		}
		if embedderCfg.BaseURL == "" || embedderCfg.Model == "" {
			return cache.LocalSimpleEmbedder{
				MaxTextChars: cfg.Cache.Semantic.MaxTextChars,
			}
		}
		return cache.NewOpenAICompatibleEmbedder(embedderCfg)
	default:
		return cache.LocalSimpleEmbedder{
			MaxTextChars: cfg.Cache.Semantic.MaxTextChars,
		}
	}
}

func findProviderConfig(cfg config.ProvidersConfig, name string) (config.OpenAICompatibleProviderConfig, bool) {
	for _, providerCfg := range cfg.OpenAICompatible {
		if providerCfg.Name == name {
			return providerCfg, true
		}
	}
	return config.OpenAICompatibleProviderConfig{}, false
}

func buildTaskStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) taskstate.Store {
	switch strings.ToLower(strings.TrimSpace(cfg.Server.TasksBackend)) {
	case "postgres":
		store, err := taskstate.NewPostgresStore(context.Background(), postgresClient)
		if err != nil {
			// Hard-fail: a memory-backed run after `BACKEND=postgres` would
			// silently lose every task on the first restart.
			logger.Error("task store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	default:
		return taskstate.NewMemoryStore()
	}
}

func buildTaskQueue(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) orchestrator.RunQueue {
	lease := time.Duration(cfg.Server.TaskQueueLeaseSeconds) * time.Second
	if lease <= 0 {
		lease = 30 * time.Second
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Server.TaskQueueBackend)) {
	case "postgres":
		queue, err := orchestrator.NewPostgresRunQueue(context.Background(), postgresClient, lease)
		if err != nil {
			logger.Error("task queue init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return queue
	default:
		return orchestrator.NewMemoryRunQueue(cfg.Server.TaskQueueBuffer, lease)
	}
}

func buildBudgetStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) governor.BudgetStore {
	if cfg.Governor.BudgetBackend == "redis" {
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Redis.Address,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Timeout:  cfg.Redis.Timeout,
		})
		return governor.NewRedisBudgetStore(client, cfg.Redis.Prefix)
	}
	if cfg.Governor.BudgetBackend == "postgres" {
		store, err := governor.NewPostgresBudgetStore(context.Background(), postgresClient)
		if err != nil {
			logger.Error("budget store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	}
	return governor.NewMemoryBudgetStore()
}

func buildPostgresClient(cfg config.Config, logger *slog.Logger) *storage.PostgresClient {
	if !postgresRequired(cfg) {
		return nil
	}

	client, err := storage.NewPostgresClient(context.Background(), storage.PostgresConfig{
		DSN:          cfg.Postgres.DSN,
		Schema:       cfg.Postgres.Schema,
		TablePrefix:  cfg.Postgres.TablePrefix,
		MaxOpenConns: cfg.Postgres.MaxOpenConns,
		MaxIdleConns: cfg.Postgres.MaxIdleConns,
	})
	if err != nil {
		logger.Error("postgres init failed", slog.Any("error", err))
		os.Exit(1)
	}
	return client
}

func postgresRequired(cfg config.Config) bool {
	return cfg.Cache.Backend == "postgres" ||
		cfg.Cache.Semantic.Backend == "postgres" ||
		cfg.Governor.BudgetBackend == "postgres" ||
		cfg.Server.ControlPlaneBackend == "postgres" ||
		cfg.Chat.SessionsBackend == "postgres" ||
		cfg.Server.TasksBackend == "postgres" ||
		cfg.Server.TaskQueueBackend == "postgres" ||
		cfg.Retention.HistoryBackend == "postgres"
}

func buildChatSessionStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) chatstate.Store {
	switch cfg.Chat.SessionsBackend {
	case "postgres":
		store, err := chatstate.NewPostgresStore(context.Background(), postgresClient)
		if err != nil {
			logger.Error("chat session store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	default:
		return chatstate.NewMemoryStore()
	}
}

func retentionHistoryKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "control-plane"
	}
	return key + ":retention-history"
}
