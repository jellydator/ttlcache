// Package stream provides functionality to stream orders.
package stream

import (
	"context"
	"dbcache/internal/order"
)

// _orders is a slice of predefined orders that will be streamed.
// This is just a mock data source for demonstration purposes.
var _orders = []order.Order{
	{
		Asset:  "BTC",
		Volume: 11,
	},
	{
		Asset:  "ETH",
		Volume: 42,
	},
	{
		Asset:  "ETH",
		Volume: 33,
	},
	{
		Asset:  "BTC",
		Volume: 15,
	},
	{
		Asset:  "BTC",
		Volume: 8,
	},
	{
		Asset:  "ETH",
		Volume: 29,
	},
	{
		Asset:  "BTC",
		Volume: 34,
	},
	{
		Asset:  "BTC",
		Volume: 65,
	},
	{
		Asset:  "ETH",
		Volume: 5,
	},
	{
		Asset:  "BTC",
		Volume: 71,
	},
}

// Streamer is responsible for streaming orders.
type Streamer struct{}

// NewStreamer creates a new streamer instance.
func NewStreamer() *Streamer {
	return &Streamer{}
}

// Consume returns a channel that streams orders. The channel
// is closed when all orders are sent or when the context
// is done.
func (s *Streamer) Consume(ctx context.Context) <-chan order.Order {
	// Most message broker APIs usually provide a way to stream messages
	// using channels. Here we simulate that by creating a channel
	// and sending predefined orders to it.
	ch := make(chan order.Order)

	go func() {
		defer close(ch)

		// Simulate streaming orders by sending them to the channel
		// one by one.
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
