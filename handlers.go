package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"agent-sentinel/ratelimit"
)

var (
	// asyncSemaphore limits concurrent async Redis operations (semaphore pattern)
	asyncSemaphore chan struct{}

	// asyncCompletion tracks completion of async operations for graceful shutdown
	asyncCompletion chan struct{}
)

func initAsyncOps() {
	limit := 10000 // Default limit - large enough to handle high traffic
	if limitStr := os.Getenv("ASYNC_OP_LIMIT"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	asyncSemaphore = make(chan struct{}, limit)
	asyncCompletion = make(chan struct{}, limit*2) // Buffer for completion signals

	slog.Info("Async operations initialized",
		"concurrent_limit", limit,
	)
}

// runAsyncOp runs an async operation with semaphore backpressure control.
// Uses wait-in-line strategy: always waits for semaphore to ensure 100% data integrity.
// The blocking happens in the async goroutine, not the main request path, so proxy latency is unaffected.
func runAsyncOp(fn func()) {
	go func() {
		// Wait in line for semaphore - ensures cost tracking is never dropped
		// This blocks the async goroutine, not the main request handler
		asyncSemaphore <- struct{}{} // Acquire semaphore (blocks if full, but with 10k limit this is rare)

		defer func() {
			<-asyncSemaphore // Release semaphore
			select {
			case asyncCompletion <- struct{}{}: // Signal completion
			default:
				// Channel full (shouldn't happen), but don't block
			}
		}()

		fn()
	}()
}

// waitForAsyncOps waits for all in-flight async operations to complete
// Returns the number of operations that were in-flight
func waitForAsyncOps(ctx context.Context) int {
	// Count how many operations are currently running
	inFlight := len(asyncSemaphore)
	if inFlight == 0 {
		return 0
	}

	// Wait for completion signals
	completed := 0
	for completed < inFlight {
		select {
		case <-asyncCompletion:
			completed++
		case <-ctx.Done():
			return inFlight - completed
		}
	}

	return 0
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	Found        bool
}

func extractModelFromPath(path string) string {
	modelsIndex := strings.Index(path, "/models/")
	if modelsIndex == -1 {
		return ""
	}

	afterModels := path[modelsIndex+8:]
	parts := strings.FieldsFunc(afterModels, func(r rune) bool {
		return r == '/' || r == ':'
	})

	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func extractPromptFromGeminiContents(data map[string]any) string {
	if contents, ok := data["contents"].([]any); ok && len(contents) > 0 {
		if firstContent, ok := contents[0].(map[string]any); ok {
			if parts, ok := firstContent["parts"].([]any); ok && len(parts) > 0 {
				if firstPart, ok := parts[0].(map[string]any); ok {
					if text, ok := firstPart["text"].(string); ok {
						return text
					}
				}
			}
		}
	}
	return ""
}

func extractPromptFromOpenAIResponses(data map[string]any) string {
	if input, ok := data["input"]; ok {
		if inputStr, ok := input.(string); ok {
			return inputStr
		}

		if messages, ok := input.([]any); ok {
			msgMaps := make([]map[string]any, 0, len(messages))
			for _, m := range messages {
				if msgMap, ok := m.(map[string]any); ok {
					msgMaps = append(msgMaps, msgMap)
				}
			}

			for _, msg := range msgMaps {
				if role, ok := msg["role"].(string); ok && role == "user" {
					if content, ok := msg["content"].(string); ok {
						return content
					}
				}
			}

			if len(msgMaps) > 0 {
				if content, ok := msgMaps[0]["content"].(string); ok {
					return content
				}
			}
		}
	}
	return ""
}

func extractFullRequestText(data map[string]any) string {
	var parts []string

	if contents, ok := data["contents"].([]any); ok {
		for _, content := range contents {
			if contentMap, ok := content.(map[string]any); ok {
				if contentParts, ok := contentMap["parts"].([]any); ok {
					for _, part := range contentParts {
						if partMap, ok := part.(map[string]any); ok {
							if text, ok := partMap["text"].(string); ok {
								parts = append(parts, text)
							}
						}
					}
				}
			}
		}
	}

	if input, ok := data["input"]; ok {
		if inputStr, ok := input.(string); ok {
			parts = append(parts, inputStr)
		} else if messages, ok := input.([]any); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]any); ok {
					if content, ok := msgMap["content"].(string); ok {
						parts = append(parts, content)
					}
				}
			}
		}
	}

	if messages, ok := data["messages"].([]any); ok {
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]any); ok {
				if content, ok := msgMap["content"].(string); ok {
					parts = append(parts, content)
				}
			}
		}
	}

	if systemInstruction, ok := data["system_instruction"].(map[string]any); ok {
		if systemParts, ok := systemInstruction["parts"].([]any); ok {
			for _, part := range systemParts {
				if partMap, ok := part.(map[string]any); ok {
					if text, ok := partMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

func parseOpenAITokenUsage(data map[string]any) TokenUsage {
	if usage, ok := data["usage"].(map[string]any); ok {
		var inputTokens, outputTokens int

		if pt, ok := usage["prompt_tokens"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := usage["completion_tokens"].(float64); ok {
			outputTokens = int(ct)
		}

		if inputTokens > 0 || outputTokens > 0 {
			return TokenUsage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				Found:        true,
			}
		}
	}
	return TokenUsage{}
}

func parseGeminiTokenUsage(data map[string]any) TokenUsage {
	if usage, ok := data["usageMetadata"].(map[string]any); ok {
		var inputTokens, outputTokens int

		if pt, ok := usage["promptTokenCount"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := usage["candidatesTokenCount"].(float64); ok {
			outputTokens = int(ct)
		}

		if inputTokens > 0 || outputTokens > 0 {
			return TokenUsage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				Found:        true,
			}
		}
	}
	return TokenUsage{}
}

func parseTokenUsage(data map[string]any, provider string) TokenUsage {
	switch provider {
	case "openai":
		return parseOpenAITokenUsage(data)
	case "gemini":
		return parseGeminiTokenUsage(data)
	default:
		usage := parseOpenAITokenUsage(data)
		if usage.Found {
			return usage
		}
		return parseGeminiTokenUsage(data)
	}
}

func hasErrorInResponse(data map[string]any) bool {
	_, ok := data["error"]
	return ok
}

func isStreamingResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "text/event-stream") ||
		strings.Contains(contentType, "application/x-ndjson") ||
		strings.Contains(contentType, "stream")
}

// streamingResponseReader reads SSE chunks and extracts token usage without buffering
type streamingResponseReader struct {
	reader    io.ReadCloser
	provider  string
	usage     TokenUsage
	buffer    []byte
	hasError  bool
	tenantID  string
	estimate  float64
	pricing   ratelimit.Pricing
	limiter   *ratelimit.RateLimiter
	finalized bool
}

func newStreamingResponseReader(reader io.ReadCloser, provider, tenantID string, estimate float64, pricing ratelimit.Pricing, limiter *ratelimit.RateLimiter) *streamingResponseReader {
	return &streamingResponseReader{
		reader:   reader,
		provider: provider,
		tenantID: tenantID,
		estimate: estimate,
		pricing:  pricing,
		limiter:  limiter,
		buffer:   make([]byte, 0, 4096),
	}
}

func (s *streamingResponseReader) Read(p []byte) (n int, err error) {
	n, err = s.reader.Read(p)
	if n > 0 {
		s.processChunk(p[:n])
	}
	if err == io.EOF && !s.finalized {
		// Stream ended, process any remaining buffer and finalize
		if len(s.buffer) > 0 {
			s.parseSSELine(s.buffer)
		}
		s.finalizeCost()
		s.finalized = true
	}
	return n, err
}

func (s *streamingResponseReader) Close() error {
	if !s.finalized {
		// Process any remaining buffer
		if len(s.buffer) > 0 {
			s.parseSSELine(s.buffer)
		}
		s.finalizeCost()
		s.finalized = true
	}
	return s.reader.Close()
}

func (s *streamingResponseReader) processChunk(data []byte) {
	s.buffer = append(s.buffer, data...)

	// Process complete SSE lines (ending with \n\n or \r\n\r\n)
	for {
		lineEnd := -1
		if idx := bytes.Index(s.buffer, []byte("\n\n")); idx >= 0 {
			lineEnd = idx + 2
		} else if idx := bytes.Index(s.buffer, []byte("\r\n\r\n")); idx >= 0 {
			lineEnd = idx + 4
		} else if idx := bytes.IndexByte(s.buffer, '\n'); idx >= 0 && len(s.buffer) > idx+1 && s.buffer[idx+1] != '\n' {
			// Single newline, might be part of SSE data line
			lineEnd = idx + 1
		}

		if lineEnd < 0 {
			break
		}

		line := s.buffer[:lineEnd]
		s.buffer = s.buffer[lineEnd:]

		s.parseSSELine(line)
	}
}

func (s *streamingResponseReader) parseSSELine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}

	// SSE format: "data: {json}" or "data: [DONE]"
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return
	}

	dataPart := line[6:] // Skip "data: "

	// Check for [DONE] marker
	if bytes.Equal(dataPart, []byte("[DONE]")) {
		s.finalizeCost()
		return
	}

	// Parse JSON
	var chunk map[string]any
	if err := json.Unmarshal(dataPart, &chunk); err != nil {
		return
	}

	// Check for errors
	if _, hasErr := chunk["error"]; hasErr {
		s.hasError = true
	}

	// Extract token usage from chunk
	usage := parseTokenUsage(chunk, s.provider)
	if usage.Found {
		// Accumulate usage (take max in case of multiple usage chunks)
		if usage.InputTokens > s.usage.InputTokens {
			s.usage.InputTokens = usage.InputTokens
		}
		if usage.OutputTokens > s.usage.OutputTokens {
			s.usage.OutputTokens = usage.OutputTokens
		}
		s.usage.Found = true
	}
}

func (s *streamingResponseReader) finalizeCost() {
	if s.limiter == nil {
		return
	}

	runAsyncOp(func() {
		bgCtx := context.Background()
		if s.usage.Found {
			actualCost := ratelimit.CalculateCost(s.usage.InputTokens, s.usage.OutputTokens, s.pricing)
			if err := s.limiter.AdjustCost(bgCtx, s.tenantID, s.estimate, actualCost); err != nil {
				slog.Warn("Failed to adjust cost from streaming response",
					"error", err,
					"tenant_id", s.tenantID,
					"estimate", s.estimate,
					"actual", actualCost,
				)
			} else {
				slog.Debug("Cost adjusted from streaming response",
					"tenant_id", s.tenantID,
					"estimate", s.estimate,
					"actual", actualCost,
					"input_tokens", s.usage.InputTokens,
					"output_tokens", s.usage.OutputTokens,
				)
			}
		} else if s.hasError {
			// Error with no usage - refund estimate
			if err := s.limiter.RefundEstimate(bgCtx, s.tenantID, s.estimate); err != nil {
				slog.Warn("Failed to refund estimate from streaming error",
					"error", err,
					"tenant_id", s.tenantID,
					"estimate", s.estimate,
				)
			} else {
				slog.Debug("Estimate refunded (streaming error with no usage)",
					"tenant_id", s.tenantID,
					"estimate", s.estimate,
				)
			}
		}
		// If no usage found and no error, keep estimate (conservative)
	})
}

func createModifyResponse(limiter *ratelimit.RateLimiter) func(*http.Response) error {
	return func(resp *http.Response) error {
		if limiter == nil {
			return nil
		}

		ctx := resp.Request.Context()
		tenantID, _ := ctx.Value(ctxKeyTenantID).(string)
		estimate, _ := ctx.Value(ctxKeyEstimate).(float64)
		provider, _ := ctx.Value(ctxKeyProvider).(string)
		pricing, _ := ctx.Value(ctxKeyPricing).(ratelimit.Pricing)

		if tenantID == "" || estimate == 0 {
			return nil
		}

		// For streaming responses, use a custom reader that processes chunks as they stream
		// This maintains TTFT while still tracking actual token usage
		if isStreamingResponse(resp) {
			streamReader := newStreamingResponseReader(resp.Body, provider, tenantID, estimate, pricing, limiter)
			resp.Body = streamReader
			slog.Debug("Streaming response detected, using chunk-based cost tracking",
				"tenant_id", tenantID,
				"estimate", estimate,
				"content_type", resp.Header.Get("Content-Type"),
			)
			return nil
		}

		// For non-streaming responses, read body for cost adjustment
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Warn("Failed to read response body for cost tracking",
				"error", err,
				"tenant_id", tenantID,
			)
			return nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			slog.Debug("Response not JSON, keeping estimate",
				"tenant_id", tenantID,
				"content_type", resp.Header.Get("Content-Type"),
			)
			return nil
		}

		isError := hasErrorInResponse(data) || resp.StatusCode >= 400
		usage := parseTokenUsage(data, provider)

		// Run cost adjustment asynchronously to not block response
		runAsyncOp(func() {
			bgCtx := context.Background()
			if usage.Found {
				actualCost := ratelimit.CalculateCost(usage.InputTokens, usage.OutputTokens, pricing)
				if err := limiter.AdjustCost(bgCtx, tenantID, estimate, actualCost); err != nil {
					slog.Warn("Failed to adjust cost",
						"error", err,
						"tenant_id", tenantID,
						"estimate", estimate,
						"actual", actualCost,
					)
				} else {
					slog.Debug("Cost adjusted",
						"tenant_id", tenantID,
						"estimate", estimate,
						"actual", actualCost,
						"input_tokens", usage.InputTokens,
						"output_tokens", usage.OutputTokens,
					)
				}
			} else if isError {
				// Only refund if it's an error AND no tokens were consumed
				if err := limiter.RefundEstimate(bgCtx, tenantID, estimate); err != nil {
					slog.Warn("Failed to refund estimate",
						"error", err,
						"tenant_id", tenantID,
						"estimate", estimate,
					)
				} else {
					slog.Debug("Estimate refunded (error with no usage)",
						"tenant_id", tenantID,
						"estimate", estimate,
						"status_code", resp.StatusCode,
					)
				}
			}
			// If success but no usage found, keep estimate (conservative)
		})

		return nil
	}
}

func createErrorHandler(limiter *ratelimit.RateLimiter) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, proxyErr error) {
		ctx := r.Context()
		tenantID, _ := ctx.Value(ctxKeyTenantID).(string)
		estimate, _ := ctx.Value(ctxKeyEstimate).(float64)

		// Refund asynchronously to not block error response
		if limiter != nil && tenantID != "" && estimate > 0 {
			runAsyncOp(func() {
				bgCtx := context.Background()
				if refundErr := limiter.RefundEstimate(bgCtx, tenantID, estimate); refundErr != nil {
					slog.Warn("Failed to refund estimate on proxy error",
						"error", refundErr,
						"tenant_id", tenantID,
						"estimate", estimate,
					)
				} else {
					slog.Debug("Estimate refunded (proxy error)",
						"tenant_id", tenantID,
						"estimate", estimate,
						"proxy_error", proxyErr.Error(),
					)
				}
			})
		}

		slog.Error("Proxy error",
			"error", proxyErr,
			"tenant_id", tenantID,
		)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}
}
