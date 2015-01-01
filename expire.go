package ttlcache

/*
import (
	"time"
)

var (
	cancelSleep chan bool
)

func (pq priorityQueue) initialize2() {
	for {
		select {
		case <-time.After(pq.items[0].ttl):
			for _, value := range pq.items {
				if value.expired() {
					pq.remove(pq.items[0])
				} else {
					break
				}
			}
		case <-cancelSleep:
		}
	}
}
*/
