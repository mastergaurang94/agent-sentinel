package embedder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndEncodeWordpiece(t *testing.T) {
	vocab := strings.TrimSpace(`
[PAD]
[UNK]
[CLS]
[SEP]
hello
world
##s
!
`)
	tmpDir := t.TempDir()
	vocabPath := filepath.Join(tmpDir, "vocab.txt")
	if err := os.WriteFile(vocabPath, []byte(vocab), 0o644); err != nil {
		t.Fatalf("write vocab: %v", err)
	}

	tok, err := loadWordpieceTokenizer(vocabPath, 8)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}

	ids, attn := tok.Encode("Hello worlds!")
	wantIDs := []int64{
		int64(tok.clsID),          // [CLS]
		int64(tok.vocab["hello"]), // hello
		int64(tok.vocab["world"]), // world
		int64(tok.vocab["##s"]),   // ##s
		int64(tok.vocab["!"]),     // !
		int64(tok.sepID),          // [SEP]
		int64(tok.padID),          // pad
		int64(tok.padID),          // pad
	}
	if len(ids) != len(wantIDs) {
		t.Fatalf("ids len mismatch: got %d want %d", len(ids), len(wantIDs))
	}
	for i := range ids {
		if ids[i] != wantIDs[i] {
			t.Fatalf("ids[%d]=%d want %d", i, ids[i], wantIDs[i])
		}
	}
	wantAttn := []int64{1, 1, 1, 1, 1, 1, 0, 0}
	for i := range attn {
		if attn[i] != wantAttn[i] {
			t.Fatalf("attn[%d]=%d want %d", i, attn[i], wantAttn[i])
		}
	}
}

func TestWordpieceUnknownFallsBack(t *testing.T) {
	vocab := strings.TrimSpace(`
[PAD]
[UNK]
[CLS]
[SEP]
hello
`)
	tmpDir := t.TempDir()
	vocabPath := filepath.Join(tmpDir, "vocab.txt")
	if err := os.WriteFile(vocabPath, []byte(vocab), 0o644); err != nil {
		t.Fatalf("write vocab: %v", err)
	}
	tok, err := loadWordpieceTokenizer(vocabPath, 6)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	ids, attn := tok.Encode("zz-top")
	if ids[1] != int64(tok.unkID) { // token after [CLS]
		t.Fatalf("expected unk id, got %d", ids[1])
	}
	if attn[1] != 1 {
		t.Fatalf("attention for unk token should be 1, got %d", attn[1])
	}
}

func TestBasicTokenizeKeepsHyphenUnderscoreApostrophe(t *testing.T) {
	got := basicTokenize("re-use_token's fine!")
	want := []string{"re-use_token's", "fine", "!"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %q want %q", i, got[i], want[i])
		}
	}
}
