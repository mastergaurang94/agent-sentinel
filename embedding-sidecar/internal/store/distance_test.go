package store

import "testing"

func TestDistanceToSimilarity(t *testing.T) {
	tests := []struct {
		dist float64
		want float64
	}{
		{0, 1},
		{0.1, 0.95},
		{1.0, 0.5},
		{2.0, 0},
		{2.5, 0},
	}
	for _, tt := range tests {
		if got := distanceToSimilarity(tt.dist); got != tt.want {
			t.Fatalf("distanceToSimilarity(%v) = %v, want %v", tt.dist, got, tt.want)
		}
	}
}
