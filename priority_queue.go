package ttlcache

import (
	"container/heap"
)

func newPriorityQueue[K comparable, V any]() *priorityQueue[K, V] {
	queue := &priorityQueue[K, V]{}
	heap.Init(queue)
	return queue
}

type priorityQueue[K comparable, V any] struct {
	items []*item[K, V]
}

func (pq *priorityQueue[K, V]) isEmpty() bool {
	return len(pq.items) == 0
}

func (pq *priorityQueue[K, V]) root() *item[K, V] {
	if len(pq.items) == 0 {
		return nil
	}

	return pq.items[0]
}

func (pq *priorityQueue[K, V]) update(item *item[K, V]) {
	heap.Fix(pq, item.queueIndex)
}

func (pq *priorityQueue[K, V]) push(item *item[K, V]) {
	heap.Push(pq, item)
}

func (pq *priorityQueue[K, V]) pop() *item[K, V] {
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(*item[K, V])
}

func (pq *priorityQueue[K, V]) remove(item *item[K, V]) {
	heap.Remove(pq, item.queueIndex)
}

func (pq priorityQueue[K, V]) Len() int {
	length := len(pq.items)
	return length
}

// Less will consider items with time.Time default value (epoch start) as
// more than set items.
func (pq priorityQueue[K, V]) Less(i, j int) bool {
	if pq.items[i].expireAt.IsZero() {
		return false
	}
	if pq.items[j].expireAt.IsZero() {
		return true
	}
	return pq.items[i].expireAt.Before(pq.items[j].expireAt)
}

func (pq priorityQueue[K, V]) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].queueIndex = i
	pq.items[j].queueIndex = j
}

func (pq *priorityQueue[K, V]) Push(x interface{}) {
	item := x.(*item[K, V])
	item.queueIndex = len(pq.items)
	pq.items = append(pq.items, item)
}

func (pq *priorityQueue[K, V]) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	item.queueIndex = -1
	// de-reference the element to be popped for Garbage Collector to
	// de-allocate the memory
	old[n-1] = nil
	pq.items = old[0 : n-1]
	return item
}
