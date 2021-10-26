package ttlcache

import "sync/atomic"

// Metrics contains common cache metrics so you can calculate hit and miss rates
type Metrics struct {
	// succesful inserts
	Inserted int64
	// retrieval attempts
	Retrievals int64
	// all get calls that were in the cache (excludes loader invocations)
	Hits int64
	// entries not in cache (includes loader invocations)
	Misses int64
	// items removed from the cache in any way
	Evicted int64
}

func (m *Metrics) GetInserted() int64 {
	return atomic.LoadInt64(&m.Inserted)
}

func (m *Metrics) IncInserted() {
	atomic.AddInt64(&m.Inserted, 1)
}

func (m *Metrics) GetRetrievals() int64 {
	return atomic.LoadInt64(&m.Retrievals)
}

func (m *Metrics) IncRetrievals() {
	m.AddRetrievals(1)
}

func (m *Metrics) AddRetrievals(delta int64) {
	atomic.AddInt64(&m.Retrievals, delta)
}

func (m *Metrics) GetHits() int64 {
	return atomic.LoadInt64(&m.Hits)
}

func (m *Metrics) IncHits() {
	atomic.AddInt64(&m.Hits, 1)
}

func (m *Metrics) GetMisses() int64 {
	return atomic.LoadInt64(&m.Misses)
}

func (m *Metrics) IncMisses() {
	atomic.AddInt64(&m.Misses, 1)
}

func (m *Metrics) GetEvicted() int64 {
	return atomic.LoadInt64(&m.Evicted)
}

func (m *Metrics) IncEvicted() {
	m.AddEvicted(1)
}

func (m *Metrics) AddEvicted(delta int64) {
	atomic.AddInt64(&m.Evicted, delta)
}