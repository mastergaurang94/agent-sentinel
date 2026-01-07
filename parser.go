package main

import (
	"strings"
)

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
