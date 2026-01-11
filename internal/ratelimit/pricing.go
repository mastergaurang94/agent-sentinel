package ratelimit

// Pricing represents token pricing for a model
type Pricing struct {
	InputPrice  float64 // Price per 1M tokens
	OutputPrice float64 // Price per 1M tokens
}

// ModelPricing stores pricing for all models
type ModelPricing map[string]Pricing

// ProviderPricing stores pricing per provider
type ProviderPricing map[string]ModelPricing

// GetPricing returns the default pricing configuration
// Pricing verified from official pages as of January 2026
// All prices are per 1M tokens for consistency
// Sources:
//   - OpenAI: https://openai.com/api/pricing
//   - Gemini: https://ai.google.dev/gemini-api/docs/pricing
func GetPricing() ProviderPricing {
	return ProviderPricing{
		"openai": ModelPricing{
			// OpenAI pricing per 1M tokens
			// Source: https://openai.com/api/pricing (verified Jan 2026)

			// GPT-5 series (latest flagship models as of 2026)
			"gpt-5.2": {
				InputPrice:  1.75,
				OutputPrice: 14.00,
			},
			"gpt-5.2-pro": {
				InputPrice:  21.00,
				OutputPrice: 168.00,
			},
			"gpt-5-mini": {
				InputPrice:  0.25,
				OutputPrice: 2.00,
			},

			// GPT-4o series (still available)
			"gpt-4o": {
				InputPrice:  2.50,
				OutputPrice: 10.00,
			},
			"gpt-4o-2024-08-06": {
				InputPrice:  2.50,
				OutputPrice: 10.00,
			},
			"gpt-4o-2024-05-13": {
				InputPrice:  2.50,
				OutputPrice: 10.00,
			},
			"gpt-4o-mini": {
				InputPrice:  0.15,
				OutputPrice: 0.60,
			},
			"gpt-4o-mini-2024-07-18": {
				InputPrice:  0.15,
				OutputPrice: 0.60,
			},

			// GPT-4 Turbo series
			"gpt-4-turbo": {
				InputPrice:  10.00,
				OutputPrice: 30.00,
			},
			"gpt-4-turbo-2024-04-09": {
				InputPrice:  10.00,
				OutputPrice: 30.00,
			},
			"gpt-4-1106-preview": {
				InputPrice:  10.00,
				OutputPrice: 30.00,
			},
			"gpt-4-0125-preview": {
				InputPrice:  10.00,
				OutputPrice: 30.00,
			},

			// GPT-4 base models
			"gpt-4": {
				InputPrice:  30.00,
				OutputPrice: 60.00,
			},
			"gpt-4-32k": {
				InputPrice:  60.00,
				OutputPrice: 120.00,
			},

			// GPT-3.5 Turbo series
			"gpt-3.5-turbo": {
				InputPrice:  0.50,
				OutputPrice: 1.50,
			},
			"gpt-3.5-turbo-0125": {
				InputPrice:  0.50,
				OutputPrice: 1.50,
			},
			"gpt-3.5-turbo-1106": {
				InputPrice:  0.50,
				OutputPrice: 1.50,
			},
			"gpt-3.5-turbo-16k": {
				InputPrice:  3.00,
				OutputPrice: 4.00,
			},

			// O1 series (reasoning models)
			"o1": {
				InputPrice:  5.00,
				OutputPrice: 15.00,
			},
			"o1-mini": {
				InputPrice:  0.30,
				OutputPrice: 1.20,
			},
			"o1-preview": {
				InputPrice:  5.00,
				OutputPrice: 15.00,
			},

			// O3 series
			"o3": {
				InputPrice:  5.00,
				OutputPrice: 15.00,
			},
			"o3-mini": {
				InputPrice:  0.30,
				OutputPrice: 1.20,
			},
		},
		"gemini": ModelPricing{
			// Gemini pricing per 1M tokens (Standard tier, Pay-as-you-go)
			// Source: https://ai.google.dev/gemini-api/docs/pricing (verified Jan 2026)
			// Note: Some models have tiered pricing based on prompt length (<=200k vs >200k tokens)
			// Using the standard tier (<=200k) pricing as default

			// Gemini 3 series (latest as of 2026)
			"gemini-3-pro-preview": {
				InputPrice:  2.00,  // $2.00 per 1M tokens (prompt <= 200k)
				OutputPrice: 12.00, // $12.00 per 1M tokens (includes thinking)
			},
			"gemini-3-flash-preview": {
				InputPrice:  0.50, // $0.50 per 1M tokens (text/image/video)
				OutputPrice: 3.00, // $3.00 per 1M tokens (includes thinking)
			},
			"gemini-3-pro-image-preview": {
				InputPrice:  2.00,  // Same as Gemini 3 Pro for text
				OutputPrice: 12.00, // Text output; image output has separate pricing
			},

			// Gemini 2.5 series
			"gemini-2.5-pro": {
				InputPrice:  1.25,  // $1.25 per 1M tokens (prompt <= 200k)
				OutputPrice: 10.00, // $10.00 per 1M tokens (includes thinking)
			},
			"gemini-2.5-pro-preview": {
				InputPrice:  1.25,
				OutputPrice: 10.00,
			},
			"gemini-2.5-flash": {
				InputPrice:  0.30, // $0.30 per 1M tokens (text/image/video)
				OutputPrice: 2.50, // $2.50 per 1M tokens (includes thinking)
			},
			"gemini-2.5-flash-preview": {
				InputPrice:  0.30,
				OutputPrice: 2.50,
			},
			"gemini-2.5-flash-lite": {
				InputPrice:  0.10, // $0.10 per 1M tokens (text/image/video)
				OutputPrice: 0.40, // $0.40 per 1M tokens (includes thinking)
			},
			"gemini-2.5-flash-lite-preview": {
				InputPrice:  0.10,
				OutputPrice: 0.40,
			},

			// Gemini 2.0 series
			"gemini-2.0-flash": {
				InputPrice:  0.10, // Same as flash-lite pricing
				OutputPrice: 0.40,
			},
			"gemini-2.0-flash-lite": {
				InputPrice:  0.10,
				OutputPrice: 0.40,
			},
			"gemini-2.0-flash-exp": {
				InputPrice:  0.10,
				OutputPrice: 0.40,
			},
			"gemini-2.0-flash-thinking-exp": {
				InputPrice:  0.10,
				OutputPrice: 0.40,
			},

			// Gemini 1.5 series (legacy but still available)
			"gemini-1.5-pro": {
				InputPrice:  1.25,
				OutputPrice: 5.00,
			},
			"gemini-1.5-pro-latest": {
				InputPrice:  1.25,
				OutputPrice: 5.00,
			},
			"gemini-1.5-pro-002": {
				InputPrice:  1.25,
				OutputPrice: 5.00,
			},
			"gemini-1.5-flash": {
				InputPrice:  0.075,
				OutputPrice: 0.30,
			},
			"gemini-1.5-flash-latest": {
				InputPrice:  0.075,
				OutputPrice: 0.30,
			},
			"gemini-1.5-flash-8b": {
				InputPrice:  0.0375,
				OutputPrice: 0.15,
			},

			// Legacy Gemini 1.0 models
			"gemini-pro": {
				InputPrice:  0.50,
				OutputPrice: 1.50,
			},
			"gemini-pro-vision": {
				InputPrice:  0.50,
				OutputPrice: 1.50,
			},
			"gemini-pro-1.0": {
				InputPrice:  0.50,
				OutputPrice: 1.50,
			},
		},
	}
}

// CalculateCost calculates the cost based on input/output tokens and pricing
// All pricing is now normalized to per 1M tokens
func CalculateCost(inputTokens, outputTokens int, pricing Pricing) float64 {
	inputCost := (float64(inputTokens) / 1_000_000.0) * pricing.InputPrice
	outputCost := (float64(outputTokens) / 1_000_000.0) * pricing.OutputPrice
	return inputCost + outputCost
}

// GetModelPricing returns pricing for a specific model, with fallback defaults
// Returns the pricing and a boolean indicating if it was found
func GetModelPricing(provider, model string) (Pricing, bool) {
	pricing := GetPricing()
	if providerPricing, ok := pricing[provider]; ok {
		if modelPricing, ok := providerPricing[model]; ok {
			return modelPricing, true
		}
	}
	return Pricing{}, false
}

// DefaultPricing returns conservative fallback pricing when model is unknown
func DefaultPricing(provider string) Pricing {
	switch provider {
	case "openai":
		// Conservative default based on GPT-4o
		return Pricing{
			InputPrice:  2.50,
			OutputPrice: 10.00,
		}
	case "gemini":
		// Conservative default based on Gemini 1.5 Pro
		return Pricing{
			InputPrice:  1.25,
			OutputPrice: 5.00,
		}
	default:
		// Reasonable fallback based on GPT-4o pricing
		// This balances being protective without being overly restrictive
		// for cheaper models that might not be in our pricing table
		return Pricing{
			InputPrice:  2.50,
			OutputPrice: 10.00,
		}
	}
}
