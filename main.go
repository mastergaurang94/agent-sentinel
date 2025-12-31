package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
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
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, `"'`)
			// Only set if not already in environment
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}
	return scanner.Err()
}

func extractModelFromPath(path string) string {
	// Handle paths like: /v1beta/models/gemini-pro:generateContent
	// or: /v1/models/gpt-4/chat/completions

	// Look for "/models/" in the path
	modelsIndex := strings.Index(path, "/models/")
	if modelsIndex == -1 {
		return ""
	}

	// Get everything after "/models/"
	afterModels := path[modelsIndex+8:] // 8 = len("/models/")

	// Split by "/" or ":" to get just the model name
	// e.g., "gemini-pro:generateContent" -> "gemini-pro"
	parts := strings.FieldsFunc(afterModels, func(r rune) bool {
		return r == '/' || r == ':'
	})

	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func extractPromptFromGeminiContents(data map[string]interface{}) string {
	// Gemini uses: {"contents": [{"parts": [{"text": "..."}]}]}
	if contents, ok := data["contents"].([]interface{}); ok && len(contents) > 0 {
		if firstContent, ok := contents[0].(map[string]interface{}); ok {
			if parts, ok := firstContent["parts"].([]interface{}); ok && len(parts) > 0 {
				if firstPart, ok := parts[0].(map[string]interface{}); ok {
					if text, ok := firstPart["text"].(string); ok {
						return text
					}
				}
			}
		}
	}
	return ""
}

func extractPromptFromOpenAIMessages(data map[string]interface{}) string {
	// OpenAI uses: {"messages": [{"role": "user", "content": "..."}]}
	if messages, ok := data["messages"].([]interface{}); ok {
		msgMaps := make([]map[string]interface{}, 0, len(messages))
		for _, m := range messages {
			if msgMap, ok := m.(map[string]interface{}); ok {
				msgMaps = append(msgMaps, msgMap)
			}
		}

		// Look for user message first
		for _, msg := range msgMaps {
			if role, ok := msg["role"].(string); ok && role == "user" {
				if content, ok := msg["content"].(string); ok {
					return content
				}
			}
		}

		// Fallback: get first message content if no user message found
		if len(msgMaps) > 0 {
			if content, ok := msgMaps[0]["content"].(string); ok {
				return content
			}
		}
	}
	return ""
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only log for POST requests (API calls)
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		// Extract model from URL path first (Gemini puts model in path)
		model := extractModelFromPath(r.URL.Path)

		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		// Restore the body for the proxy
		r.Body = io.NopCloser(bytes.NewReader(body))

		// Try to parse as JSON to extract prompt
		var prompt string
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err == nil {
			// Try to get model from body if not found in path
			if model == "" {
				if m, ok := data["model"].(string); ok {
					model = m
				}
			}

			// Try different request formats in order
			// 1. Gemini format (contents array)
			prompt = extractPromptFromGeminiContents(data)

			// 2. OpenAI chat completion format (messages array)
			if prompt == "" {
				prompt = extractPromptFromOpenAIMessages(data)
			}

			// 3. Completion format (prompt field)
			if prompt == "" {
				if p, ok := data["prompt"].(string); ok {
					prompt = p
				}
			}
		}

		// Log if we have a model (from path or body)
		if model != "" {
			log.Printf("Model: %s, Prompt: %s", model, prompt)
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	// Load .env file if it exists (ignore error if file doesn't exist)
	_ = loadEnvFile(".env")

	// Determine which API to use
	targetAPI := os.Getenv("TARGET_API")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	openAIKey := os.Getenv("OPENAI_API_KEY")

	var targetURL *url.URL
	var apiKey string
	var apiName string

	// Determine target API: check TARGET_API env var first, then which key is available
	if targetAPI == "openai" || (targetAPI == "" && openAIKey != "" && geminiKey == "") {
		// Use OpenAI
		if openAIKey == "" {
			log.Fatal("OPENAI_API_KEY environment variable is not set")
		}
		apiKey = openAIKey
		apiName = "OpenAI"
		var err error
		targetURL, err = url.Parse("https://api.openai.com")
		if err != nil {
			log.Fatalf("Error parsing target URL: %v", err)
		}
	} else {
		// Default to Gemini
		if geminiKey == "" {
			log.Fatal("GEMINI_API_KEY environment variable is not set")
		}
		apiKey = geminiKey
		apiName = "Gemini"
		var err error
		targetURL, err = url.Parse("https://generativelanguage.googleapis.com")
		if err != nil {
			log.Fatalf("Error parsing target URL: %v", err)
		}
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Modify the request to add the API key
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host

		if apiName == "OpenAI" {
			// OpenAI uses Bearer token in Authorization header
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
		} else {
			// Gemini API uses the key as a query parameter
			q := req.URL.Query()
			q.Set("key", apiKey)
			req.URL.RawQuery = q.Encode()
		}
	}

	// Wrap proxy with logging middleware
	handler := loggingMiddleware(proxy)

	// Start server
	port := ":8080"
	log.Printf("Agent Sentinel proxy listening on port %s", port)
	log.Printf("Forwarding requests to %s (%s)", targetURL.String(), apiName)

	if err := http.ListenAndServe(port, handler); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
