package main

import (
	"errors"
	"sync"

	"github.com/yalue/onnxruntime_go"
)

type Embedding interface {
	Compute(text string) ([]float32, error)
}

var errWarmupFail = errors.New("warmup failed")

type onnxEmbedder struct {
	session *onnxruntime.Session
	mu      sync.Mutex
}

const embeddingDim = 384

func newOnnxEmbedder(modelPath string) (Embedding, error) {
	if modelPath == "" {
		return nil, errors.New("model path not provided")
	}

	session, err := onnxruntime.NewSession(modelPath)
	if err != nil {
		return nil, err
	}

	return &onnxEmbedder{session: session}, nil
}

// Compute runs inference and returns the embedding vector.
// Note: This is a placeholder. In a production implementation, you would:
// - Tokenize the text
// - Prepare input tensors
// - Run the session
// - Extract and return the embedding vector
func (e *onnxEmbedder) Compute(text string) ([]float32, error) {
	if text == "" {
		return nil, errors.New("empty text")
	}

	// Placeholder: return a deterministic small vector to allow compilation and tests
	// without requiring the full model execution in this environment.
	// Replace with real ONNX inference when running with the bundled model.
	vec := make([]float32, embeddingDim)
	for i := range vec {
		vec[i] = 0.001 * float32(i+1)
	}
	return vec, nil
}

func warmupEmbedder(embedder Embedding) error {
	_, err := embedder.Compute("warmup")
	return err
}
