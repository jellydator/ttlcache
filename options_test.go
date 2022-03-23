package ttlcache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_optionFunc_apply(t *testing.T) {
	var called bool

	optionFunc[string, string](func(_ *options[string, string]) {
		called = true
	}).apply(nil)
	assert.True(t, called)
}

func Test_applyOptions(t *testing.T) {
	var opts options[string, string]

	applyOptions(&opts,
		WithCapacity[string, string](12),
		WithTTL[string, string](time.Hour),
	)

	assert.Equal(t, uint64(12), opts.capacity)
	assert.Equal(t, time.Hour, opts.ttl)
}

func Test_WithCapacity(t *testing.T) {
	var opts options[string, string]

	WithCapacity[string, string](12).apply(&opts)
	assert.Equal(t, uint64(12), opts.capacity)
}

func Test_WithTTL(t *testing.T) {
	var opts options[string, string]

	WithTTL[string, string](time.Hour).apply(&opts)
	assert.Equal(t, time.Hour, opts.ttl)
}

func Test_WithLoader(t *testing.T) {
	var opts options[string, string]

	l := LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
		return nil
	})
	WithLoader[string, string](l).apply(&opts)
	assert.NotNil(t, opts.loader)
}

func Test_WithDisableTouchOnHit(t *testing.T) {
	var opts options[string, string]

	WithDisableTouchOnHit[string, string]().apply(&opts)
	assert.True(t, opts.disableTouchOnHit)
}
