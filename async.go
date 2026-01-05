package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
)

var (
	asyncSemaphore  chan struct{}
	asyncCompletion chan struct{}
)

func initAsyncOps() {
	limit := 10000
	if limitStr := os.Getenv("ASYNC_OP_LIMIT"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	asyncSemaphore = make(chan struct{}, limit)
	asyncCompletion = make(chan struct{}, limit*2)

	slog.Info("Async operations initialized",
		"concurrent_limit", limit,
	)
}

func runAsyncOp(fn func()) {
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

func waitForAsyncOps(ctx context.Context) int {
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
