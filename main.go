package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	"agent-sentinel/internal/telemetry"
	"agent-sentinel/ratelimit"
)

func main() {
	config.ConfigureLogging()
	_ = config.LoadEnvFile(".env")

	// Initialize async operations (semaphore + completion tracking)
	async.Init()

	// Initialize OpenTelemetry tracing (optional, based on env)
	shutdownTracing := telemetry.InitTracing()

	redisClient := ratelimit.NewRedisClient()
	var rateLimiter *ratelimit.RateLimiter
	if redisClient != nil {
		rateLimiter = ratelimit.NewRateLimiter(redisClient)
		if rateLimiter != nil {
			slog.Info("Rate limiting enabled via Redis")
		} else {
			slog.Info("Rate limiting disabled (RateLimiter initialization failed)")
		}
	} else {
		slog.Info("Rate limiting disabled (Redis not available)")
	}

	targetAPI := os.Getenv("TARGET_API")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	openAIKey := os.Getenv("OPENAI_API_KEY")

	var targetURL *url.URL
	var apiKey string
	var apiName string

	if targetAPI == "openai" || (targetAPI == "" && openAIKey != "" && geminiKey == "") {
		if openAIKey == "" {
			slog.Error("OPENAI_API_KEY environment variable is not set")
			os.Exit(1)
		}
		apiKey = openAIKey
		apiName = "OpenAI"
		var err error
		targetURL, err = url.Parse("https://api.openai.com")
		if err != nil {
			slog.Error("Failed to parse target URL", "error", err)
			os.Exit(1)
		}
	} else {
		if geminiKey == "" {
			slog.Error("GEMINI_API_KEY environment variable is not set")
			os.Exit(1)
		}
		apiKey = geminiKey
		apiName = "Gemini"
		var err error
		targetURL, err = url.Parse("https://generativelanguage.googleapis.com")
		if err != nil {
			slog.Error("Failed to parse target URL", "error", err)
			os.Exit(1)
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host

		if apiName == "OpenAI" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
		} else {
			q := req.URL.Query()
			q.Set("key", apiKey)
			req.URL.RawQuery = q.Encode()
		}
	}

	proxy.ModifyResponse = handlers.CreateModifyResponse(rateLimiter)
	proxy.ErrorHandler = handlers.CreateErrorHandler(rateLimiter)

	rateLimitHeader := os.Getenv("RATE_LIMIT_HEADER")
	if rateLimitHeader == "" {
		rateLimitHeader = "X-Tenant-ID"
	}

	loopUDS := os.Getenv("LOOP_EMBEDDING_SIDECAR_UDS")
	if loopUDS == "" {
		loopUDS = "/sockets/embedding-sidecar.sock"
	}
	loopTimeoutMs := 50
	if v := os.Getenv("LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			loopTimeoutMs = parsed
		}
	}
	loopHint := os.Getenv("LOOP_INTERVENTION_HINT")
	if loopHint == "" {
		loopHint = "System: break the loop and respond with a new approach."
	}
	var loopClient *loopdetect.Client
	if client, err := loopdetect.New(loopUDS, time.Duration(loopTimeoutMs)*time.Millisecond); err != nil {
		slog.Warn("Loop detection client init failed (fail-open)", "error", err)
	} else {
		loopClient = client
		slog.Info("Loop detection enabled", "uds", loopUDS, "timeout_ms", loopTimeoutMs)
	}

	// Build middleware chain (order: tracing -> rate limiting -> logging -> proxy)
	var handler http.Handler = proxy
	handler = middleware.Logging(handler)
	handler = middleware.LoopDetection(loopClient, rateLimitHeader, loopHint)(handler)
	handler = middleware.RateLimiting(rateLimiter, strings.ToLower(apiName), rateLimitHeader)(handler)
	handler = telemetry.Middleware(handler)

	port := ":8080"
	slog.Info("Agent Sentinel proxy started",
		"port", port,
		"target_api", apiName,
		"target_url", targetURL.String(),
	)

	// Graceful shutdown handling
	server := &http.Server{Addr: port, Handler: handler}
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		slog.Info("Shutting down gracefully...")

		// Stop accepting new connections
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("Server shutdown error", "error", err)
		}

		// Wait for in-flight async operations (Redis cost adjustments, refunds)
		slog.Info("Waiting for in-flight operations to complete...")
		remaining := async.Wait(shutdownCtx)
		if remaining > 0 {
			slog.Warn("Some async operations did not complete",
				"remaining", remaining,
			)
		} else {
			slog.Info("All async operations completed")
		}

		// Shutdown OpenTelemetry
		if err := shutdownTracing(shutdownCtx); err != nil {
			slog.Warn("Tracing shutdown error", "error", err)
		}

		slog.Info("Shutdown complete")
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed to start", "error", err, "port", port)
		os.Exit(1)
	}
}
