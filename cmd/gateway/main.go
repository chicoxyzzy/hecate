package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hecate/agent-runtime/internal/api"
	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/catalog"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/internal/telemetry"
)

func main() {
	cfg := config.LoadFromEnv()
	logger, shutdownLogs, err := telemetry.NewLoggerWithOTLP(context.Background(), cfg.LogLevel, telemetry.OTelLogOptions{
		Enabled:     cfg.OTel.LogsEnabled,
		Endpoint:    firstNonEmpty(cfg.OTel.LogsEndpoint, cfg.OTel.TracesEndpoint),
		Headers:     firstNonEmptyMap(cfg.OTel.LogsHeaders, cfg.OTel.TracesHeaders),
		ServiceName: cfg.OTel.ServiceName,
		Timeout:     firstNonZeroDuration(cfg.OTel.LogsTimeout, cfg.OTel.TracesTimeout),
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
	metrics := telemetry.NewMetrics()
	postgresClient := buildPostgresClient(cfg, logger)
	if postgresClient != nil {
		defer func() {
			if err := postgresClient.Close(); err != nil {
				logger.Warn("postgres close failed", slog.Any("error", err))
			}
		}()
	}

	providerList := make([]providers.Provider, 0, len(cfg.Providers.OpenAICompatible))
	for _, providerCfg := range cfg.Providers.OpenAICompatible {
		providerList = append(providerList, providers.NewOpenAICompatibleProvider(providerCfg, logger))
	}
	providerRegistry := providers.NewRegistry(providerList...)
	healthTracker := providers.NewMemoryHealthTracker(cfg.Provider.HealthThreshold, cfg.Provider.HealthCooldown)

	pricebook := billing.NewStaticPricebook(cfg.Providers)
	otelProvider, err := profiler.NewTracerProvider(
		context.Background(),
		cfg.OTel.TracesEnabled,
		cfg.OTel.TracesEndpoint,
		cfg.OTel.TracesHeaders,
		cfg.OTel.ServiceName,
		cfg.OTel.TracesTimeout,
	)
	if err != nil {
		logger.Error("otel tracer provider init failed", slog.Any("error", err))
		os.Exit(1)
	}
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
	controlPlaneStore := buildControlPlaneStore(cfg, logger, postgresClient)
	providerCatalog := catalog.NewRegistryCatalog(providerRegistry, healthTracker)
	routerEngine := router.NewRuleRouter(
		cfg.Router.DefaultProvider,
		cfg.Router.DefaultModel,
		cfg.Router.Strategy,
		cfg.Router.FallbackProvider,
		providerCatalog,
	)
	governorEngine := governor.NewStaticGovernor(cfg.Governor, budgetStore, budgetStore)

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
	))

	handler := api.NewHandler(cfg, logger, service, controlPlaneStore)
	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           api.NewServer(logger, handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("gateway starting",
			slog.String("addr", cfg.Server.Address),
			slog.String("default_provider", cfg.Router.DefaultProvider),
			slog.String("default_model", cfg.Router.DefaultModel),
			slog.String("router_strategy", cfg.Router.Strategy),
			slog.String("cache_backend", cfg.Cache.Backend),
			slog.Bool("semantic_cache_enabled", cfg.Cache.Semantic.Enabled),
			slog.String("semantic_cache_backend", cfg.Cache.Semantic.Backend),
			slog.Int("provider_max_attempts", cfg.Provider.MaxAttempts),
			slog.Bool("provider_failover_enabled", cfg.Provider.FailoverEnabled),
			slog.Int("provider_health_failure_threshold", cfg.Provider.HealthThreshold),
			slog.Duration("provider_health_cooldown", cfg.Provider.HealthCooldown),
			slog.Bool("otel_traces_enabled", cfg.OTel.TracesEnabled),
			slog.String("otel_traces_endpoint", cfg.OTel.TracesEndpoint),
			slog.Bool("otel_logs_enabled", cfg.OTel.LogsEnabled),
			slog.String("otel_logs_endpoint", firstNonEmpty(cfg.OTel.LogsEndpoint, cfg.OTel.TracesEndpoint)),
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
		Router:        routerEngine,
		Catalog:       providerCatalog,
		Governor:      governorEngine,
		Providers:     providerRegistry,
		HealthTracker: healthTracker,
		Pricebook:     pricebook,
		Tracer:        tracer,
		Metrics:       metrics,
	}
}

func buildControlPlaneStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) controlplane.Store {
	switch cfg.Server.ControlPlaneBackend {
	case "file":
		store, err := controlplane.NewFileStore(cfg.Server.ControlPlaneFile)
		if err != nil {
			logger.Error("control plane store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		return store
	case "redis":
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Cache.Redis.Address,
			Password: cfg.Cache.Redis.Password,
			DB:       cfg.Cache.Redis.DB,
			Timeout:  cfg.Cache.Redis.Timeout,
		})
		store, err := controlplane.NewRedisStore(client, cfg.Cache.Redis.Prefix, cfg.Server.ControlPlaneKey)
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
		return nil
	}
}

func buildCacheStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) cache.Store {
	if cfg.Cache.Backend == "redis" {
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Cache.Redis.Address,
			Password: cfg.Cache.Redis.Password,
			DB:       cfg.Cache.Redis.DB,
			Timeout:  cfg.Cache.Redis.Timeout,
		})
		return cache.NewRedisStore(client, cfg.Cache.Redis.Prefix, cfg.Cache.DefaultTTL)
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

func buildBudgetStore(cfg config.Config, logger *slog.Logger, postgresClient *storage.PostgresClient) governor.BudgetStore {
	if cfg.Governor.BudgetBackend == "redis" {
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Cache.Redis.Address,
			Password: cfg.Cache.Redis.Password,
			DB:       cfg.Cache.Redis.DB,
			Timeout:  cfg.Cache.Redis.Timeout,
		})
		return governor.NewRedisBudgetStore(client, cfg.Cache.Redis.Prefix)
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
		cfg.Server.ControlPlaneBackend == "postgres"
}
