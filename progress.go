package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

func startProgressLogger(ctx context.Context, label string, totalItems int64, totalBytes int64, items *atomic.Int64, bytes *atomic.Int64) func() {
	ctx, cancel := context.WithCancel(ctx)
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				attrs := []any{
					"processed", items.Load(),
					"total", totalItems,
				}
				if bytes != nil {
					attrs = append(attrs, "bytes", bytes.Load(), "total_bytes", totalBytes)
				}
				slog.Info(label, attrs...)
			}
		}
	}()
	return cancel
}
