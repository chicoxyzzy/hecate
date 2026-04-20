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
	logger := telemetry.NewLogger(cfg.LogLevel)
	metrics := telemetry.NewMetrics()

	providerList := make([]providers.Provider, 0, len(cfg.Providers.OpenAICompatible))
	for _, providerCfg := range cfg.Providers.OpenAICompatible {
		providerList = append(providerList, providers.NewOpenAICompatibleProvider(providerCfg, logger))
	}
	providerRegistry := providers.NewRegistry(providerList...)

	pricebook := billing.NewStaticPricebook(cfg.Providers)
	tracer := profiler.NewInMemoryTracer()
	cacheStore := buildCacheStore(cfg)
	budgetStore := buildBudgetStore(cfg)
	controlPlaneStore := buildControlPlaneStore(cfg, logger)
	routerEngine := router.NewRuleRouter(
		cfg.Router.DefaultProvider,
		cfg.Router.DefaultModel,
		cfg.Router.Strategy,
		cfg.Router.FallbackProvider,
		providerRegistry,
	)
	governorEngine := governor.NewStaticGovernor(cfg.Governor, budgetStore)

	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cacheStore,
		Router:    routerEngine,
		Governor:  governorEngine,
		Providers: providerRegistry,
		Pricebook: pricebook,
		Tracer:    tracer,
		Metrics:   metrics,
	})

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

func buildControlPlaneStore(cfg config.Config, logger *slog.Logger) controlplane.Store {
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
	default:
		return nil
	}
}

func buildCacheStore(cfg config.Config) cache.Store {
	if cfg.Cache.Backend == "redis" {
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Cache.Redis.Address,
			Password: cfg.Cache.Redis.Password,
			DB:       cfg.Cache.Redis.DB,
			Timeout:  cfg.Cache.Redis.Timeout,
		})
		return cache.NewRedisStore(client, cfg.Cache.Redis.Prefix, cfg.Cache.DefaultTTL)
	}
	return cache.NewMemoryStore(cfg.Cache.DefaultTTL)
}

func buildBudgetStore(cfg config.Config) governor.BudgetStore {
	if cfg.Governor.BudgetBackend == "redis" {
		client := storage.NewRedisClient(storage.RedisConfig{
			Address:  cfg.Cache.Redis.Address,
			Password: cfg.Cache.Redis.Password,
			DB:       cfg.Cache.Redis.DB,
			Timeout:  cfg.Cache.Redis.Timeout,
		})
		return governor.NewRedisBudgetStore(client, cfg.Cache.Redis.Prefix)
	}
	return governor.NewMemoryBudgetStore()
}
