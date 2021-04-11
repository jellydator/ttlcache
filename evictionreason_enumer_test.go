package ttlcache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError(t *testing.T) {
	assert.Equal(t, "key not found", ErrNotFound.Error())

}

func TestEvictionError(t *testing.T) {
	assert.Equal(t, "Removed", Removed.String())
	assert.Equal(t, "Expired", Expired.String())

}
