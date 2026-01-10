package store

import (
	"reflect"
	"testing"
)

func TestEscapeTagValue(t *testing.T) {
	got := escapeTagValue(`te-nant\id`)
	want := `te\-nant\\id`
	if got != want {
		t.Fatalf("escapeTagValue got %q want %q", got, want)
	}
}

func TestParseSearchArrayResult(t *testing.T) {
	arr := []any{
		int64(2),
		"key1", []any{"prompt", "p1", "score", "0.5"},
		"key2", []any{"prompt", "p2", "score", float64(1.5)},
	}
	records := parseSearchArrayResult(arr, 2)
	want := []EmbeddingRecord{
		{Prompt: "p1", Distance: 0.5, Similarity: distanceToSimilarity(0.5), Key: "key1"},
		{Prompt: "p2", Distance: 1.5, Similarity: distanceToSimilarity(1.5), Key: "key2"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("parseSearchArrayResult got %+v want %+v", records, want)
	}
}

func TestParseSearchMapResult(t *testing.T) {
	m := map[any]any{
		"results": []any{
			map[any]any{
				"id": "key1",
				"extra_attributes": map[any]any{
					"prompt": "p1",
					"score":  "0.25",
				},
			},
		},
	}
	records := parseSearchMapResult(m, 1)
	want := []EmbeddingRecord{
		{Prompt: "p1", Distance: 0.25, Similarity: distanceToSimilarity(0.25), Key: "key1"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("parseSearchMapResult got %+v want %+v", records, want)
	}
}
