package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agent-sentinel/ratelimit"
)

func loadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}
	return scanner.Err()
}

func configureLogging() {
	logLevel := slog.LevelInfo
	if levelStr := os.Getenv("LOG_LEVEL"); levelStr != "" {
		switch strings.ToLower(levelStr) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: false,
	}

	jsonHandler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(jsonHandler)
	slog.SetDefault(logger)
}

func main() {
	configureLogging()
	_ = loadEnvFile(".env")

	// Initialize async operations (semaphore + completion tracking)
	initAsyncOps()

	// Initialize OpenTelemetry tracing (optional, based on env)
	shutdownTracing := initTracing()

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

	proxy.ModifyResponse = createModifyResponse(rateLimiter)
	proxy.ErrorHandler = createErrorHandler(rateLimiter)

	rateLimitHeader := os.Getenv("RATE_LIMIT_HEADER")
	if rateLimitHeader == "" {
		rateLimitHeader = "X-Tenant-ID"
	}

	// Build middleware chain (order: tracing -> rate limiting -> logging -> proxy)
	var handler http.Handler = proxy
	handler = loggingMiddleware(handler)
	handler = rateLimitingMiddleware(rateLimiter, strings.ToLower(apiName), rateLimitHeader)(handler)
	handler = tracingMiddleware(handler)

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
		remaining := waitForAsyncOps(shutdownCtx)
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
