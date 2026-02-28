package api

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const positionsPath = "/api/v0/equity/positions"

// Client is a Trading 212 API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	authHeader string
}

// NewClient creates a Client. apiKeyID and apiSecret are combined as HTTP Basic auth.
// If httpClient is nil, a default client with TLS 1.3 is used.
func NewClient(apiKeyID, apiSecret, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS13,
				},
			},
		}
	}
	auth := base64.StdEncoding.EncodeToString([]byte(apiKeyID + ":" + apiSecret))
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		authHeader: "Basic " + auth,
	}
}

// FetchPositions calls GET /api/v0/equity/positions and returns parsed positions
// plus rate limit metadata. Positions have Compute() called before return.
func (c *Client) FetchPositions(ctx context.Context) ([]Position, RateLimitInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+positionsPath, nil)
	if err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, RateLimitInfo{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var positions []Position
	if err := json.NewDecoder(resp.Body).Decode(&positions); err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("decode response: %w", err)
	}

	for i := range positions {
		positions[i].Compute()
	}

	rl := parseRateLimit(resp)
	return positions, rl, nil
}

func parseRateLimit(resp *http.Response) RateLimitInfo {
	remaining, _ := strconv.Atoi(resp.Header.Get("x-ratelimit-remaining"))
	resetUnix, _ := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	return RateLimitInfo{
		Remaining: remaining,
		Reset:     time.Unix(resetUnix, 0),
	}
}
