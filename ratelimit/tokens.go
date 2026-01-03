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
			return estimateTokensByChars(text)
		}
	}

	ids, _, err := enc.Encode(text)
	if err != nil {
		slog.Warn("Failed to encode text, using character estimation",
			"error", err,
		)
		return estimateTokensByChars(text)
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

// estimateTokensByChars provides a rough token estimate based on character count
// Uses ~4 characters per token as a common approximation for English text
func estimateTokensByChars(text string) int {
	return (len(text) + 3) / 4 // +3 for rounding up
}
