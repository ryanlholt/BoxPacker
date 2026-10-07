package boxpacker

import "context"

// Internal packers constructed by existing callers may have no context.
// Check Done first to avoid locking an active cancel context at every placement.
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
