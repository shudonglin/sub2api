package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidatedTransport_CacheHostValidation(t *testing.T) {
	originalValidate := validateResolvedIP
	defer func() { validateResolvedIP = originalValidate }()

	var validateCalls int32
	validateResolvedIP = func(host string) error {
		atomic.AddInt32(&validateCalls, 1)
		require.Equal(t, "api.openai.com", host)
		return nil
	}

	var baseCalls int32
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&baseCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})

	now := time.Unix(1730000000, 0)
	transport := newValidatedTransport(base)
	transport.now = func() time.Time { return now }

	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)
	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	require.Equal(t, int32(1), atomic.LoadInt32(&validateCalls))
	require.Equal(t, int32(2), atomic.LoadInt32(&baseCalls))
}

func TestValidatedTransport_ExpiredCacheTriggersRevalidation(t *testing.T) {
	originalValidate := validateResolvedIP
	defer func() { validateResolvedIP = originalValidate }()

	var validateCalls int32
	validateResolvedIP = func(_ string) error {
		atomic.AddInt32(&validateCalls, 1)
		return nil
	}

	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})

	now := time.Unix(1730001000, 0)
	transport := newValidatedTransport(base)
	transport.now = func() time.Time { return now }

	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	now = now.Add(validatedHostTTL + time.Second)
	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	require.Equal(t, int32(2), atomic.LoadInt32(&validateCalls))
}

func TestValidatedTransport_ValidationErrorStopsRoundTrip(t *testing.T) {
	originalValidate := validateResolvedIP
	defer func() { validateResolvedIP = originalValidate }()

	expectedErr := errors.New("dns rebinding rejected")
	validateResolvedIP = func(_ string) error {
		return expectedErr
	}

	var baseCalls int32
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&baseCalls, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})

	transport := newValidatedTransport(base)
	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, int32(0), atomic.LoadInt32(&baseCalls))
}

// ---------------------------------------------------------------------------
// Pool preset tests
// ---------------------------------------------------------------------------

func TestStreamingOptions_DistinctFromNonStreaming(t *testing.T) {
	base := Options{}
	sOpts := StreamingOptions(base)
	nOpts := NonStreamingOptions(base)

	assert.NotEqual(t, sOpts.MaxIdleConns, nOpts.MaxIdleConns, "MaxIdleConns should differ")
	assert.NotEqual(t, sOpts.MaxIdleConnsPerHost, nOpts.MaxIdleConnsPerHost, "MaxIdleConnsPerHost should differ")
	assert.NotEqual(t, sOpts.IdleConnTimeout, nOpts.IdleConnTimeout, "IdleConnTimeout should differ")

	// Sanity: streaming values are larger (stream pools are bigger)
	assert.Greater(t, sOpts.MaxIdleConns, nOpts.MaxIdleConns)
	assert.Greater(t, sOpts.MaxIdleConnsPerHost, nOpts.MaxIdleConnsPerHost)
	assert.Greater(t, sOpts.IdleConnTimeout, nOpts.IdleConnTimeout)
}

func TestGetClient_StreamingVsNonStreamingReturnDifferentInstances(t *testing.T) {
	// Reset shared map so this test is independent.
	sharedClients = sync.Map{}
	t.Cleanup(func() { sharedClients = sync.Map{} })

	base := Options{Timeout: 0}
	sClient, err := GetClient(StreamingOptions(base))
	require.NoError(t, err)

	nClient, err := GetClient(NonStreamingOptions(base))
	require.NoError(t, err)

	assert.NotSame(t, sClient, nClient, "streaming and non-streaming clients must be distinct cache entries")
}

func TestGetClient_SameStreamingOptionsReturnsCachedInstance(t *testing.T) {
	sharedClients = sync.Map{}
	t.Cleanup(func() { sharedClients = sync.Map{} })

	base := Options{Timeout: 5 * time.Second}
	c1, err := GetClient(StreamingOptions(base))
	require.NoError(t, err)

	c2, err := GetClient(StreamingOptions(base))
	require.NoError(t, err)

	assert.Same(t, c1, c2, "repeated calls with identical streaming options must return the cached instance")
}
