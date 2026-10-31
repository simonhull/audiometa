// Package testctx provides contexts for testing cancellation inside parse loops.
package testctx

import (
	"context"
	"sync/atomic"
	"time"
)

type cancelAfter struct {
	remaining atomic.Int64
}

// Deadline reports that the context has no deadline.
func (*cancelAfter) Deadline() (time.Time, bool) { return time.Time{}, false }

// Done returns nil: the context is only observable through Err.
func (*cancelAfter) Done() <-chan struct{} { return nil }

// Value returns nil: the context carries no values.
func (*cancelAfter) Value(any) any { return nil }

// Err reports nil for the first n calls and context.Canceled afterwards.
func (c *cancelAfter) Err() error {
	if c.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

// CancelAfter returns a context whose Err method succeeds n times and then
// reports context.Canceled. It lets a test get past a parser's entry check and
// prove that a loop deeper in the parse observes cancellation.
func CancelAfter(n int) context.Context {
	c := &cancelAfter{}
	c.remaining.Store(int64(n))
	return c
}
