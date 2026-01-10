package ratelimit

import "testing"

func TestEstimateOutputTokens(t *testing.T) {
	if got := EstimateOutputTokens(10, 0); got != MinOutputEstimate {
		t.Fatalf("expected min output, got %d", got)
	}
	if got := EstimateOutputTokens(1000, 0); got != 4096 {
		t.Fatalf("expected cap to 4096, got %d", got)
	}
	if got := EstimateOutputTokens(10, 50); got != 50 {
		t.Fatalf("expected explicit max override, got %d", got)
	}
}

func TestExtractMaxOutputTokens(t *testing.T) {
	body := map[string]any{
		"max_tokens": float64(10),
	}
	if got := ExtractMaxOutputTokens(body); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
	body = map[string]any{
		"generationConfig": map[string]any{
			"maxOutputTokens": float64(20),
		},
	}
	if got := ExtractMaxOutputTokens(body); got != 20 {
		t.Fatalf("expected 20, got %d", got)
	}
}

func TestCountTokensFallback(t *testing.T) {
	// Simple smoke test that returns >0 for non-empty text.
	if got := CountTokens("hello world", "unknown-model"); got == 0 {
		t.Fatalf("expected token count > 0")
	}
}
