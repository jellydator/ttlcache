package stream

import "dbcache/internal/order"

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
