package binary

import "context"

// CtxCheckInterval is how often loops should test for cancellation.
const CtxCheckInterval = 1024

// Canceled returns ctx.Err() when i is a multiple of CtxCheckInterval, else nil.
// Use it in tight parse loops so cancellation is observed without paying for a
// context check on every iteration; i == 0 is checked, so an already-canceled
// context is caught on the first iteration.
func Canceled(ctx context.Context, i int) error {
	if i%CtxCheckInterval == 0 {
		return ctx.Err()
	}
	return nil
}
