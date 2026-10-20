package llm

import "time"

func (a *Analyzer) SetRetryBase(d time.Duration) { a.retryBase = d }
