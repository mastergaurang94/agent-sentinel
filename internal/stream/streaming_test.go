package stream

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"agent-sentinel/internal/async"
	"agent-sentinel/internal/ratelimit"
)

type fakeLimiter struct {
	adjustEstimate float64
	adjustActual   float64
	refundEstimate float64
	adjustCh       chan struct{}
	refundCh       chan struct{}
}

func (f *fakeLimiter) AdjustCost(ctx context.Context, tenantID string, estimate, actual float64) error {
	f.adjustEstimate = estimate
	f.adjustActual = actual
	if f.adjustCh != nil {
		select {
		case f.adjustCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeLimiter) RefundEstimate(ctx context.Context, tenantID string, estimate float64) error {
	f.refundEstimate = estimate
	if f.refundCh != nil {
		select {
		case f.refundCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func TestStreamingAdjustsCostOnUsage(t *testing.T) {
	// Simulate SSE stream with one chunk containing usage and a DONE terminator.
	streamData := "data: {\"usage\": {\"prompt_tokens\": 2, \"completion_tokens\": 3}}\n\ndata: [DONE]\n\n"
	lim := &fakeLimiter{}
	lim.adjustCh = make(chan struct{}, 1)
	start := time.Now()
	async.Init()
	reader := NewStreamingResponseReader(io.NopCloser(bytes.NewBufferString(streamData)), func(m map[string]any) TokenUsage {
		if usage, ok := m["usage"].(map[string]any); ok {
			return TokenUsage{
				InputTokens:  int(usage["prompt_tokens"].(float64)),
				OutputTokens: int(usage["completion_tokens"].(float64)),
				Found:        true,
			}
		}
		return TokenUsage{}
	}, "tenant", 1.0, ratelimit.Pricing{InputPrice: 1, OutputPrice: 1}, lim, "prov", "model", start)

	buf := make([]byte, 1024)
	_, _ = reader.Read(buf)
	// finalize via close
	_ = reader.Close()

	select {
	case <-lim.adjustCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for adjust")
	}
	if lim.adjustEstimate != 1.0 || lim.adjustActual == 0 {
		t.Fatalf("expected adjust called, got estimate=%v actual=%v", lim.adjustEstimate, lim.adjustActual)
	}
}

func TestStreamingRefundsOnErrorNoUsage(t *testing.T) {
	streamData := "data: {\"error\": \"boom\"}\n\ndata: [DONE]\n\n"
	lim := &fakeLimiter{}
	lim.refundCh = make(chan struct{}, 1)
	start := time.Now()
	async.Init()
	reader := NewStreamingResponseReader(io.NopCloser(bytes.NewBufferString(streamData)), func(m map[string]any) TokenUsage {
		return TokenUsage{}
	}, "tenant", 2.0, ratelimit.Pricing{InputPrice: 1, OutputPrice: 1}, lim, "prov", "model", start)

	buf := make([]byte, 1024)
	_, _ = reader.Read(buf)
	_ = reader.Close()

	select {
	case <-lim.refundCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for refund")
	}
	if lim.refundEstimate != 2.0 {
		t.Fatalf("expected refund 2.0, got %v", lim.refundEstimate)
	}
}
