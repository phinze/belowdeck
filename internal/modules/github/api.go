package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PRStats holds counts of my PRs per status, plus how many have failing CI.
// Every PR lands in exactly one status bucket, so the buckets sum to the
// number of open PRs.
type PRStats struct {
	Draft            int
	Waiting          int
	Approved         int
	ChangesRequested int
	CIFailed         int
}

// PRStatus is the single bucket a PR is shown in.
type PRStatus string

const (
	PRStatusDraft    PRStatus = "draft"
	PRStatusWaiting  PRStatus = "waiting"
	PRStatusApproved PRStatus = "approved"
	PRStatusChanges  PRStatus = "changes"
)

// CIStatus represents the CI check status of a PR.
type CIStatus string

const (
	CIStatusPending CIStatus = "pending"
	CIStatusPassed  CIStatus = "passed"
	CIStatusFailed  CIStatus = "failed"
)

// PRInfo holds information about a single PR.
type PRInfo struct {
	Title  string
	Repo   string
	Number int
	Status PRStatus
	CI     CIStatus
	URL    string
}

// classify picks the one bucket a PR belongs in. Draft wins over any review
// decision: an approved draft still can't merge, so it isn't "OK" yet.
func classify(isDraft bool, reviewDecision string) PRStatus {
	switch {
	case isDraft:
		return PRStatusDraft
	case reviewDecision == "CHANGES_REQUESTED":
		return PRStatusChanges
	case reviewDecision == "APPROVED":
		return PRStatusApproved
	default:
		return PRStatusWaiting
	}
}

// ciStatus maps a statusCheckRollup state onto our three CI states. The
// rollup covers both legacy commit statuses and check runs, so GitHub Actions
// results show up here.
func ciStatus(rollupState string) CIStatus {
	switch rollupState {
	case "SUCCESS":
		return CIStatusPassed
	case "FAILURE", "ERROR":
		return CIStatusFailed
	default:
		return CIStatusPending
	}
}

// summarize counts a PR list into stats. The key and the overlay both derive
// from the same list through this, so they can't disagree.
func summarize(prs []PRInfo) PRStats {
	var stats PRStats
	for _, pr := range prs {
		switch pr.Status {
		case PRStatusDraft:
			stats.Draft++
		case PRStatusApproved:
			stats.Approved++
		case PRStatusChanges:
			stats.ChangesRequested++
		default:
			stats.Waiting++
		}
		if pr.CI == CIStatusFailed {
			stats.CIFailed++
		}
	}
	return stats
}

// Client is a GitHub API client.
type Client struct {
	token      string
	endpoint   string // GraphQL URL; tests point this at a fake
	httpClient *http.Client
}

// NewClient creates a new GitHub API client using the gh CLI token.
func NewClient() (*Client, error) {
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
		token:    token,
		endpoint: "https://api.github.com/graphql",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// GetMyPRList fetches my open PRs with review and CI status.
func (c *Client) GetMyPRList(ctx context.Context) ([]PRInfo, error) {
	return c.searchPRs(ctx, "is:pr is:open author:@me archived:false")
}

// GetReviewRequestedPRList fetches open PRs awaiting my review.
func (c *Client) GetReviewRequestedPRList(ctx context.Context) ([]PRInfo, error) {
	prs, err := c.searchPRs(ctx, "is:pr is:open review-requested:@me archived:false")
	if err != nil {
		return nil, err
	}

	// These are waiting on me regardless of what other reviewers decided, so
	// only draft-ness is worth distinguishing.
	for i := range prs {
		if prs[i].Status != PRStatusDraft {
			prs[i].Status = PRStatusWaiting
		}
	}
	return prs, nil
}

// searchQuery pulls everything the module shows in one round trip per page.
//
// This replaces a REST fan-out of three searches plus two requests per PR (one
// for draft state and head SHA, one for CI). That fan-out tripped GitHub's
// secondary rate limit as PR counts grew, assembled one view from several
// separately-timed queries, and read CI from the legacy combined status
// endpoint, which never sees GitHub Actions check runs.
const searchQuery = `query($q: String!, $cursor: String) {
  search(query: $q, type: ISSUE, first: 100, after: $cursor) {
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on PullRequest {
        title
        number
        url
        isDraft
        reviewDecision
        repository { nameWithOwner }
        commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
      }
    }
  }
}`

type searchResponse struct {
	Search struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []struct {
			Title          string `json:"title"`
			Number         int    `json:"number"`
			URL            string `json:"url"`
			IsDraft        bool   `json:"isDraft"`
			ReviewDecision string `json:"reviewDecision"`
			Repository     struct {
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"repository"`
			Commits struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							State string `json:"state"`
						} `json:"statusCheckRollup"`
					} `json:"commit"`
				} `json:"nodes"`
			} `json:"commits"`
		} `json:"nodes"`
	} `json:"search"`
}

// searchPRs runs a PR search, following pages until every match is in hand.
func (c *Client) searchPRs(ctx context.Context, query string) ([]PRInfo, error) {
	var prs []PRInfo
	var cursor *string

	for {
		var resp searchResponse
		vars := map[string]any{"q": query, "cursor": cursor}
		if err := c.graphql(ctx, searchQuery, vars, &resp); err != nil {
			return nil, err
		}

		for _, n := range resp.Search.Nodes {
			rollup := ""
			if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
				rollup = n.Commits.Nodes[0].Commit.StatusCheckRollup.State
			}
			prs = append(prs, PRInfo{
				Title:  n.Title,
				Repo:   n.Repository.NameWithOwner,
				Number: n.Number,
				Status: classify(n.IsDraft, n.ReviewDecision),
				CI:     ciStatus(rollup),
				URL:    n.URL,
			})
		}

		if !resp.Search.PageInfo.HasNextPage {
			break
		}
		next := resp.Search.PageInfo.EndCursor
		cursor = &next
	}

	sortPRsByRepo(prs)
	return prs, nil
}

// graphql posts a query and decodes its data into out.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return apiError(resp)
	}

	// GraphQL reports query failures with a 200 and an errors array, so a
	// clean status line isn't enough to trust the data.
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
		}
		return errors.New("GraphQL error: " + strings.Join(msgs, "; "))
	}

	return json.Unmarshal(envelope.Data, out)
}

// apiError builds an error from a non-200 response.
//
// GitHub explains every refusal in the response body, and for throttling it
// also names the exhausted budget in the rate limit headers. Reporting only
// the status line turns a 403 into a guessing game: the status alone cannot
// distinguish a scope problem from a primary rate limit from a secondary one,
// and those want completely different fixes.
func apiError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	detail := strings.TrimSpace(string(body))
	var payload struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Message != "" {
		detail = payload.Message
		if payload.DocumentationURL != "" {
			detail += " (" + payload.DocumentationURL + ")"
		}
	}

	msg := "API error: " + resp.Status
	if detail != "" {
		msg += ": " + detail
	}

	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		resets := "an unknown time"
		if sec, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			resets = "in " + time.Until(time.Unix(sec, 0)).Round(time.Second).String()
		}
		msg += fmt.Sprintf(" [%s budget exhausted (limit %s), resets %s]",
			resp.Header.Get("X-RateLimit-Resource"),
			resp.Header.Get("X-RateLimit-Limit"),
			resets)
	} else if retry := resp.Header.Get("Retry-After"); retry != "" {
		msg += " [secondary rate limit, retry after " + retry + "s]"
	}

	return errors.New(msg)
}

// shortRepo drops the owner from an owner/repo name.
func shortRepo(repo string) string {
	if idx := strings.LastIndex(repo, "/"); idx != -1 {
		return repo[idx+1:]
	}
	return repo
}

// sortPRsByRepo sorts PRs by repo name alphabetically, then by PR number.
func sortPRsByRepo(prs []PRInfo) {
	sort.Slice(prs, func(i, j int) bool {
		repoI, repoJ := shortRepo(prs[i].Repo), shortRepo(prs[j].Repo)
		if repoI != repoJ {
			return repoI < repoJ
		}
		return prs[i].Number < prs[j].Number
	})
}
