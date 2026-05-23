package history

import (
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"time"
)

// idCounter is a monotonic counter used as a last-resort fallback when
// crypto/rand repeatedly fails. It ensures uniqueness even if the system
// entropy pool is depleted.
var idCounter atomic.Int64

// newID returns a random hex string suitable as a DB primary key.
// It retries crypto/rand on failure; only on repeated failure does it
// fall back to a counter-based ID that guarantees uniqueness (no collisions).
func newID() string {
	b := make([]byte, 16)
	for i := 0; i < 3; i++ {
		if _, err := rand.Read(b); err == nil {
			return fmt.Sprintf("%x-%x-%x-%x-%x",
				b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
		}
	}
	// Last resort: counter + timestamp ensures no collisions.
	c := idCounter.Add(1)
	now := time.Now()
	return fmt.Sprintf("fallback-%d-%x-%d", now.UnixMilli(), now.Nanosecond(), c)
}
