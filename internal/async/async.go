package async

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
)

var (
	asyncSemaphore  chan struct{}
	asyncCompletion chan struct{}
	RunOverride     func(fn func())
	initOnce        sync.Once
)

// Init initializes bounded async execution primitives.
func Init() {
	ensureInit()
}

func ensureInit() {
	initOnce.Do(func() {
		limit := 10000
		if limitStr := os.Getenv("ASYNC_OP_LIMIT"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		asyncSemaphore = make(chan struct{}, limit)
		asyncCompletion = make(chan struct{}, limit*2)

		slog.Info("Async operations initialized", "concurrent_limit", limit)
	})
}

// Run executes fn with bounded concurrency and tracks completion.
func Run(fn func()) {
	if RunOverride != nil {
		RunOverride(fn)
		return
	}
	ensureInit()
	go func() {
		asyncSemaphore <- struct{}{}

		defer func() {
			<-asyncSemaphore
			select {
			case asyncCompletion <- struct{}{}:
			default:
			}
		}()

		fn()
	}()
}

// Wait drains completions for inflight work or until ctx expires.
func Wait(ctx context.Context) int {
	ensureInit()
	inFlight := len(asyncSemaphore)
	if inFlight == 0 {
		return 0
	}

	completed := 0
	for completed < inFlight {
		select {
		case <-asyncCompletion:
			completed++
		case <-ctx.Done():
			return inFlight - completed
		}
	}

	return 0
}

// QueueDepth returns current in-flight async operations.
func QueueDepth() int64 {
	ensureInit()
	return int64(len(asyncSemaphore))
}
