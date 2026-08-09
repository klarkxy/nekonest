package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionVisibilityUsesInclusiveSevenDayBoundaryAndAttentionOverride(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	cutoff := recentSessionCutoff(now)
	tests := []struct {
		name   string
		status AgentStatus
		last   time.Time
		want   bool
	}{
		{name: "exact boundary", status: StatusIdle, last: cutoff, want: true},
		{name: "one second too old", status: StatusIdle, last: cutoff.Add(-time.Second)},
		{name: "old running", status: StatusRunning, last: cutoff.Add(-30 * 24 * time.Hour), want: true},
		{name: "old approval", status: StatusWaitingApproval, last: cutoff.Add(-30 * 24 * time.Hour), want: true},
		{name: "old user input", status: StatusWaitingUser, last: cutoff.Add(-30 * 24 * time.Hour), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := &SessionInfo{Status: test.status, LastActivity: test.last}
			if got := sessionIsVisible(session, now); got != test.want {
				t.Fatalf("sessionIsVisible() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFileDiscoveryCacheReusesInvalidatesCachesErrorsAndPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.json")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newFileDiscoveryCache[string]()
	parseCount := 0
	load := func(parseErr error) (string, error) {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		return cache.load(path, info, func() (string, error) {
			parseCount++
			return "parsed", parseErr
		})
	}
	if value, err := load(nil); err != nil || value != "parsed" {
		t.Fatalf("first load = %q, %v", value, err)
	}
	if _, err := load(nil); err != nil || parseCount != 1 {
		t.Fatalf("unchanged load reparsed: count=%d err=%v", parseCount, err)
	}
	if err := os.WriteFile(path, []byte("changed-size"), 0o600); err != nil {
		t.Fatal(err)
	}
	parseFailure := errors.New("fixture parse failure")
	if _, err := load(parseFailure); !errors.Is(err, parseFailure) || parseCount != 2 {
		t.Fatalf("changed load = count=%d err=%v", parseCount, err)
	}
	if _, err := load(nil); !errors.Is(err, parseFailure) || parseCount != 2 {
		t.Fatalf("cached failure reparsed: count=%d err=%v", parseCount, err)
	}
	cache.prune(map[string]struct{}{})
	_, _, entries := cache.stats()
	if entries != 0 {
		t.Fatalf("prune retained %d entries", entries)
	}
}
