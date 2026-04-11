package btsemm

import "fmt"

// APIError is returned when the API responds with a non-2xx status code.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("btse: api error %d: %s", e.StatusCode, e.Body)
}

// RateLimitError is returned when the API responds with 429.
type RateLimitError struct {
	RetryAfter string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("btse: rate limited, retry after %s", e.RetryAfter)
}
