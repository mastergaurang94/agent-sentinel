package parser

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	Found        bool
}

func HasErrorInResponse(data map[string]any) bool {
	_, ok := data["error"]
	return ok
}
