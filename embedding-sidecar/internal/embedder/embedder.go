package embedder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

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
	maskTensor, err := onnxruntime_go.NewTensor[int64](onnxruntime_go.Shape{1, int64(len(attentionMask))}, attentionMask)
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}

	inputNames := []string{"input_ids", "attention_mask"}
	outputNames := []string{e.outputName}
	outputBuffer := make([]float32, e.dim)
	outputTensor, err := onnxruntime_go.NewTensor[float32](onnxruntime_go.Shape{1, int64(e.dim)}, outputBuffer)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, fmt.Errorf("create output tensor: %w", err)
	}

	inputVals := []onnxruntime_go.Value{inputTensor, maskTensor}
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
	if len(data) != e.dim {
		err := fmt.Errorf("unexpected embedding dim: got %d want %d", len(data), e.dim)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, err
	}
	return data, nil
}

func Warmup(embedder Embedding) error {
	_, err := embedder.Compute("warmup")
	return err
}

// --- tokenizer ---

type wordpieceTokenizer struct {
	vocab     map[string]int
	maxSeqLen int
	unkID     int
	clsID     int
	sepID     int
	padID     int
	lowercase bool
}

func loadWordpieceTokenizer(vocabPath string, maxSeqLen int) (*wordpieceTokenizer, error) {
	data, err := os.ReadFile(vocabPath)
	if err != nil {
		return nil, err
	}
	vocab := make(map[string]int)
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		vocab[line] = i
	}
	wpt := &wordpieceTokenizer{
		vocab:     vocab,
		maxSeqLen: maxSeqLen,
		unkID:     vocabOr(vocab, "[UNK]", 0),
		clsID:     vocabOr(vocab, "[CLS]", 101),
		sepID:     vocabOr(vocab, "[SEP]", 102),
		padID:     vocabOr(vocab, "[PAD]", 0),
		lowercase: true,
	}
	return wpt, nil
}

func (t *wordpieceTokenizer) Encode(text string) ([]int64, []int64) {
	if t.lowercase {
		text = strings.ToLower(text)
	}
	tokens := basicTokenize(text)
	var wordpieces []int
	for _, tok := range tokens {
		wp := t.wordpiece(tok)
		wordpieces = append(wordpieces, wp...)
	}
	// Add specials, truncate, pad
	ids := []int{t.clsID}
	ids = append(ids, wordpieces...)
	ids = append(ids, t.sepID)
	if len(ids) > t.maxSeqLen {
		ids = ids[:t.maxSeqLen]
	}
	attn := make([]int, len(ids))
	for i := range attn {
		attn[i] = 1
	}
	for len(ids) < t.maxSeqLen {
		ids = append(ids, t.padID)
		attn = append(attn, 0)
	}
	return sliceToInt64(ids), sliceToInt64(attn)
}

func (t *wordpieceTokenizer) wordpiece(token string) []int {
	if _, ok := t.vocab[token]; ok {
		return []int{t.vocab[token]}
	}
	var pieces []int
	runes := []rune(token)
	for len(runes) > 0 {
		end := len(runes)
		var cur string
		for end > 0 {
			sub := string(runes[:end])
			if len(pieces) > 0 {
				sub = "##" + sub
			}
			if id, ok := t.vocab[sub]; ok {
				cur = sub
				pieces = append(pieces, id)
				runes = runes[end:]
				break
			}
			end--
		}
		if cur == "" {
			return []int{t.unkID}
		}
	}
	return pieces
}

func basicTokenize(text string) []string {
	var tokens []string
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			tokens = append(tokens, sb.String())
			sb.Reset()
		}
	}
	isWord := func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
		// keep hyphen/underscore/apostrophe inside words (e.g., mid-token)
		return r == '-' || r == '_' || r == '\''
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flush()
		case isWord(r):
			sb.WriteRune(r)
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			flush()
			tokens = append(tokens, string(r))
		default:
			flush()
			tokens = append(tokens, string(r))
		}
	}
	flush()
	return tokens
}

func vocabOr(v map[string]int, key string, fallback int) int {
	if v == nil {
		return fallback
	}
	if id, ok := v[key]; ok {
		return id
	}
	return fallback
}

func sliceToInt64(src []int) []int64 {
	out := make([]int64, len(src))
	for i, v := range src {
		out[i] = int64(v)
	}
	return out
}
