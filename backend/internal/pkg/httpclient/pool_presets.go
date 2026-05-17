package httpclient

import "time"

// Streaming pool defaults — tuned for long-lived SSE connections.
// Large MaxIdleConns / MaxIdleConnsPerHost lets many concurrent streams keep
// their connections warm without starving the non-streaming pool.
const (
	streamingMaxIdleConns        = 2048
	streamingMaxIdleConnsPerHost = 256
	streamingIdleConnTimeout     = 300 * time.Second
)

// Non-streaming pool defaults — short-lived request/response cycles.
const (
	nonStreamingMaxIdleConns        = 256
	nonStreamingMaxIdleConnsPerHost = 64
	nonStreamingIdleConnTimeout     = 90 * time.Second
)

// StreamingOptions returns a copy of baseOpts with stream-optimised transport
// pool parameters applied when they are not already set by the caller.
// Long-lived SSE connections use a separate pool so they cannot starve
// short-lived non-streaming requests.
func StreamingOptions(baseOpts Options) Options {
	o := baseOpts
	if o.MaxIdleConns <= 0 {
		o.MaxIdleConns = streamingMaxIdleConns
	}
	if o.MaxIdleConnsPerHost <= 0 {
		o.MaxIdleConnsPerHost = streamingMaxIdleConnsPerHost
	}
	if o.IdleConnTimeout <= 0 {
		o.IdleConnTimeout = streamingIdleConnTimeout
	}
	return o
}

// NonStreamingOptions returns a copy of baseOpts with conservative transport
// pool parameters suited for short-lived request/response cycles.
func NonStreamingOptions(baseOpts Options) Options {
	o := baseOpts
	if o.MaxIdleConns <= 0 {
		o.MaxIdleConns = nonStreamingMaxIdleConns
	}
	if o.MaxIdleConnsPerHost <= 0 {
		o.MaxIdleConnsPerHost = nonStreamingMaxIdleConnsPerHost
	}
	if o.IdleConnTimeout <= 0 {
		o.IdleConnTimeout = nonStreamingIdleConnTimeout
	}
	return o
}
