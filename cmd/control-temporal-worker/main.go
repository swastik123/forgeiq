package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forgeiq/internal/catalog"
	"forgeiq/internal/config"
	"forgeiq/internal/controlplane/temporal"
	"forgeiq/internal/eval"
	"forgeiq/internal/observability"
	"forgeiq/internal/registry"
	"forgeiq/internal/transport/a2a"
	mcpclient "forgeiq/internal/transport/mcp"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize observability
	logger, err := observability.NewLogger(cfg.Observability.LogLevel == "debug")
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger = logger.WithComponent("temporal-worker")

	// Initialize Temporal client
	c, err := client.Dial(client.Options{
		HostPort: cfg.Temporal.HostPort,
	})
	if err != nil {
		logger.Fatal("unable to create Temporal client", zap.Error(err))
	}
	defer c.Close()

	acts := &temporal.Activities{
		Config: cfg,
	}
	if cfg.AgentRouter.Enabled {
		var a2aAgents []a2a.Agent
		if cfg.AgentRouter.RegistryEnabled {
			store, err := registry.NewPostgresAgentStore(cfg.Storage.DSN)
			if err != nil {
				logger.Fatal("failed to init agent registry store", zap.Error(err))
			}
			defer store.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			regAgents, err := store.ListAgents(ctx, registry.AgentFilter{Limit: 200})
			if err != nil {
				logger.Fatal("failed to load agents from registry", zap.Error(err))
			}
			a2aAgents = make([]a2a.Agent, 0, len(regAgents))
			for _, a := range regAgents {
				a2aAgents = append(a2aAgents, a2a.Agent{
					ID:        a.ID,
					Type:      a.Type,
					BaseURL:   a.BaseURL,
					Name:      a.Name,
					Version:   a.Version,
					TaskTypes: a.Capabilities.TaskTypes,
					Tags:      a.Capabilities.Tags,
				})
			}
			logger.Info("Agent router loaded from registry", zap.Int("agents", len(a2aAgents)))
		} else {
			rr, err := a2a.NewAgentRouterFromConfig(cfg)
			if err != nil {
				logger.Fatal("invalid agent router config", zap.Error(err))
			}
			a2aAgents = rr.Agents
			// Best-effort discovery (only affects metadata, not routing safety)
			if cfg.AgentRouter.DiscoveryEnabled {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				rr.Discover(ctx, cfg.AgentRouter.DiscoveryPath)
				a2aAgents = rr.Agents
			}
			logger.Info("Agent router loaded from config", zap.Int("agents", len(a2aAgents)))
		}
		rt, err := a2a.NewRouterFromConfig(cfg, a2aAgents)
		if err != nil {
			logger.Fatal("failed to build agent router", zap.Error(err))
		}
		acts.Router = rt
	}

	// Tool Catalog: fetch MCP tool list and validate plan/tool calls against it.
	cat := catalog.New()
	{
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		tc := mcpclient.NewClientFromConfig(cfg)
		if err := cat.RefreshFromToolClient(ctx, tc); err != nil {
			logger.Warn("failed to refresh tool catalog from MCP; tool compatibility validation disabled", zap.Error(err))
		} else {
			acts.Catalog = cat
			logger.Info("Tool catalog refreshed", zap.Int("tools", len(cat.List())))
		}
	}

	// Eval loop store: record per-run metrics for comparing agent versions.
	if cfg.Storage.Type == "postgres" && cfg.Storage.DSN != "" {
		es, err := eval.NewPostgresStore(cfg.Storage.DSN)
		if err != nil {
			logger.Warn("failed to init eval store; eval loop disabled", zap.Error(err))
		} else {
			defer es.Close()
			acts.Eval = es

			// Runtime weights (optional): compute from eval_runs and inject into router scoring.
			if cfg.AgentRouter.RuntimeWeightsEnabled && acts.Router != nil {
				refresh := func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					weights, err := es.RefreshRuntimeWeights(ctx, cfg.AgentRouter.RuntimeWeightsWindowHours, cfg.AgentRouter.RuntimeWeightsMinRuns, cfg.AgentRouter.RuntimeWeightsMax)
					if err != nil {
						logger.Warn("failed to refresh runtime weights", zap.Error(err))
						return
					}
					if a2a.ApplyRuntimeWeights(acts.Router, weights, cfg.AgentRouter.RuntimeWeightsMax) {
						logger.Info("runtime weights applied", zap.Any("stamp", a2a.ApplyRuntimeWeightsStamp(acts.Router)))
					}
				}
				refresh()
				if cfg.AgentRouter.RuntimeWeightsRefreshSeconds > 0 {
					go func() {
						t := time.NewTicker(time.Duration(cfg.AgentRouter.RuntimeWeightsRefreshSeconds) * time.Second)
						defer t.Stop()
						for range t.C {
							refresh()
						}
					}()
				}
			}
		}
	}

	w := worker.New(c, temporal.TaskQueue, worker.Options{})

	w.RegisterWorkflow(temporal.IncidentWorkflow)
	w.RegisterWorkflow(temporal.IncidentWorkflowIterative)
	w.RegisterWorkflow(temporal.IncidentWorkflowWithAgenticLoop)
	w.RegisterActivity(acts)

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Start worker in goroutine
	errChan := make(chan error, 1)
	go func() {
		logger.Info("Temporal worker running", zap.String("task_queue", temporal.TaskQueue))
		if err := w.Run(worker.InterruptCh()); err != nil {
			errChan <- err
		}
	}()

	// Wait for interrupt or error
	select {
	case <-quit:
		logger.Info("Shutting down worker...")
		w.Stop()
		logger.Info("Worker stopped gracefully")
	case err := <-errChan:
		logger.Fatal("Worker failed", zap.Error(err))
	}
}
