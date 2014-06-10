package ttlcache

import (
	"testing"
	"time"
)

func TestExpired(t *testing.T) {
	item := &Item{data: "blahblah"}
	if !item.expired() {
		t.Errorf("Expected item to be expired by default")
	}

	expiration := time.Now().Add(time.Second)
	item.expires = &expiration
	if item.expired() {
		t.Errorf("Expected item to not be expired")
	}

	expiration = time.Now().Add(0 - time.Second)
	item.expires = &expiration
	if !item.expired() {
		t.Errorf("Expected item to be expired once time has passed")
	}
}

func TestTouch(t *testing.T) {
	item := &Item{data: "blahblah"}
	item.touch(time.Second)
	if item.expired() {
		t.Errorf("Expected item to not be expired once touched")
	}
}
