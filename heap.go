package ttlcache

import (
	"container/heap"
	"time"
)

type priorityQueue []*Item

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].expires.Before(pq[j].expires)
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*Item)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

func (cache *Cache) NewPriorityQueue() {
	cache.priorityQueue = make([]*Item, 0)
	cache.priorityQueueNewItem = make(chan bool)
	heap.Init(&cache.priorityQueue)
}

func (pq *priorityQueue) update(item *Item) {
	heap.Fix(pq, item.index)
}

func (pq *priorityQueue) add(item *Item) {
	heap.Push(pq, item)
}

func (pq *priorityQueue) remove(item *Item) {
	heap.Remove(pq, item.index)
}

func (cache *Cache) startPriorityQueueProcessing() {
	expireFunc := func(item *Item, cache *Cache) {
		cache.priorityQueue.remove(item)
		delete(cache.items, item.key)
	}

	go func(cache *Cache) {
		var sleepTime time.Duration
		for {
			if len(cache.priorityQueue) > 0 {
				sleepTime = cache.priorityQueue[0].expires.Sub(time.Now())
			} else {
				sleepTime = time.Duration(1 * time.Hour)
			}
			select {
			case <-time.After(sleepTime):
				expireFunc(cache.priorityQueue[0], cache)
			case <-cache.priorityQueueNewItem:
				continue
			}
		}
	}(cache)
}
