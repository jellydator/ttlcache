package ttlcache

import (
	"testing"
	"time"
)

func TestExpired(t *testing.T) {
	ttl := time.Duration(1) * time.Second
	item := &Item{data: "blahblah", ttl: &ttl}
	if !item.expired() {
		t.Errorf("Expected item to be expired by default")
	}

	item.touch()
	if item.expired() {
		t.Errorf("Expected item to not be expired")
	}

	<-time.After(2 * time.Second)

	if !item.expired() {
		t.Errorf("Expected item to be expired once time has passed")
	}
}

func TestTouch(t *testing.T) {
	ttl := time.Duration(1) * time.Second
	item := &Item{data: "blahblah", ttl: &ttl}
	item.touch()
	if item.expired() {
		t.Errorf("Expected item to not be expired once touched")
	}
}
