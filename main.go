package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent-sentinel/internal/async"
	"agent-sentinel/internal/config"
	"agent-sentinel/internal/handlers"
	"agent-sentinel/internal/loopdetect"
	"agent-sentinel/internal/middleware"
	"agent-sentinel/internal/providers"
	"agent-sentinel/internal/providers/anthropic"
	"agent-sentinel/internal/providers/gemini"
	"agent-sentinel/internal/providers/openai"
	"agent-sentinel/internal/ratelimit"
	"agent-sentinel/internal/telemetry"
)

// initProvider initializes the LLM provider based on TARGET_API env var or auto-detection.
func initProvider() providers.Provider {
	targetAPI := strings.ToLower(os.Getenv("TARGET_API"))
	openAIKey := os.Getenv("OPENAI_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	switch targetAPI {
	case "openai":
		return mustInitOpenAI(openAIKey)
	case "anthropic":
		return mustInitAnthropic(anthropicKey)
	case "gemini":
		return mustInitGemini(geminiKey)
	default:
		// Auto-detect based on available keys (backwards compatible)
		if geminiKey != "" {
			return mustInitGemini(geminiKey)
		}
		if openAIKey != "" && anthropicKey == "" {
			return mustInitOpenAI(openAIKey)
		}
		slog.Error("TARGET_API not set and no API key detected. Set TARGET_API to 'openai', 'gemini', or 'anthropic'")
		os.Exit(1)
		return nil
	}
}

func mustInitOpenAI(apiKey string) providers.Provider {
	if apiKey == "" {
		slog.Error("OPENAI_API_KEY environment variable is not set")
		os.Exit(1)
	}
	p, err := openai.New(apiKey)
	if err != nil {
		slog.Error("Failed to init OpenAI provider", "error", err)
		os.Exit(1)
	}
	return p
}

func mustInitAnthropic(apiKey string) providers.Provider {
	if apiKey == "" {
		slog.Error("ANTHROPIC_API_KEY environment variable is not set")
		os.Exit(1)
	}
	p, err := anthropic.New(apiKey)
	if err != nil {
		slog.Error("Failed to init Anthropic provider", "error", err)
		os.Exit(1)
	}
	return p
}

func mustInitGemini(apiKey string) providers.Provider {
	if apiKey == "" {
		slog.Error("GEMINI_API_KEY environment variable is not set")
		os.Exit(1)
	}
	p, err := gemini.New(apiKey)
	if err != nil {
		slog.Error("Failed to init Gemini provider", "error", err)
		os.Exit(1)
	}
	return p
}

// initRateLimiter initializes rate limiting via Redis if available.
// Returns nil if Redis is unavailable or initialization fails.
func initRateLimiter() *ratelimit.RateLimiter {
	redisClient := ratelimit.NewRedisClient()
	if redisClient == nil {
		slog.Info("Rate limiting disabled (Redis not available)")
		return nil
	}

	rl := ratelimit.NewRateLimiter(redisClient)
	if rl == nil {
		slog.Info("Rate limiting disabled (RateLimiter initialization failed)")
		return nil
	}

	slog.Info("Rate limiting enabled via Redis")
	return rl
}

// initLoopClient initializes the loop detection gRPC client.
// Returns nil if initialization fails (fail-open).
func initLoopClient() *loopdetect.Client {
	loopUDS := os.Getenv("LOOP_EMBEDDING_SIDECAR_UDS")
	if loopUDS == "" {
		loopUDS = "/sockets/embedding-sidecar.sock"
	}

	loopTimeoutMs := 1000
	if v := os.Getenv("LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			loopTimeoutMs = parsed
		}
	}

	client, err := loopdetect.New(loopUDS, time.Duration(loopTimeoutMs)*time.Millisecond)
	if err != nil {
		slog.Warn("Loop detection client init failed (fail-open)", "error", err)
		return nil
	}

	slog.Info("Loop detection enabled", "uds", loopUDS, "timeout_ms", loopTimeoutMs)
	return client
}

func main() {
	config.ConfigureLogging()
	_ = config.LoadEnvFile(".env")

	// Initialize async operations (semaphore + completion tracking)
	async.Init()

	// Initialize OpenTelemetry tracing (optional, based on env)
	shutdownTracing := telemetry.InitTracing()
	telemetry.RegisterRuntimeGauges(async.QueueDepth)

	// Initialize components
	rateLimiter := initRateLimiter()
	provider := initProvider()
	loopClient := initLoopClient()

	// Configure reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(provider.BaseURL())
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		provider.PrepareRequest(req)
	}
	proxy.Transport = telemetry.NewInstrumentedTransport(provider, proxy.Transport)
	proxy.ModifyResponse = handlers.CreateModifyResponse(rateLimiter, provider)
	proxy.ErrorHandler = handlers.CreateErrorHandler(rateLimiter)

	// Configure middleware
	rateLimitHeader := os.Getenv("RATE_LIMIT_HEADER")
	if rateLimitHeader == "" {
		rateLimitHeader = "X-Tenant-ID"
	}
	loopHint := os.Getenv("LOOP_INTERVENTION_HINT")
	if loopHint == "" {
		loopHint = "System: break the loop and respond with a new approach."
	}

	// Build middleware chain (order: tracing -> rate limiting -> loop detection -> logging -> proxy)
	var handler http.Handler = proxy
	handler = middleware.Logging(provider, handler)
	if loopClient != nil {
		handler = middleware.LoopDetection(loopClient, provider, rateLimitHeader, loopHint)(handler)
	}
	if rateLimiter != nil {
		handler = middleware.RateLimiting(rateLimiter, provider, rateLimitHeader)(handler)
	}
	handler = telemetry.Middleware(provider, handler)

	// Start server
	port := ":8080"
	slog.Info("Agent Sentinel proxy started",
		"port", port,
		"target_api", provider.Name(),
		"target_url", provider.BaseURL().String(),
	)

	server := &http.Server{Addr: port, Handler: handler}
	go gracefulShutdown(server, shutdownTracing)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed to start", "error", err, "port", port)
		os.Exit(1)
	}
}

func gracefulShutdown(server *http.Server, shutdownTracing func(context.Context) error) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	slog.Info("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("Server shutdown error", "error", err)
	}

	slog.Info("Waiting for in-flight operations to complete...")
	remaining := async.Wait(shutdownCtx)
	if remaining > 0 {
		slog.Warn("Some async operations did not complete", "remaining", remaining)
	} else {
		slog.Info("All async operations completed")
	}

	if err := shutdownTracing(shutdownCtx); err != nil {
		slog.Warn("Tracing shutdown error", "error", err)
	}

	slog.Info("Shutdown complete")
}
