package salesforce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	
	"github.com/agentbrain/agentbrain/internal/observability"
	"github.com/agentbrain/agentbrain/internal/retry"
)

const (
	apiVersion      = "v59.0"
	defaultLoginURL = "https://login.salesforce.com"
	maxRetries      = 3
	retryBaseDelay  = time.Second
)

// Client is a Salesforce HTTP client with OAuth, rate limiting, and retries.
type Client struct {
	httpClient      *http.Client
	auth            AuthConfig
	logger          *slog.Logger
	retryPolicy     *retry.RetryPolicy
	circuitBreaker  *retry.CircuitBreaker
	tracingManager  *observability.TracingManager
	metricsManager  *observability.MetricsManager

	mu          sync.RWMutex
	accessToken string
	instanceURL string
}

// NewClient creates a new Salesforce API client.
func NewClient(auth AuthConfig, logger *slog.Logger) *Client {
	if auth.LoginURL == "" {
		auth.LoginURL = defaultLoginURL
	}
	
	client := &Client{
		httpClient: &http.Client{Timeout: 2 * time.Minute},
		auth:       auth,
		logger:     logger,
	}
	
	// Initialize retry policies
	client.initializeRetryPolicies()
	
	return client
}

// initializeRetryPolicies sets up retry policies and circuit breaker for Salesforce operations.
func (c *Client) initializeRetryPolicies() {
	// Create retry policy optimized for API operations with rate limiting
	c.retryPolicy = retry.APIPolicy()
	
	// Create circuit breaker for Salesforce operations
	c.circuitBreaker = retry.NewCircuitBreaker("salesforce_api", 5, 2*time.Minute)
}

// SetRetryPolicy allows customizing the retry policy.
func (c *Client) SetRetryPolicy(policy *retry.RetryPolicy) {
	c.retryPolicy = policy
}

// SetCircuitBreaker allows customizing the circuit breaker.
func (c *Client) SetCircuitBreaker(cb *retry.CircuitBreaker) {
	c.circuitBreaker = cb
}

// SetObservabilityManagers sets the observability managers for Salesforce operations.
func (c *Client) SetObservabilityManagers(tracingManager *observability.TracingManager, metricsManager *observability.MetricsManager) {
	c.tracingManager = tracingManager
	c.metricsManager = metricsManager
}

// Authenticate performs the OAuth2 username-password flow.
func (c *Client) Authenticate(ctx context.Context) error {
	data := url.Values{
		"grant_type":    {"password"},
		"client_id":     {c.auth.ClientID},
		"client_secret": {c.auth.ClientSecret},
		"username":      {c.auth.Username},
		"password":      {c.auth.Password + c.auth.SecurityToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.auth.LoginURL+"/services/oauth2/token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("decode auth response: %w", err)
	}

	c.mu.Lock()
	c.accessToken = tokenResp.AccessToken
	c.instanceURL = tokenResp.InstanceURL
	c.mu.Unlock()

	c.logger.Info("authenticated with Salesforce", "instance", tokenResp.InstanceURL)
	return nil
}

// Get performs an authenticated GET request with retries.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	return c.doWithRetry(ctx, http.MethodGet, path, nil)
}

// Post performs an authenticated POST request with retries.
func (c *Client) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return c.doWithRetry(ctx, http.MethodPost, path, body)
}

// GetStream performs an authenticated GET and returns the response body for streaming.
func (c *Client) GetStream(ctx context.Context, path string) (io.ReadCloser, error) {
	c.mu.RLock()
	base := c.instanceURL
	token := c.accessToken
	c.mu.RUnlock()

	reqURL := base + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("request %s returned status %d", path, resp.StatusCode)
	}

	return resp.Body, nil
}

func (c *Client) doWithRetry(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	operation := func(ctx context.Context) ([]byte, error) {
		c.mu.RLock()
		base := c.instanceURL
		token := c.accessToken
		c.mu.RUnlock()

		reqURL := base + path
		req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", path, err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, fmt.Errorf("request %s returned status %d: %s", path, resp.StatusCode, string(respBody))
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("request %s returned status %d: %s", path, resp.StatusCode, string(respBody))
		}

		return respBody, nil
	}

	// Execute with unified retry framework
	if c.retryPolicy != nil && c.circuitBreaker != nil {
		return retry.ExecuteWithRetryAndCircuitBreaker(ctx, c.retryPolicy, c.circuitBreaker, operation)
	}

	// Fallback to direct execution
	return operation(ctx)
}

// BaseURL returns the API base path.
func (c *Client) BaseURL() string {
	return fmt.Sprintf("/services/data/%s", apiVersion)
}
