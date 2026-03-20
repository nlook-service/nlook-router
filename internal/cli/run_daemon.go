package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/config"
	"github.com/nlook-service/nlook-router/internal/engine"
	"github.com/nlook-service/nlook-router/internal/executor"
	"github.com/nlook-service/nlook-router/internal/heartbeat"
	"github.com/nlook-service/nlook-router/internal/scheduler"
	"github.com/nlook-service/nlook-router/internal/cache"
	"github.com/nlook-service/nlook-router/internal/chat"
	"github.com/nlook-service/nlook-router/internal/embedding"
	"github.com/nlook-service/nlook-router/internal/llm"
	"github.com/nlook-service/nlook-router/internal/mcp"
	"github.com/nlook-service/nlook-router/internal/memory"
	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/server"
	"github.com/nlook-service/nlook-router/internal/agentproxy"
	"github.com/nlook-service/nlook-router/internal/db"
	"github.com/nlook-service/nlook-router/internal/session"
	"github.com/nlook-service/nlook-router/internal/sshproxy"
	"github.com/nlook-service/nlook-router/internal/tools"
	"github.com/nlook-service/nlook-router/internal/tracing"
	"github.com/nlook-service/nlook-router/internal/usage"
	"github.com/nlook-service/nlook-router/internal/ws"
)

// Version is set by ldflags at build time, or defaults to dev.
var Version = "0.2.60"

// RunDaemon starts the local HTTP server, heartbeat loop, WebSocket client, and SSH proxy.
func RunDaemon(cfg *config.Config) error {
	// Check for updates in background (non-blocking)
	CheckForUpdate()
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	status := &server.Status{
		RouterID:  cfg.RouterID,
		Connected: false,
	}
	srv := server.New(addr, status)
	client := apiclient.New(cfg.APIURL, cfg.APIKey)
	payload := &apiclient.RegisterPayload{
		RouterID: cfg.RouterID,
		Version:  Version,
	}
	if payload.RouterID == "" {
		payload.RouterID = "local-1"
	}
	// Always include built-in tools
	payload.Tools = tools.BuiltInTools()

	// Discover local Ollama models
	ollamaClient := ollama.NewClient()
	if ollamaClient.IsRunning(context.Background()) {
		if models, err := ollamaClient.List(context.Background()); err == nil {
			for _, m := range models {
				sizeMB := float64(m.Size) / (1024 * 1024)
				sizeStr := fmt.Sprintf("%.0f MB", sizeMB)
				if sizeMB >= 1024 {
					sizeStr = fmt.Sprintf("%.1f GB", sizeMB/1024)
				}
				payload.Models = append(payload.Models, apiclient.ModelMeta{
					Name:     m.Name,
					Provider: "ollama",
					Size:     sizeStr,
				})
			}
		}
	}

	var toolsBridge *tools.CLIBridge
	if cfg.ToolsBridgeDir != "" {
		toolsBridge = tools.DefaultCLIBridge(cfg.ToolsBridgeDir)
		srv.SetToolsLister(toolsBridge)
		toolList, err := toolsBridge.ListTools(context.Background())
		if err != nil {
			// Auto-install tools-bridge dependencies
			log.Printf("tools bridge: installing dependencies...")
			reqFile := cfg.ToolsBridgeDir + "/requirements.txt"
			if _, statErr := os.Stat(reqFile); statErr == nil {
				installCmd := exec.Command("pip3", "install", "-q", "-r", reqFile)
				installCmd.Dir = cfg.ToolsBridgeDir
				if installErr := installCmd.Run(); installErr != nil {
					log.Printf("tools bridge: pip install failed: %v", installErr)
				} else {
					// Retry after install
					toolList, err = toolsBridge.ListTools(context.Background())
				}
			}
			if err != nil {
				log.Printf("tools bridge: %v (using built-in tools only)", err)
			}
		}
		if toolList != nil {
			payload.Tools = tools.MergeTools(payload.Tools, toolList)
		}
	}
	// Apply LLM config from config.yaml as env vars (before any engine init)
	cfg.ApplyLLMEnv()

	reg := heartbeat.NewRegistrar(client, 0, payload)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		log.Printf("register warning: %v (continuing)", err)
	} else {
		status.Connected = true
	}

	// Engine setup
	skillRunner := engine.NewSkillRunner()
	if toolsBridge != nil {
		skillRunner.SetToolExecutor(toolsBridge)
	}
	if cfg.APIKey != "" {
		skillRunner.SetMCPClient(mcp.NewClient(cfg.APIKey))
	}
	stepExec := engine.NewStepExecutor(client, skillRunner)
	eng := engine.NewWorkflowEngine(stepExec)
	execService := executor.NewExecutionService(client, eng, 5*time.Second)

	// SSH proxy
	sshProxy := sshproxy.NewProxy()

	// Unified DB layer (optional, configured via config.yaml db.driver)
	dataDir := config.ConfigDir()
	if cfg.DB.DataDir != "" {
		dataDir = cfg.DB.DataDir
	}
	var storage db.DB
	if cfg.DB.Driver != "" && cfg.DB.Driver != "file" {
		var err error
		storage, err = db.New(cfg.DB.Driver, dataDir)
		if err != nil {
			log.Printf("db: init %s failed: %v (falling back to file)", cfg.DB.Driver, err)
		} else {
			log.Printf("db: using %s driver (data_dir=%s)", cfg.DB.Driver, dataDir)
			defer storage.Close()
		}
	}

	// Session store + tracing (shared across chat, agent, engine)
	var sessionStore *session.Store
	var traceWriter *tracing.Writer
	if storage != nil {
		sessionStore = session.NewStoreWithDB(storage, session.DefaultTTL)
		traceWriter = tracing.NewWriterWithDB(storage)
	} else {
		sessionStore = session.NewStore(dataDir, session.DefaultTTL)
		traceWriter = tracing.NewWriter(dataDir)
	}
	traceCollector := tracing.NewCollector(traceWriter)
	srv.SetSessionStore(sessionStore)
	srv.SetTraceWriter(traceWriter)
	eng.SetTracer(traceCollector)

	// Agent proxy (Claude Code CLI execution in workspaces)
	agentManager := agentproxy.NewSessionManager(ctx, agentproxy.SessionConfig{
		Workspaces:      cfg.Agent.Workspaces,
		MaxSessions:     cfg.Agent.MaxSessions,
		SessionTimeout:  cfg.Agent.SessionTimeout,
		AllowedCommands: cfg.Agent.AllowedCommands,
	})

	// WebSocket client (real-time communication with cloud)
	var wsClient *ws.Client
	if cfg.APIKey != "" {
		wsClient = ws.NewClient(cfg.APIURL, cfg.APIKey, payload.RouterID)

		// Wire WebSocket run dispatch → executor
		wsClient.OnRunDispatch = func(p ws.RunDispatchPayload) {
			log.Printf("ws: received run dispatch: run_id=%d workflow_id=%d", p.RunID, p.WorkflowID)
			execService.DispatchRun(ctx, apiclient.RunInfo{
				ID:         p.RunID,
				WorkflowID: p.WorkflowID,
				UserID:     p.UserID,
			})
		}

		// Wire WebSocket run cancel → executor
		wsClient.OnRunCancel = func(runID int64) {
			log.Printf("ws: received run cancel: run_id=%d", runID)
			execService.CancelRun(runID)
		}

		// Wire session:end → session cleanup + summary to cloud
		wsClient.OnSessionEnd = func(sessionID string) {
			log.Printf("ws: session:end received: %s", sessionID)
			sess := sessionStore.Get(sessionID)
			if sess == nil {
				return
			}

			// Collect trace stats
			events, _ := traceWriter.ReadEvents(sessionID)
			traceCount := len(events)

			// Build summary
			summary := map[string]interface{}{
				"session_id":  sessionID,
				"type":        string(sess.Type),
				"user_id":     sess.UserID,
				"agent_ids":   sess.AgentIDs,
				"run_ids":     sess.RunIDs,
				"trace_count": traceCount,
				"duration_ms": time.Since(sess.CreatedAt).Milliseconds(),
			}
			if sess.Context != nil {
				summary["message_count"] = len(sess.Context.Messages)
				summary["agent_result_count"] = len(sess.Context.AgentResults)
				summary["summary"] = sess.Context.Summary
			}

			// Send summary to cloud
			_ = wsClient.SendMessage("session:summary", summary)

			// Emit trace event for session end
			traceCollector.Emit(tracing.NewEvent(sessionID, tracing.EventSessionEnd, "session:end").
				WithMeta(map[string]interface{}{"trace_count": traceCount}))

			// Cleanup local session (trace files preserved)
			sess.Complete()
			sessionStore.Delete(sessionID)
			log.Printf("ws: session %s completed (traces=%d)", sessionID, traceCount)
		}

		// LLM engine (auto-detect vLLM or Ollama)
		llmEngine := llm.NewEngine()
		if llmEngine.Type() == llm.EngineVLLM {
			if err := llmEngine.StartManaged(ctx); err != nil {
				log.Printf("llm: failed to start vLLM: %v (falling back to Ollama)", err)
			} else {
				defer llmEngine.Stop()
			}
		}
		// llmEngine will be passed to chat handler and server
		srv.SetLLMEngine(llmEngine)

		// Cache store for user data (documents, tasks)
		var cacheStore *cache.Store
		if storage != nil {
			cacheStore = cache.NewStoreWithDB(storage)
		} else {
			cacheStore = cache.NewStore()
		}
		syncHandler := cache.NewSyncHandler(cacheStore)

		// Embedding vector store for semantic search
		embedder := embedding.NewEmbedder()
		vectorStore := embedding.NewVectorStore(embedder)
		syncHandler.SetVectorStore(vectorStore)

		// Usage tracker — persists to ~/.nlook/usage.json
		usageTracker := usage.NewTracker(config.ConfigDir() + "/usage.json")
		reg.UsageTracker = usageTracker

		// Wire chat messages from cloud → chat handler
		chatHandler := chat.NewHandler(skillRunner, func(msg []byte) {
			wsClient.Send(msg)
		}, cfg.APIKey, usageTracker)
		var memoryStore *memory.Store
		if storage != nil {
			memoryStore = memory.NewStoreWithDB(storage)
		} else {
			memoryStore = memory.NewStore()
		}
		// Wire LLM-based memory optimization (uses Ollama for compression & fact extraction)
		if ollamaClient.IsRunning(context.Background()) {
			aiModel := os.Getenv("NLOOK_AI_MODEL")
			memoryStore.SetOptimizer(memory.NewSummarizeStrategy(ollamaClient, aiModel))
			memoryStore.SetFactExtractor(memory.NewFactExtractor(ollamaClient, aiModel))
			log.Printf("memory: LLM-based optimizer & fact extractor enabled")
		}
		if storage != nil {
			chatHandler.SetDB(storage)
		}
		chatHandler.SetCacheStore(cacheStore)
		chatHandler.SetVectorStore(vectorStore)
		chatHandler.SetMemoryStore(memoryStore)
		chatHandler.SetLLMEngine(llmEngine)
		chatHandler.SetSessionStore(sessionStore)
		chatHandler.SetTracer(traceCollector)
		if toolsBridge != nil {
			chatHandler.SetToolExecutor(toolsBridge)
			log.Printf("chat: built-in tools connected (web_search, code_interpreter, etc.)")
		}

		// Wire SSH messages from cloud → SSH proxy
		sshHandler := sshproxy.NewHandler(sshProxy, func(msg []byte) {
			wsClient.Send(msg)
		})

		// Wire agent messages from cloud → agent proxy
		agentHandler := agentproxy.NewHandler(agentManager, func(msg []byte) {
			wsClient.Send(msg)
		})
		agentHandler.SetUsageRecorder(agentUsageAdapter{tracker: usageTracker})
		agentHandler.SetSessionStore(sessionStore)
		agentHandler.SetTracer(traceCollector)

		// Route messages: sync → chat → agent → SSH
		wsClient.OnMessage = func(msgType string, payload json.RawMessage) {
			if syncHandler.HandleMessage(msgType, payload) {
				return
			}
			if chatHandler.HandleMessage(msgType, payload) {
				return
			}
			if agentHandler.HandleMessage(msgType, payload) {
				return
			}
			sshHandler.HandleMessage(msgType, payload)
		}

		// Tell executor to skip polling when WebSocket is connected
		execService.SetWSConnected(wsClient.IsConnected)

		wsClient.Start(ctx)
	}

	// Warmup Ollama model in background for fast fallback
	go server.WarmupOllama()

	execService.Start(ctx)

	// Scheduler: polls server for schedules and triggers runs on cron
	sched := scheduler.New(client, 30*time.Second)
	sched.Start(ctx)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()
	log.Printf("router v%s listening on http://%s", Version, addr)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	_ = reg.Stop()
	sched.Stop()
	execService.Stop()
	if wsClient != nil {
		wsClient.Stop()
	}
	agentManager.CloseAll()
	sshProxy.CloseAll()
	traceCollector.Close()
	sessionStore.Close()
	return srv.Shutdown(ctx)
}

// agentUsageAdapter bridges usage.Tracker to agentproxy.UsageRecorder.
type agentUsageAdapter struct {
	tracker *usage.Tracker
}

func (a agentUsageAdapter) Record(u agentproxy.UsageRecord) {
	a.tracker.Record(usage.TokenUsage{
		UserID:       u.UserID,
		Provider:     u.Provider,
		Model:        u.Model,
		Category:     u.Category,
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		ElapsedMs:    u.ElapsedMs,
	})
}
