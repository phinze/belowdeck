package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		draft    bool
		decision string
		want     PRStatus
	}{
		{false, "", PRStatusWaiting},
		{false, "REVIEW_REQUIRED", PRStatusWaiting},
		{false, "APPROVED", PRStatusApproved},
		{false, "CHANGES_REQUESTED", PRStatusChanges},
		{true, "", PRStatusDraft},
		{true, "APPROVED", PRStatusDraft},
		{true, "CHANGES_REQUESTED", PRStatusDraft},
	}
	for _, tt := range tests {
		if got := classify(tt.draft, tt.decision); got != tt.want {
			t.Errorf("classify(%v, %q) = %q, want %q", tt.draft, tt.decision, got, tt.want)
		}
	}
}

func TestCIStatus(t *testing.T) {
	tests := map[string]CIStatus{
		"SUCCESS":  CIStatusPassed,
		"FAILURE":  CIStatusFailed,
		"ERROR":    CIStatusFailed,
		"PENDING":  CIStatusPending,
		"EXPECTED": CIStatusPending,
		"":         CIStatusPending,
	}
	for state, want := range tests {
		if got := ciStatus(state); got != want {
			t.Errorf("ciStatus(%q) = %q, want %q", state, got, want)
		}
	}
}

// TestSummarize uses the PR mix that exposed the key/overlay mismatch: an
// approved draft used to count as both Draft and OK on the key while the
// overlay showed it only as a draft.
func TestSummarize(t *testing.T) {
	prs := []PRInfo{
		{Repo: "mirendev/runtime", Number: 1298, Status: PRStatusWaiting, CI: CIStatusPassed},
		{Repo: "mirendev/runtime", Number: 1299, Status: PRStatusWaiting, CI: CIStatusPassed},
		{Repo: "mirendev/runtime", Number: 1301, Status: PRStatusWaiting, CI: CIStatusPassed},
		{Repo: "mirendev/infra", Number: 132, Status: PRStatusDraft, CI: CIStatusFailed},
		{Repo: "mirendev/rfd", Number: 164, Status: classify(true, "APPROVED"), CI: CIStatusFailed},
	}
	want := PRStats{Draft: 2, Waiting: 3, Approved: 0, ChangesRequested: 0, CIFailed: 2}
	got := summarize(prs)
	if got != want {
		t.Fatalf("summarize = %+v, want %+v", got, want)
	}
	if sum := got.Draft + got.Waiting + got.Approved + got.ChangesRequested; sum != len(prs) {
		t.Errorf("buckets sum to %d, want %d", sum, len(prs))
	}
}

type fakeNode struct {
	repo     string
	number   int
	draft    bool
	decision string
	rollup   string
}

func (n fakeNode) json() map[string]any {
	var rollup any
	if n.rollup != "" {
		rollup = map[string]any{"state": n.rollup}
	}
	return map[string]any{
		"title":          fmt.Sprintf("PR %d", n.number),
		"number":         n.number,
		"url":            fmt.Sprintf("https://github.com/%s/pull/%d", n.repo, n.number),
		"isDraft":        n.draft,
		"reviewDecision": n.decision,
		"repository":     map[string]any{"nameWithOwner": n.repo},
		"commits": map[string]any{"nodes": []any{
			map[string]any{"commit": map[string]any{"statusCheckRollup": rollup}},
		}},
	}
}

// fakeGraphQL serves pages of search results keyed by the incoming cursor.
func fakeGraphQL(t *testing.T, pages map[string][]fakeNode) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Cursor *string `json:"cursor"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		cursor := ""
		if req.Variables.Cursor != nil {
			cursor = *req.Variables.Cursor
		}

		nodes := []any{}
		for _, n := range pages[cursor] {
			nodes = append(nodes, n.json())
		}
		next := cursor + "x"
		_, hasNext := pages[next]
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"search": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": next},
			"nodes":    nodes,
		}}})
	}))
	t.Cleanup(srv.Close)
	return &Client{endpoint: srv.URL, httpClient: srv.Client()}
}

func TestSearchPRsFollowsPagesAndSorts(t *testing.T) {
	c := fakeGraphQL(t, map[string][]fakeNode{
		"":   {{repo: "o/zeta", number: 2, decision: "APPROVED", rollup: "SUCCESS"}},
		"x":  {{repo: "o/alpha", number: 9, draft: true, decision: "APPROVED", rollup: "FAILURE"}},
		"xx": {{repo: "o/alpha", number: 3, decision: "CHANGES_REQUESTED"}},
	})

	prs, err := c.GetMyPRList(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, pr := range prs {
		got = append(got, fmt.Sprintf("%s#%d %s %s", pr.Repo, pr.Number, pr.Status, pr.CI))
	}
	want := []string{
		"o/alpha#3 changes pending",
		"o/alpha#9 draft failed",
		"o/zeta#2 approved passed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReviewRequestedIgnoresOtherReviewers(t *testing.T) {
	c := fakeGraphQL(t, map[string][]fakeNode{
		"": {
			{repo: "o/r", number: 1, decision: "APPROVED"},
			{repo: "o/r", number: 2, decision: "CHANGES_REQUESTED"},
			{repo: "o/r", number: 3, draft: true},
		},
	})

	prs, err := c.GetReviewRequestedPRList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []PRStatus{PRStatusWaiting, PRStatusWaiting, PRStatusDraft}
	for i, pr := range prs {
		if pr.Status != want[i] {
			t.Errorf("#%d status = %q, want %q", pr.Number, pr.Status, want[i])
		}
	}
}

func TestGraphQLErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":null,"errors":[{"message":"Something went wrong"}]}`))
	}))
	defer srv.Close()
	c := &Client{endpoint: srv.URL, httpClient: srv.Client()}

	_, err := c.GetMyPRList(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Something went wrong") {
		t.Fatalf("err = %v, want GraphQL error message", err)
	}
}
