// Package stream provides functionality to stream orders.
package stream

import (
	"context"
	"dbcache/internal/order"
)

// Streamer is responsible for streaming orders.
type Streamer struct{}

// NewStreamer creates a new streamer instance.
func NewStreamer() *Streamer {
	return &Streamer{}
}

// Consume returns a channel that streams orders.
func (s *Streamer) Consume(ctx context.Context) <-chan order.Order {
	ch := make(chan order.Order)

	go func() {
		defer close(ch)

		for _, ord := range _orders {
			select {
			case <-ctx.Done():
				return
			case ch <- ord:
			}
		}
	}()

	return ch
}
