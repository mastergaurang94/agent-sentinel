package ratelimit

import (
	"log/slog"
	"strings"

	"github.com/tiktoken-go/tokenizer"
)

// CountTokens estimates the number of tokens in the given text
// Uses tiktoken with model-specific encoding when possible
func CountTokens(text, model string) int {
	if text == "" {
		return 0
	}

	// Try to get model-specific encoder for OpenAI models
	enc, err := getEncoderForModel(model)
	if err != nil {
		slog.Debug("Using default encoder for model",
			"model", model,
			"reason", err.Error(),
		)
		// Fallback to cl100k_base which works well for most modern models
		enc, err = tokenizer.Get(tokenizer.Cl100kBase)
		if err != nil {
			slog.Warn("Failed to load tokenizer, using character estimation",
				"error", err,
			)
			return estimateInputTokensByChars(text)
		}
	}

	ids, _, err := enc.Encode(text)
	if err != nil {
		slog.Warn("Failed to encode text, using character estimation",
			"error", err,
		)
		return estimateInputTokensByChars(text)
	}

	return len(ids)
}

// getEncoderForModel attempts to get the appropriate tokenizer encoder for a model
func getEncoderForModel(model string) (tokenizer.Codec, error) {
	// Normalize model name
	model = strings.ToLower(model)

	// Try direct model match first (for OpenAI models)
	if enc, err := tokenizer.ForModel(tokenizer.Model(model)); err == nil {
		return enc, nil
	}

	// Map common model prefixes to encodings
	switch {
	// OpenAI O-series and GPT-4o use o200k_base
	case strings.HasPrefix(model, "o1"),
		strings.HasPrefix(model, "o3"),
		strings.HasPrefix(model, "o4"),
		strings.HasPrefix(model, "gpt-4o"),
		strings.HasPrefix(model, "gpt-5"),
		strings.HasPrefix(model, "gpt-4.1"):
		return tokenizer.Get(tokenizer.O200kBase)

	// GPT-4 and GPT-3.5 use cl100k_base
	case strings.HasPrefix(model, "gpt-4"),
		strings.HasPrefix(model, "gpt-3.5"):
		return tokenizer.Get(tokenizer.Cl100kBase)

	// Gemini models - use cl100k_base as a reasonable approximation
	// Gemini uses SentencePiece but cl100k_base provides close-enough estimates
	case strings.HasPrefix(model, "gemini"):
		return tokenizer.Get(tokenizer.Cl100kBase)

	default:
		// Default to cl100k_base for unknown models
		return tokenizer.Get(tokenizer.Cl100kBase)
	}
}

// estimateInputTokensByChars provides a rough input token estimate based on character count.
// Uses ~4 characters per token as a common approximation for English text.
// This is a fallback when tiktoken encoding fails.
func estimateInputTokensByChars(text string) int {
	return (len(text) + 3) / 4 // +3 for rounding up
}

const (
	OutputMultiplier  = 10   // Assume output is 10x input when unknown
	MinOutputEstimate = 100  // Minimum output tokens to estimate
	MaxOutputEstimate = 4096 // Cap estimate to avoid over-blocking
)

// EstimateOutputTokens estimates the number of output tokens for cost calculation.
// Uses maxFromRequest if specified, otherwise applies a multiplier with floor/ceiling.
func EstimateOutputTokens(inputTokens, maxFromRequest int) int {
	if maxFromRequest > 0 {
		if maxFromRequest > MaxOutputEstimate {
			return MaxOutputEstimate
		}
		return maxFromRequest
	}

	estimated := inputTokens * OutputMultiplier
	if estimated < MinOutputEstimate {
		return MinOutputEstimate
	}
	if estimated > MaxOutputEstimate {
		return MaxOutputEstimate
	}
	return estimated
}

// ExtractMaxOutputTokens extracts the max output tokens from an API request body.
// Supports both OpenAI (max_tokens, max_completion_tokens) and Gemini (generationConfig.maxOutputTokens).
func ExtractMaxOutputTokens(data map[string]any) int {
	// OpenAI: max_tokens or max_completion_tokens
	if v, ok := data["max_tokens"].(float64); ok && v > 0 {
		return int(v)
	}
	if v, ok := data["max_completion_tokens"].(float64); ok && v > 0 {
		return int(v)
	}

	// Gemini: generationConfig.maxOutputTokens
	if config, ok := data["generationConfig"].(map[string]any); ok {
		if v, ok := config["maxOutputTokens"].(float64); ok && v > 0 {
			return int(v)
		}
	}

	return 0
}
