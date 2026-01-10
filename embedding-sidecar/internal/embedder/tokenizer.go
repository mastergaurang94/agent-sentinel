package embedder

import (
	"os"
	"strings"
	"unicode"
)

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
