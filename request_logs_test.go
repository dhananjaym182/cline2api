package main

import (
	"testing"
	"time"
)

func TestPruneRequestLogsBoundsAgeAndCount(t *testing.T) {
	now := time.Now()
	old := now.Add(-31 * 24 * time.Hour)
	entries := []RequestLog{
		{ID: "old", StartedAt: old},
		{ID: "keep1", StartedAt: now.Add(-time.Hour)},
		{ID: "keep2", StartedAt: now.Add(-2 * time.Hour)},
	}

	requestLogsMu.Lock()
	requestLogs = pruneRequestLogsLocked(entries)
	requestLogsMu.Unlock()

	if len(requestLogs) != 2 {
		t.Fatalf("expected 2 entries after age pruning, got %d", len(requestLogs))
	}
	if requestLogs[0].ID != "keep1" {
		t.Fatalf("expected newest first, got %q", requestLogs[0].ID)
	}
}

func TestListRequestLogsCursorPagination(t *testing.T) {
	now := time.Now()
	entries := make([]RequestLog, 0, 75)
	for i := 0; i < 75; i++ {
		entries = append(entries, RequestLog{
			ID:        "req_" + string(rune('a'+i)),
			StartedAt: now.Add(-time.Duration(75-i) * time.Second),
		})
	}

	requestLogsMu.Lock()
	requestLogs = pruneRequestLogsLocked(entries)
	requestLogsMu.Unlock()

	page1, err := listRequestLogs(50, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 50 || !page1.HasMore {
		t.Fatalf("page1 unexpected: len=%d hasMore=%v", len(page1.Items), page1.HasMore)
	}
	if page1.Items[0].ID != requestLogs[0].ID {
		t.Fatalf("page1 first item = %q, want %q", page1.Items[0].ID, requestLogs[0].ID)
	}

	page2, err := listRequestLogs(50, page1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 25 || page2.HasMore {
		t.Fatalf("page2 unexpected: len=%d hasMore=%v", len(page2.Items), page2.HasMore)
	}

	if page2.Items[0].ID != requestLogs[50].ID {
		t.Fatalf("page2 first item = %q, want %q", page2.Items[0].ID, requestLogs[50].ID)
	}
}

func TestListRequestLogsRejectsInvalidCursor(t *testing.T) {
	if _, err := listRequestLogs(50, "not-a-valid-cursor"); err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestParseTokenUsageCachePrecedence(t *testing.T) {
	usage := parseTokenUsage(map[string]any{
		"prompt_tokens": float64(100),
		"prompt_tokens_details": map[string]any{
			"cached_tokens": float64(40),
		},
		"cache_read_input_tokens":     float64(10),
		"cache_creation_input_tokens": float64(5),
	})
	if usage.Cached != 40 {
		t.Fatalf("expected nested precedence cached=40, got %d", usage.Cached)
	}
	if usage.Prompt != 100 {
		t.Fatalf("expected prompt=100, got %d", usage.Prompt)
	}
}
