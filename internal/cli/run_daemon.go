package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
	"github.com/nlook-service/nlook-router/internal/server"
	"github.com/nlook-service/nlook-router/internal/sshproxy"
	"github.com/nlook-service/nlook-router/internal/tools"
	"github.com/nlook-service/nlook-router/internal/ws"
)

// Version is set by ldflags at build time, or defaults to dev.
var Version = "0.2.15"

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

	var toolsBridge *tools.CLIBridge
	if cfg.ToolsBridgeDir != "" {
		toolsBridge = tools.DefaultCLIBridge(cfg.ToolsBridgeDir)
		srv.SetToolsLister(toolsBridge)
		toolList, err := toolsBridge.ListTools(context.Background())
		if err != nil {
			log.Printf("tools bridge list: %v (using built-in tools only)", err)
		} else {
			// Merge bridge tools with built-in (bridge tools override by name)
			payload.Tools = tools.MergeTools(payload.Tools, toolList)
		}
	}
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
	stepExec := engine.NewStepExecutor(client, skillRunner)
	eng := engine.NewWorkflowEngine(stepExec)
	execService := executor.NewExecutionService(client, eng, 5*time.Second)

	// SSH proxy
	sshProxy := sshproxy.NewProxy()

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

		// Cache store for user data (documents, tasks)
		cacheStore := cache.NewStore()
		syncHandler := cache.NewSyncHandler(cacheStore)

		// Embedding vector store for semantic search
		embedder := embedding.NewEmbedder()
		vectorStore := embedding.NewVectorStore(embedder)
		syncHandler.SetVectorStore(vectorStore)

		// Wire chat messages from cloud → chat handler
		chatHandler := chat.NewHandler(skillRunner, func(msg []byte) {
			wsClient.Send(msg)
		})
		chatHandler.SetCacheStore(cacheStore)
		chatHandler.SetVectorStore(vectorStore)

		// Wire SSH messages from cloud → SSH proxy
		sshHandler := sshproxy.NewHandler(sshProxy, func(msg []byte) {
			wsClient.Send(msg)
		})

		// Route messages: sync first, then chat, then SSH
		wsClient.OnMessage = func(msgType string, payload json.RawMessage) {
			if syncHandler.HandleMessage(msgType, payload) {
				return
			}
			if chatHandler.HandleMessage(msgType, payload) {
				return
			}
			sshHandler.HandleMessage(msgType, payload)
		}

		// Tell executor to skip polling when WebSocket is connected
		execService.SetWSConnected(wsClient.IsConnected)

		wsClient.Start(ctx)
	}

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
	sshProxy.CloseAll()
	return srv.Shutdown(ctx)
}
