package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/api"
	"github.com/DigiLogicTech/OnePane/internal/bootstrap"
	"github.com/DigiLogicTech/OnePane/internal/chatcommands"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/config"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/ingress"
	"github.com/DigiLogicTech/OnePane/internal/nodefederation"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/webui"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to bootstrap YAML configuration")
	flag.Parse()

	log.Printf("harnessd startup: loading configuration")
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("harnessd startup: entering bootstrap")
	runtime, err := bootstrap.Open(ctx, cfg)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	log.Printf("harnessd startup: bootstrap ready; preparing API routes")
	defer runtime.DB.Close()
	// A previous service crash may have stranded private OCI credential files
	// without another sandbox command to trigger the per-runtime cleanup.
	// Never clean global/SYSTEM temp or user Project/Workspace data.
	if err:=sandboxrunner.CleanupStaleRuntimeCredentialFiles(cfg.Storage.DataDir);err!=nil{
		log.Printf("private sandbox credential cleanup needs attention: %v",err)
	}

	state, err := runtime.System.Get(ctx)
	if err != nil {
		log.Fatalf("system state: %v", err)
	}
	n, err := runtime.Nodes.Local(ctx)
	if err != nil {
		log.Fatalf("local node: %v", err)
	}

	bearerAuth := api.NewBearerAuthorizer(runtime.DB.SQL(), clock.Real{})
	auth := api.NewHybridAuthorizer(bearerAuth, runtime.WebAuth)
	apiServer := api.NewServer(runtime.ProjectWorkspaces, event.NewReader(runtime.DB.SQL()), auth)
	apiServer.SetAttentionDB(runtime.DB.SQL())
	apiServer.SetLibraryArtifacts(runtime.Artifacts)
	apiServer.SetWebAuth(runtime.WebAuth, strings.HasPrefix(config.APIOrigin(cfg), "https://"))
	apiServer.SetVault(runtime.Vault)
	apiServer.SetProviderOnboarding(runtime.ProviderOnboarding)
	apiServer.SetScheduler(runtime.Scheduler)
	apiServer.SetAgentRuntimes(runtime.AgentRuntimes)
	apiServer.SetGateway(runtime.Gateway)
	apiServer.SetTeam(runtime.Team)
	apiServer.SetBots(runtime.Bots)
	apiServer.SetFederation(runtime.Federation)
	apiServer.SetTasks(runtime.Tasks)
	apiServer.SetRoutines(runtime.Routines)
	chatCommandService := chatcommands.NewService(chatcommands.SQLStore{DB: runtime.DB.SQL()})
	apiServer.SetChatCommands(chatCommandService)
	apiServer.SetAssistant(runtime.Assistant)
	apiServer.SetProjectOrchestrator(runtime.ProjectOrchestrator)
	apiServer.SetAgentProfiles(runtime.AgentProfiles)
	apiServer.SetSkills(runtime.Skills)
	apiServer.SetProviderOAuth(runtime.ProviderOAuth)
	apiServer.SetLocalAI(runtime.LocalAI, n.ID)
	apiServer.SetAssurance(runtime.Assurance)
	modelPoolPath := strings.TrimSpace(cfg.LocalAI.ModelPoolPath)
	if modelPoolPath == "" {
		modelPoolPath = filepath.Join(cfg.Storage.DataDir, "models", "managed")
	}
	apiServer.SetRuntimeConfig(configPath, modelPoolPath)
	apiServer.SetWebUI(webui.Handler())
	previewSessions, err := ingress.NewSessions(config.PreviewOrigin(cfg), clock.Real{})
	if err != nil {
		log.Fatalf("preview sessions: %v", err)
	}
	apiServer.SetPreviewSessions(previewSessions)
	previewServer := ingress.NewServer(runtime.ProjectWorkspaces, ingress.NewDBAccessChecker(runtime.DB.SQL(), clock.Real{}), previewSessions)
	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // SSE responses are intentionally long-lived.
		IdleTimeout:       60 * time.Second,
	}
	// The Windows SCM wrapper cannot send POSIX SIGTERM to its hidden
	// subprocess. It provides an ephemeral per-launch token and requests a
	// loopback-only graceful stop before any forced process-tree termination.
	// No shutdown endpoint exists when running without that token.
	if token := os.Getenv("ONEPANE_SERVICE_SHUTDOWN_TOKEN"); len(token) >= 32 {
		base := httpServer.Handler
		httpServer.Handler = http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
			if r.URL.Path != "/__onepane/service/shutdown" {base.ServeHTTP(w,r);return}
			host,_,err:=net.SplitHostPort(r.RemoteAddr)
			if err!=nil||!net.ParseIP(host).IsLoopback()||r.Method!=http.MethodPost||
				subtle.ConstantTimeCompare([]byte(r.Header.Get("X-OnePane-Service-Token")),[]byte(token))!=1 {
				http.Error(w,"not found",http.StatusNotFound);return
			}
			w.WriteHeader(http.StatusAccepted)
			go cancel()
		})
	}
	previewHTTPServer := &http.Server{
		Addr:              cfg.Server.PreviewListen,
		Handler:           previewServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       0, // App requests may stream; proxy-level bounds still apply.
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	var federationHTTPServer *http.Server
	if runtime.Federation != nil {
		fedServer := nodefederation.NewServer(runtime.Federation, runtime.Inference, runtime.LocalAI)
		federationHTTPServer = &http.Server{
			Addr: cfg.Federation.Listen, Handler: fedServer.Handler(), TLSConfig: fedServer.TLSConfig(),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		}
	}
	log.Printf("harnessd startup: recovering model download/install jobs")
	if err := runtime.LocalAI.RecoverInstallJobs(ctx); err != nil {
		log.Printf("recover local AI install jobs: %v", err)
	}
	if strings.TrimSpace(cfg.LocalAI.CatalogURL) != "" {
		if rec, err := runtime.LocalAI.RefreshCatalog(ctx, cfg.LocalAI.CatalogURL, nil); err != nil {
			log.Printf("refresh local AI catalog: %v", err)
		} else {
			log.Printf("local AI catalog active: version=%s key=%s", rec.CatalogVersion, rec.KeyID)
		}
	}

	if strings.TrimSpace(cfg.LocalAI.CatalogURL) != "" {
		go func() {
			ticker := time.NewTicker(6 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if rec, err := runtime.LocalAI.RefreshCatalog(ctx, cfg.LocalAI.CatalogURL, nil); err != nil && ctx.Err() == nil {
						log.Printf("refresh local AI catalog: %v", err)
					} else if err == nil {
						log.Printf("local AI catalog refreshed: version=%s", rec.CatalogVersion)
					}
				}
			}
		}()
	}

	go func() {
		err := runtime.Watchdog.Run(ctx, 5*time.Second, func() map[string]any {
			return map[string]any{"status": "healthy", "component": "harnessd"}
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("watchdog stopped: %v; autonomous mutation will fail closed", err)
		}
	}()

	// Local AI installs are durable jobs. The worker advances only jobs whose
	// catalog artifacts were signature-verified and pinned at approval time.
	go func() {
		run := func() {
			jobs, err := runtime.LocalAI.TickInstallJobs(ctx, 1)
			if err != nil && ctx.Err() == nil {
				log.Printf("local AI install worker: %v", err)
				return
			}
			for _, job := range jobs {
				if job.Status == "failed" && job.FailureReason != nil {
					log.Printf("local AI install job %s failed: %s", job.ID, *job.FailureReason)
				}
			}
		}
		run()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	// Managed local models are loaded lazily and may be unloaded after an idle
	// period to free VRAM/RAM. A later inference request starts them again.
	if cfg.LocalAI.IdleUnloadMinutes > 0 {
		go func() {
			idle := time.Duration(cfg.LocalAI.IdleUnloadMinutes) * time.Minute
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := runtime.LocalAI.ReapIdleManagedRuntimes(ctx, idle, 10); err != nil && ctx.Err() == nil {
						log.Printf("local AI idle reaper: %v", err)
					} else if n > 0 {
						log.Printf("local AI idle reaper stopped %d runtime(s)", n)
					}
				}
			}
		}()
	}

	// Routine scheduling is deterministic control-plane work. Occurrences become
	// normal Tasks; this loop never executes project/app commands directly.
	go func() {
		tick := func() {
			if _, err := runtime.Routines.Tick(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
				log.Printf("routine scheduler tick: %v", err)
			}
		}
		tick()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tick()
			}
		}
	}()

	// Budget reservations are durable but time-bounded. Expiring abandoned
	// reservations returns capacity without relying on a caller to clean up.
	go func() {
		run := func() {
			if n, err := runtime.Budgets.ExpireDue(ctx, 200); err != nil && ctx.Err() == nil {
				log.Printf("budget expiry: %v", err)
			} else if n > 0 {
				log.Printf("budget expiry released %d reservation(s)", n)
			}
		}
		run()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	// Team Mode deliberation processes explicit human-requested rounds before or
	// during execution. It performs reasoning only: no tool proposal is permitted.
	go func() {
		run := func() {
			results, err := runtime.TeamWorker.Tick(ctx, 16)
			if err != nil && ctx.Err() == nil {
				log.Printf("team deliberation tick: %v", err)
				return
			}
			for _, result := range results {
				if result.Error != "" {
					log.Printf("team turn %s member %s status=%s: %s", result.TurnID, result.MemberID, result.Status, result.Error)
				}
			}
		}
		run()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	// General autonomous agent execution consumes ordinary READY Tasks. Each
	// attempt is durable and bounded; reasoning may route to a ModelDeployment or
	// an eligible external agent runtime, while all tool side effects remain
	// behind CapabilityLease/ToolGateway/OperationCoordinator enforcement.
	go func() {
		run := func() {
			results, err := runtime.AgentWorker.Tick(ctx, 10)
			if err != nil && ctx.Err() == nil {
				log.Printf("agent worker tick: %v", err)
				return
			}
			for _, result := range results {
				if result.Error != "" && result.Status != "retry_reasoning" && result.Status != "escalating" {
					log.Printf("agent task %s run %s status=%s: %s", result.TaskID, result.RunID, result.Status, result.Error)
				}
			}
		}
		run()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	// Independent assurance evaluates pending V1-V5 completion evidence. It
	// never treats model agreement as verification: deterministic checks, direct
	// observations, independent integration paths and bound human acceptance are
	// required to earn the corresponding levels.
	go func() {
		run := func() {
			results, err := runtime.Assurance.Tick(ctx, 20)
			if err != nil && ctx.Err() == nil {
				log.Printf("assurance worker tick: %v", err)
				return
			}
			for _, result := range results {
				if result.Status == "failed" || result.Status == "inconclusive" {
					log.Printf("assurance verification %s status=%s: %s", result.VerificationID, result.Status, result.Message)
				}
			}
		}
		run()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	// Messaging gateway delivery is a durable control-plane worker. It materializes
	// notification rules from the append-only Event Ledger and sends each delivery
	// through CapabilityLease -> OperationCoordinator -> gateway.send -> V1 receipt
	// verification. Unknown transport outcomes are never blindly replayed.
	go func() {
		run := func() {
			results, err := runtime.Gateway.Tick(ctx, 20)
			if err != nil && ctx.Err() == nil {
				log.Printf("gateway delivery tick: %v", err)
				return
			}
			for _, result := range results {
				if result.Status == "failed" || result.Status == "unknown" {
					msg := ""
					if result.LastError != nil {
						msg = *result.LastError
					}
					log.Printf("gateway delivery %s status=%s: %s", result.ID, result.Status, msg)
				}
			}
		}
		run()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	// Routine execution is a separate deterministic worker. It only consumes
	// materialized project app-command Tasks and still goes through ToolGateway,
	// CapabilityLease, Observation, Verification and Checkpoint.
	go func() {
		run := func() {
			results, err := runtime.RoutineWorker.Tick(ctx, 20)
			if err != nil && ctx.Err() == nil {
				log.Printf("routine worker tick: %v", err)
				return
			}
			for _, result := range results {
				if result.Status != "complete" {
					log.Printf("routine occurrence %s task %s failed: %s", result.OccurrenceID, result.TaskID, result.Error)
				}
			}
		}
		run()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	if runtime.Federation != nil {
		if cfg.Federation.DiscoveryEnabled {
			go func() {
				d := nodefederation.NewDiscovery(runtime.Federation, cfg.Federation.DiscoveryMulticast, mustPort(cfg.Federation.Listen))
				if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
					log.Printf("node LAN discovery stopped: %v", err)
				}
			}()
		}
		go func() {
			run := func() {
				if err := runtime.Federation.HeartbeatPeers(ctx); err != nil && ctx.Err() == nil {
					log.Printf("node federation heartbeat: %v", err)
				}
				if n, err := runtime.Federation.ExpireStale(ctx); err != nil && ctx.Err() == nil {
					log.Printf("node federation stale check: %v", err)
				} else if n > 0 {
					log.Printf("node federation marked %d peer(s) unavailable", n)
				}
			}
			run()
			ticker := time.NewTicker(time.Duration(cfg.Federation.HeartbeatSeconds) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					run()
				}
			}
		}()
	}

	type serverResult struct {
		name string
		err  error
	}
	serverCount := 2
	if federationHTTPServer != nil {
		serverCount++
	}
	serverErr := make(chan serverResult, serverCount)
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- serverResult{name: "control-plane", err: err}
			return
		}
		serverErr <- serverResult{name: "control-plane"}
	}()
	go func() {
		err := previewHTTPServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- serverResult{name: "preview", err: err}
			return
		}
		serverErr <- serverResult{name: "preview"}
	}()
	if federationHTTPServer != nil {
		go func() {
			err := federationHTTPServer.ListenAndServeTLS("", "")
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- serverResult{name: "federation", err: err}
				return
			}
			serverErr <- serverResult{name: "federation"}
		}()
	}

	fmt.Printf("harnessd initialized: mode=%s revision=%d node=%s api=%s preview=%s federation=%t\n", state.Mode, state.Revision, n.ID, cfg.Server.Listen, cfg.Server.PreviewListen, federationHTTPServer != nil)

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 48*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		if err := previewHTTPServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("preview http shutdown: %v", err)
		}
		if federationHTTPServer != nil {
			if err := federationHTTPServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("federation http shutdown: %v", err)
			}
		}
		if err := runtime.LocalAI.ShutdownManagedRuntimes(shutdownCtx); err != nil {
			log.Printf("managed local AI shutdown: %v", err)
		}
		if err := runtime.LocalAI.StopManagedLLMFit(shutdownCtx); err != nil {
			log.Printf("managed llmfit shutdown: %v", err)
		}
		for i := 0; i < serverCount; i++ {
			<-serverErr
		}
	case result := <-serverErr:
		if result.err != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = httpServer.Shutdown(shutdownCtx)
			_ = previewHTTPServer.Shutdown(shutdownCtx)
			if federationHTTPServer != nil {
				_ = federationHTTPServer.Shutdown(shutdownCtx)
			}
			_ = runtime.LocalAI.ShutdownManagedRuntimes(shutdownCtx)
			_ = runtime.LocalAI.StopManagedLLMFit(shutdownCtx)
			log.Fatalf("%s http server: %v", result.name, result.err)
		}
	}
}

func mustPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 18443
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return 18443
	}
	return n
}
