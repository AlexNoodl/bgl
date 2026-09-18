package catalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestClient(t *testing.T, transport roundTripFunc, configure func(*IGDBConfig)) *IGDBClient {
	t.Helper()
	cfg := IGDBConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		HTTPClient:   &http.Client{Transport: transport},
		BaseURL:      "https://api.igdb.test/v4",
		AuthURL:      "https://id.twitch.test/oauth2/token",
	}
	if configure != nil {
		configure(&cfg)
	}
	return NewIGDBClient(cfg)
}

func TestIGDBClient_Query_SendsAuthenticatedRequest(t *testing.T) {
	var authCalls, queryCalls int32

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://id.twitch.test/oauth2/token":
			atomic.AddInt32(&authCalls, 1)
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		case "https://api.igdb.test/v4/games":
			atomic.AddInt32(&queryCalls, 1)
			if got := req.Header.Get("Client-ID"); got != "test-client-id" {
				t.Errorf("Client-ID header = %q, want test-client-id", got)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer tok-1" {
				t.Errorf("Authorization header = %q, want %q", got, "Bearer tok-1")
			}
			body, _ := io.ReadAll(req.Body)
			if string(body) != `fields name; where id = 1;` {
				t.Errorf("request body = %q", body)
			}
			return jsonResponse(http.StatusOK, `[{"id":1,"name":"Elden Ring"}]`), nil
		default:
			t.Fatalf("unexpected request to %s", req.URL)
			return nil, nil
		}
	})

	client := newTestClient(t, transport, nil)

	got, err := client.Query(context.Background(), "games", "fields name; where id = 1;")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if string(got) != `[{"id":1,"name":"Elden Ring"}]` {
		t.Errorf("Query result = %s", got)
	}
	if authCalls != 1 || queryCalls != 1 {
		t.Errorf("authCalls=%d queryCalls=%d, want 1 and 1", authCalls, queryCalls)
	}
}

func TestIGDBClient_Query_ReusesTokenUntilExpiry(t *testing.T) {
	clock := newFakeClock()
	var authCalls int32

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://id.twitch.test/oauth2/token" {
			atomic.AddInt32(&authCalls, 1)
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		}
		return jsonResponse(http.StatusOK, `[]`), nil
	})

	client := newTestClient(t, transport, func(cfg *IGDBConfig) {
		cfg.now = clock.Now
	})

	for i := 0; i < 3; i++ {
		if _, err := client.Query(context.Background(), "games", "fields name;"); err != nil {
			t.Fatalf("Query %d: %v", i, err)
		}
	}
	if authCalls != 1 {
		t.Errorf("authCalls = %d, want 1 (token should be cached across calls)", authCalls)
	}

	clock.Advance(59 * time.Minute)
	if _, err := client.Query(context.Background(), "games", "fields name;"); err != nil {
		t.Fatalf("Query after expiry: %v", err)
	}
	if authCalls != 2 {
		t.Errorf("authCalls after expiry = %d, want 2", authCalls)
	}
}

func TestIGDBClient_CircuitBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	clock := newFakeClock()
	var queryCalls int32

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://id.twitch.test/oauth2/token" {
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		}
		atomic.AddInt32(&queryCalls, 1)
		return jsonResponse(http.StatusInternalServerError, `oops`), nil
	})

	const threshold = 3
	client := newTestClient(t, transport, func(cfg *IGDBConfig) {
		cfg.now = clock.Now
		cfg.FailureThreshold = threshold
		cfg.OpenDuration = time.Minute
	})

	for i := 0; i < threshold; i++ {
		if _, err := client.Query(context.Background(), "games", "q"); err == nil {
			t.Fatalf("Query %d: expected an error", i)
		}
	}
	if queryCalls != threshold {
		t.Fatalf("queryCalls = %d, want %d", queryCalls, threshold)
	}

	_, err := client.Query(context.Background(), "games", "q")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Query with open breaker: err = %v, want ErrCircuitOpen", err)
	}
	if queryCalls != threshold {
		t.Fatalf("queryCalls after open = %d, want unchanged %d", queryCalls, threshold)
	}
}

func TestIGDBClient_CircuitBreaker_HalfOpenAfterPause(t *testing.T) {
	clock := newFakeClock()
	var succeed atomic.Bool

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://id.twitch.test/oauth2/token" {
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		}
		if succeed.Load() {
			return jsonResponse(http.StatusOK, `[]`), nil
		}
		return jsonResponse(http.StatusInternalServerError, `oops`), nil
	})

	const threshold = 2
	openDuration := 30 * time.Second
	client := newTestClient(t, transport, func(cfg *IGDBConfig) {
		cfg.now = clock.Now
		cfg.FailureThreshold = threshold
		cfg.OpenDuration = openDuration
	})

	for i := 0; i < threshold; i++ {
		if _, err := client.Query(context.Background(), "games", "q"); err == nil {
			t.Fatalf("Query %d: expected an error", i)
		}
	}

	if _, err := client.Query(context.Background(), "games", "q"); err != ErrCircuitOpen {
		t.Fatalf("Query still within open window: err = %v, want ErrCircuitOpen", err)
	}

	clock.Advance(openDuration + time.Second)
	succeed.Store(true)
	if _, err := client.Query(context.Background(), "games", "q"); err != nil {
		t.Fatalf("half-open trial: unexpected error: %v", err)
	}

	if _, err := client.Query(context.Background(), "games", "q"); err != nil {
		t.Fatalf("post-recovery Query: unexpected error: %v", err)
	}
}

func TestIGDBClient_CircuitBreaker_HalfOpenTrialFailureReopens(t *testing.T) {
	clock := newFakeClock()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://id.twitch.test/oauth2/token" {
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		}
		return jsonResponse(http.StatusInternalServerError, `oops`), nil
	})

	const threshold = 2
	openDuration := 30 * time.Second
	client := newTestClient(t, transport, func(cfg *IGDBConfig) {
		cfg.now = clock.Now
		cfg.FailureThreshold = threshold
		cfg.OpenDuration = openDuration
	})

	for i := 0; i < threshold; i++ {
		if _, err := client.Query(context.Background(), "games", "q"); err == nil {
			t.Fatalf("Query %d: expected an error", i)
		}
	}

	clock.Advance(openDuration + time.Second)

	if _, err := client.Query(context.Background(), "games", "q"); err == nil || err == ErrCircuitOpen {
		t.Fatalf("half-open trial: err = %v, want a real (non-circuit) failure", err)
	}
	if _, err := client.Query(context.Background(), "games", "q"); err != ErrCircuitOpen {
		t.Fatalf("Query right after failed trial: err = %v, want ErrCircuitOpen", err)
	}
}

func TestIGDBClient_Query_UnexpectedStatusIsAnError(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://id.twitch.test/oauth2/token" {
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		}
		return jsonResponse(http.StatusTooManyRequests, `rate limited`), nil
	})

	client := newTestClient(t, transport, nil)

	if _, err := client.Query(context.Background(), "games", "q"); err == nil {
		t.Fatal("expected an error for a 429 response")
	}
}

func TestTokenBucket_WaitRespectsContextDeadline(t *testing.T) {
	b := newTokenBucket(0.001, 1, time.Now)

	ctx := context.Background()
	if err := b.wait(ctx); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := b.wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second wait: err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("second wait took %v, expected to return promptly after the deadline", elapsed)
	}
}
