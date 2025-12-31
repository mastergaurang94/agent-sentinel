package main

import (
	"bufio"
	"bytes"
	"encoding/json"
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

func extractPrompt(messages []map[string]interface{}) string {
	for _, msg := range messages {
		if role, ok := msg["role"].(string); ok && role == "user" {
			if content, ok := msg["content"].(string); ok {
				return content
			}
		}
	}
	// Fallback: get first message content if no user message found
	if len(messages) > 0 {
		if content, ok := messages[0]["content"].(string); ok {
			return content
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

		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		// Restore the body for the proxy
		r.Body = io.NopCloser(bytes.NewReader(body))

		// Try to parse as JSON to extract model and prompt
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err == nil {
			model, _ := data["model"].(string)
			var prompt string

			// Try chat completion format (messages array)
			if messages, ok := data["messages"].([]interface{}); ok {
				msgMaps := make([]map[string]interface{}, 0, len(messages))
				for _, m := range messages {
					if msgMap, ok := m.(map[string]interface{}); ok {
						msgMaps = append(msgMaps, msgMap)
					}
				}
				prompt = extractPrompt(msgMaps)
			} else if p, ok := data["prompt"].(string); ok {
				// Try completion format (prompt field)
				prompt = p
			}

			if model != "" {
				log.Printf("Model: %s, Prompt: %s", model, prompt)
			}
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	// Load .env file if it exists (ignore error if file doesn't exist)
	_ = loadEnvFile(".env")

	// Get GEMINI API key from environment variable
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is not set")
	}

	// Parse the target URL
	targetURL, err := url.Parse("https://generativelanguage.googleapis.com")
	if err != nil {
		log.Fatalf("Error parsing target URL: %v", err)
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Modify the request to add the API key as a query parameter
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Gemini API uses the key as a query parameter
		q := req.URL.Query()
		q.Set("key", apiKey)
		req.URL.RawQuery = q.Encode()
		req.Host = targetURL.Host
	}

	// Wrap proxy with logging middleware
	handler := loggingMiddleware(proxy)

	// Start server
	port := ":8080"
	log.Printf("Agent Sentinel proxy listening on port %s", port)
	log.Printf("Forwarding requests to %s", targetURL.String())

	if err := http.ListenAndServe(port, handler); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
