package embedder

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"embedding-sidecar/internal/telemetry"

	"github.com/yalue/onnxruntime_go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Embedding interface {
	Compute(text string) ([]float32, error)
}

var errWarmupFail = errors.New("warmup failed")

type onnxEmbedder struct {
	modelPath  string
	tokenizer  *wordpieceTokenizer
	outputName string
	dim        int
}

const DefaultEmbeddingDim = 384

var runtimeInitOnce sync.Once
var runtimeInitErr error

func NewONNXEmbedder(modelPath string, vocabPath string, outputName string, dim int) (Embedding, error) {
	if modelPath == "" {
		return nil, errors.New("model path not provided")
	}
	if vocabPath == "" {
		return nil, errors.New("vocab path not provided")
	}
	if outputName == "" {
		outputName = "sentence_embedding"
	}
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	tokenizer, err := loadWordpieceTokenizer(vocabPath, 256)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	onnxruntime_go.SetSharedLibraryPath("/usr/local/lib/libonnxruntime.so")
	runtimeInitOnce.Do(func() {
		runtimeInitErr = onnxruntime_go.InitializeEnvironment()
	})
	if runtimeInitErr != nil {
		return nil, fmt.Errorf("init onnx runtime: %w", runtimeInitErr)
	}
	return &onnxEmbedder{
		modelPath:  modelPath,
		tokenizer:  tokenizer,
		outputName: outputName,
		dim:        dim,
	}, nil
}

// Compute runs inference and returns the embedding vector.
func (e *onnxEmbedder) Compute(text string) ([]float32, error) {
	if text == "" {
		return nil, errors.New("empty text")
	}
	ctx := context.Background()
	ctx, span := telemetry.StartSpan(ctx, "embedder.compute",
		attribute.Int("embedder.dim", e.dim),
		attribute.String("embedder.output_name", e.outputName),
	)
	defer span.End()
	start := time.Now()
	result := "ok"
	defer func() {
		telemetry.ObserveEmbedderLatency(ctx, e.dim, e.outputName, result, time.Since(start))
	}()
	inputIDs, attentionMask := e.tokenizer.Encode(text)

	inputTensor, err := onnxruntime_go.NewTensor[int64](onnxruntime_go.Shape{1, int64(len(inputIDs))}, inputIDs)
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	tokenTypeIDs := make([]int64, len(inputIDs)) // all zeros
	typeTensor, err := onnxruntime_go.NewTensor[int64](onnxruntime_go.Shape{1, int64(len(tokenTypeIDs))}, tokenTypeIDs)
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
	}
	maskTensor, err := onnxruntime_go.NewTensor[int64](onnxruntime_go.Shape{1, int64(len(attentionMask))}, attentionMask)
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}

	inputNames := []string{"input_ids", "token_type_ids", "attention_mask"}
	outputNames := []string{e.outputName}
	seqLen := int64(len(attentionMask))
	outputBuffer := make([]float32, int(seqLen)*e.dim)
	outputTensor, err := onnxruntime_go.NewTensor[float32](onnxruntime_go.Shape{1, seqLen, int64(e.dim)}, outputBuffer)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, fmt.Errorf("create output tensor: %w", err)
	}

	inputVals := []onnxruntime_go.Value{inputTensor, typeTensor, maskTensor}
	outputVals := []onnxruntime_go.Value{outputTensor}
	session, err := onnxruntime_go.NewAdvancedSession(e.modelPath, inputNames, outputNames, inputVals, outputVals, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, fmt.Errorf("create onnx session: %w", err)
	}
	defer session.Destroy()

	if err := session.Run(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, fmt.Errorf("onnx run: %w", err)
	}

	data := outputTensor.GetData()
	if len(data) != int(seqLen)*e.dim {
		err := fmt.Errorf("unexpected output size: got %d want %d", len(data), int(seqLen)*e.dim)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, err
	}

	// Mean pooling using attention mask to produce a sentence embedding.
	var pooled = make([]float32, e.dim)
	var count int
	for i := int64(0); i < seqLen; i++ {
		if attentionMask[i] == 0 {
			continue
		}
		count++
		base := int(i) * e.dim
		for d := 0; d < e.dim; d++ {
			pooled[d] += data[base+d]
		}
	}
	if count == 0 {
		err := errors.New("no tokens after attention masking")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, err
	}
	scale := float32(1.0 / float32(count))
	for d := 0; d < e.dim; d++ {
		pooled[d] *= scale
	}
	return pooled, nil
}

func Warmup(embedder Embedding) error {
	_, err := embedder.Compute("warmup")
	return err
}
