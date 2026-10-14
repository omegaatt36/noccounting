package llm

import "time"

// SetRetryBase lets the external tests shrink the backoff between retries.
func (a *Analyzer) SetRetryBase(d time.Duration) { a.retryBase = d }
