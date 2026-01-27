package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// PRStats holds counts of PRs in different states.
type PRStats struct {
	WaitingForReview int
	Approved         int
	ChangesRequested int
}

// PRStatus represents the review status of a PR.
type PRStatus string

const (
	PRStatusWaiting PRStatus = "waiting"
	PRStatusApproved PRStatus = "approved"
	PRStatusChanges  PRStatus = "changes"
)

// PRInfo holds information about a single PR.
type PRInfo struct {
	Title  string
	Repo   string
	Number int
	Status PRStatus
	URL    string
}

// Client is a GitHub API client.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a new GitHub API client using the gh CLI token.
func NewClient() (*Client, error) {
	// Get token from gh CLI
	cmd := exec.Command("gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get gh auth token: %w", err)
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		return nil, fmt.Errorf("gh auth token is empty")
	}

	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// GetMyPRStats fetches stats about the authenticated user's PRs.
func (c *Client) GetMyPRStats(ctx context.Context) (PRStats, error) {
	var stats PRStats

	// Get username first
	username, err := c.getAuthenticatedUser(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to get username: %w", err)
	}

	// Fetch counts in parallel
	type result struct {
		field string
		count int
		err   error
	}
	results := make(chan result, 3)

	queries := []struct {
		field string
		query string
	}{
		{"waiting", fmt.Sprintf("is:pr author:%s is:open review:required", username)},
		{"approved", fmt.Sprintf("is:pr author:%s is:open review:approved", username)},
		{"changes", fmt.Sprintf("is:pr author:%s is:open review:changes_requested", username)},
	}

	for _, q := range queries {
		go func(field, query string) {
			count, err := c.searchPRCount(ctx, query)
			results <- result{field, count, err}
		}(q.field, q.query)
	}

	for i := 0; i < 3; i++ {
		r := <-results
		if r.err != nil {
			return stats, r.err
		}
		switch r.field {
		case "waiting":
			stats.WaitingForReview = r.count
		case "approved":
			stats.Approved = r.count
		case "changes":
			stats.ChangesRequested = r.count
		}
	}

	return stats, nil
}

// getAuthenticatedUser returns the authenticated user's login.
func (c *Client) getAuthenticatedUser(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s", resp.Status)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}

	return user.Login, nil
}

// searchPRCount searches for PRs matching a query and returns the count.
func (c *Client) searchPRCount(ctx context.Context, query string) (int, error) {
	apiURL := "https://api.github.com/search/issues?per_page=1&q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API error: %s", resp.Status)
	}

	var result struct {
		TotalCount int `json:"total_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.TotalCount, nil
}

// GetMyPRList fetches a list of PRs with details.
func (c *Client) GetMyPRList(ctx context.Context) ([]PRInfo, error) {
	username, err := c.getAuthenticatedUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get username: %w", err)
	}

	type result struct {
		status PRStatus
		prs    []PRInfo
		err    error
	}
	results := make(chan result, 3)

	queries := []struct {
		status PRStatus
		query  string
	}{
		{PRStatusWaiting, fmt.Sprintf("is:pr author:%s is:open review:required", username)},
		{PRStatusApproved, fmt.Sprintf("is:pr author:%s is:open review:approved", username)},
		{PRStatusChanges, fmt.Sprintf("is:pr author:%s is:open review:changes_requested", username)},
	}

	for _, q := range queries {
		go func(status PRStatus, query string) {
			prs, err := c.searchPRs(ctx, query, status)
			results <- result{status, prs, err}
		}(q.status, q.query)
	}

	var allPRs []PRInfo
	for i := 0; i < 3; i++ {
		r := <-results
		if r.err != nil {
			return nil, r.err
		}
		allPRs = append(allPRs, r.prs...)
	}

	return allPRs, nil
}

// searchPRs searches for PRs matching a query and returns details.
func (c *Client) searchPRs(ctx context.Context, query string, status PRStatus) ([]PRInfo, error) {
	apiURL := "https://api.github.com/search/issues?per_page=10&q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s", resp.Status)
	}

	var searchResult struct {
		Items []struct {
			Title         string `json:"title"`
			Number        int    `json:"number"`
			HTMLURL       string `json:"html_url"`
			RepositoryURL string `json:"repository_url"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, err
	}

	var prs []PRInfo
	for _, item := range searchResult.Items {
		// Extract repo name from repository URL
		// https://api.github.com/repos/owner/repo -> owner/repo
		repoName := item.RepositoryURL
		if idx := strings.Index(repoName, "/repos/"); idx != -1 {
			repoName = repoName[idx+7:]
		}

		prs = append(prs, PRInfo{
			Title:  item.Title,
			Repo:   repoName,
			Number: item.Number,
			Status: status,
			URL:    item.HTMLURL,
		})
	}

	return prs, nil
}
