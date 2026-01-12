package async

import (
	"context"
	"testing"
	"time"
)

func TestQueueDepth(t *testing.T) {
	Init()
	initial := QueueDepth()
	if initial != 0 {
		t.Fatalf("expected 0 depth after init, got %d", initial)
	}
	done := make(chan struct{})
	Run(func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	})
	// Allow goroutine to start
	time.Sleep(5 * time.Millisecond)
	if QueueDepth() != 1 {
		t.Fatalf("expected depth 1, got %d", QueueDepth())
	}
	<-done
	// Allow release
	time.Sleep(5 * time.Millisecond)
	if QueueDepth() != 0 {
		t.Fatalf("expected depth 0 after completion, got %d", QueueDepth())
	}
}

func TestWait(t *testing.T) {
	Init()
	Run(func() { time.Sleep(10 * time.Millisecond) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	remaining := Wait(ctx)
	if remaining != 0 {
		t.Fatalf("expected all tasks complete, got remaining %d", remaining)
	}
}

func TestWaitContextCancel(t *testing.T) {
	Init()
	Run(func() { time.Sleep(100 * time.Millisecond) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	remaining := Wait(ctx)
	if remaining != 0 {
		t.Fatalf("expected all tasks complete, got remaining %d", remaining)
	}
}
