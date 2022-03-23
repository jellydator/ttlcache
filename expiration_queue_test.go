package ttlcache

import (
	"container/list"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_newExpirationQueue(t *testing.T) {
	assert.NotNil(t, newExpirationQueue[string, string]())
}

func Test_expirationQueue_isEmpty(t *testing.T) {
	assert.True(t, (expirationQueue[string, string]{}).isEmpty())
	assert.False(t, (expirationQueue[string, string]{{}}).isEmpty())
}

func Test_expirationQueue_update(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
		{
			Value: &Item[string, string]{
				value:      "test2",
				queueIndex: 1,
				expiresAt:  time.Now().Add(time.Minute),
			},
		},
	}

	q.update(q[1])
	require.Len(t, q, 2)
	assert.Equal(t, "test2", q[0].Value.(*Item[string, string]).value)
}

func Test_expirationQueue_push(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
	}
	elem := &list.Element{
		Value: &Item[string, string]{
			value:      "test2",
			queueIndex: 1,
			expiresAt:  time.Now().Add(time.Minute),
		},
	}

	q.push(elem)
	require.Len(t, q, 2)
	assert.Equal(t, "test2", q[0].Value.(*Item[string, string]).value)
}

func Test_expirationQueue_remove(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
		{
			Value: &Item[string, string]{
				value:      "test2",
				queueIndex: 1,
				expiresAt:  time.Now().Add(time.Minute),
			},
		},
	}

	q.remove(q[1])
	require.Len(t, q, 1)
	assert.Equal(t, "test1", q[0].Value.(*Item[string, string]).value)
}

func Test_expirationQueue_Len(t *testing.T) {
	assert.Equal(t, 1, (expirationQueue[string, string]{{}}).Len())
}

func Test_expirationQueue_Less(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
		{
			Value: &Item[string, string]{
				value:      "test2",
				queueIndex: 1,
				expiresAt:  time.Now().Add(time.Minute),
			},
		},
		{
			Value: &Item[string, string]{
				value:      "test3",
				queueIndex: 2,
			},
		},
	}

	assert.False(t, q.Less(2, 1))
	assert.True(t, q.Less(1, 2))
	assert.True(t, q.Less(1, 0))
	assert.False(t, q.Less(0, 1))
}

func Test_expirationQueue_Swap(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
		{
			Value: &Item[string, string]{
				value:      "test2",
				queueIndex: 1,
				expiresAt:  time.Now().Add(time.Minute),
			},
		},
	}

	q.Swap(0, 1)
	assert.Equal(t, "test2", q[0].Value.(*Item[string, string]).value)
	assert.Equal(t, "test1", q[1].Value.(*Item[string, string]).value)
}

func Test_expirationQueue_Push(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
	}

	elem := &list.Element{
		Value: &Item[string, string]{
			value:      "test2",
			queueIndex: 1,
			expiresAt:  time.Now().Add(time.Minute),
		},
	}

	q.Push(elem)
	require.Len(t, q, 2)
	assert.Equal(t, "test2", q[1].Value.(*Item[string, string]).value)
}

func Test_expirationQueue_Pop(t *testing.T) {
	q := expirationQueue[string, string]{
		{
			Value: &Item[string, string]{
				value:      "test1",
				queueIndex: 0,
				expiresAt:  time.Now().Add(time.Hour),
			},
		},
		{
			Value: &Item[string, string]{
				value:      "test2",
				queueIndex: 1,
				expiresAt:  time.Now().Add(time.Minute),
			},
		},
	}

	v := q.Pop()
	require.NotNil(t, v)
	assert.Equal(t, "test2", v.(*list.Element).Value.(*Item[string, string]).value)
	require.Len(t, q, 1)
	assert.Equal(t, "test1", q[0].Value.(*Item[string, string]).value)
}
