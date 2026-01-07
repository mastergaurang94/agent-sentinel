package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"agent-sentinel/ratelimit"
)

func isStreamingResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "text/event-stream") ||
		strings.Contains(contentType, "application/x-ndjson") ||
		strings.Contains(contentType, "stream")
}

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

	for {
		lineEnd := -1
		if idx := bytes.Index(s.buffer, []byte("\n\n")); idx >= 0 {
			lineEnd = idx + 2
		} else if idx := bytes.Index(s.buffer, []byte("\r\n\r\n")); idx >= 0 {
			lineEnd = idx + 4
		} else if idx := bytes.IndexByte(s.buffer, '\n'); idx >= 0 && len(s.buffer) > idx+1 && s.buffer[idx+1] != '\n' {
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

	if !bytes.HasPrefix(line, []byte("data: ")) {
		return
	}

	dataPart := line[6:]

	if bytes.Equal(dataPart, []byte("[DONE]")) {
		s.finalizeCost()
		return
	}

	var chunk map[string]any
	if err := json.Unmarshal(dataPart, &chunk); err != nil {
		return
	}

	if _, hasErr := chunk["error"]; hasErr {
		s.hasError = true
	}

	usage := parseTokenUsage(chunk, s.provider)
	if usage.Found {
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
	})
}
