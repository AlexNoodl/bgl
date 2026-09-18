package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultIGDBBaseURL      = "https://api.igdb.com/v4"
	defaultIGDBAuthURL      = "https://id.twitch.tv/oauth2/token"
	defaultRequestTimeout   = 5 * time.Second
	defaultRateLimitPerSec  = 4 // IGDB's documented default limit
	defaultRateLimitBurst   = 4
	defaultFailureThreshold = 5
	defaultOpenDuration     = 30 * time.Second
	tokenExpirySafetyMargin = 60 * time.Second
)

var ErrCircuitOpen = errors.New("igdb: circuit breaker is open")

type IGDBConfig struct {
	ClientID       string
	ClientSecret   string
	HTTPClient     *http.Client
	BaseURL        string
	AuthURL        string
	RequestTimeout time.Duration
	now            func() time.Time

	RateLimitPerSecond float64
	RateLimitBurst     int

	FailureThreshold int

	OpenDuration time.Duration
}

type IGDBClient struct {
	clientID       string
	clientSecret   string
	httpClient     *http.Client
	baseURL        string
	authURL        string
	requestTimeout time.Duration
	now            func() time.Time

	limiter *tokenBucket
	breaker *circuitBreaker

	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time
}

func NewIGDBClient(cfg IGDBConfig) *IGDBClient {
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultIGDBBaseURL
	}
	authURL := cfg.AuthURL
	if authURL == "" {
		authURL = defaultIGDBAuthURL
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	rate := cfg.RateLimitPerSecond
	if rate <= 0 {
		rate = defaultRateLimitPerSec
	}
	burst := cfg.RateLimitBurst
	if burst <= 0 {
		burst = defaultRateLimitBurst
	}
	failureThreshold := cfg.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = defaultFailureThreshold
	}
	openDuration := cfg.OpenDuration
	if openDuration <= 0 {
		openDuration = defaultOpenDuration
	}

	return &IGDBClient{
		clientID:       cfg.ClientID,
		clientSecret:   cfg.ClientSecret,
		httpClient:     httpClient,
		baseURL:        baseURL,
		authURL:        authURL,
		requestTimeout: requestTimeout,
		now:            now,
		limiter:        newTokenBucket(rate, burst, now),
		breaker:        newCircuitBreaker(failureThreshold, openDuration, now),
	}
}

func (c *IGDBClient) Query(ctx context.Context, endpoint string, query string) ([]byte, error) {
	if !c.breaker.allow() {
		return nil, ErrCircuitOpen
	}

	body, err := c.doQuery(ctx, endpoint, query)
	if err != nil {
		c.breaker.recordFailure()
		return nil, err
	}
	c.breaker.recordSuccess()
	return body, nil
}

func (c *IGDBClient) doQuery(ctx context.Context, endpoint string, query string) ([]byte, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("igdb: authenticating: %w", err)
	}

	if err := c.limiter.wait(ctx); err != nil {
		return nil, fmt.Errorf("igdb: rate limit wait: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	requestUrl := c.baseURL + "/" + endpoint
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, requestUrl, bytes.NewReader([]byte(query)))
	if err != nil {
		return nil, fmt.Errorf("igdb: building request: %w", err)
	}
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("igdb: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("igdb: reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("igdb: unexpected status %d: %s", resp.StatusCode, truncate(respBody, 500))
	}

	return respBody, nil
}

func (c *IGDBClient) ensureToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token != "" && c.now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	token, expiresIn, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	c.token = token
	c.tokenExpiry = c.now().Add(expiresIn - tokenExpirySafetyMargin)
	return c.token, nil
}

type twitchTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func (c *IGDBClient) fetchToken(ctx context.Context) (token string, expiresIn time.Duration, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"client_credentials"},
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.authURL, bytes.NewReader([]byte(form.Encode())))
	if err != nil {
		return "", 0, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(respBody, 500))
	}

	var parsed twitchTokenResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", 0, fmt.Errorf("decoding token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", 0, errors.New("token response had no access_token")
	}

	return parsed.AccessToken, time.Duration(parsed.ExpiresIn) * time.Second, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
