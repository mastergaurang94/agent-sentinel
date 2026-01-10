package embedder

import (
	"testing"
)

func TestMeanPoolHappyPath(t *testing.T) {
	dim := 3
	attn := []int64{1, 0, 1}
	data := []float32{
		1, 2, 3, // token 0
		9, 9, 9, // masked out
		4, 6, 8, // token 2
	}
	got, err := meanPool(data, attn, dim)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []float32{
		(1 + 4) / 2.0,
		(2 + 6) / 2.0,
		(3 + 8) / 2.0,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dim %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestMeanPoolErrors(t *testing.T) {
	_, err := meanPool([]float32{1, 2}, []int64{1, 0}, 0)
	if err == nil {
		t.Fatalf("expected dim error")
	}
	_, err = meanPool([]float32{1, 2}, []int64{1, 0}, 3)
	if err == nil {
		t.Fatalf("expected size error")
	}
	_, err = meanPool([]float32{1, 2}, []int64{0, 0}, 1)
	if err == nil {
		t.Fatalf("expected no tokens error")
	}
}
