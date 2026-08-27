package jobs

import "time"

func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 30 * time.Second
	for i := 1; i < attempt && delay < 30*time.Minute; i++ {
		delay *= 2
		if delay > 30*time.Minute {
			delay = 30 * time.Minute
		}
	}
	return delay
}
