package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestLogsClear(t *testing.T) {
	oldLogs := requestLogs
	oldPath := requestLogsPath
	dir := t.TempDir()
	requestLogsPath = filepath.Join(dir, ".cline-request-logs.json")
	requestLogsMu.Lock()
	requestLogs = []RequestLog{
		{ID: "req_2", StartedAt: time.Now()},
		{ID: "req_1", StartedAt: time.Now().Add(-time.Minute)},
	}
	saveRequestLogsLocked()
	requestLogsMu.Unlock()
	defer func() {
		requestLogsMu.Lock()
		requestLogs = oldLogs
		requestLogsMu.Unlock()
		requestLogsPath = oldPath
	}()

	rec := httptest.NewRecorder()
	handleAdminRequestLogsClear(rec, httptest.NewRequest(http.MethodGet, "/admin/api/request-logs/clear", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	handleAdminRequestLogsClear(rec, httptest.NewRequest(http.MethodPost, "/admin/api/request-logs/clear", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	requestLogsMu.Lock()
	n := len(requestLogs)
	requestLogsMu.Unlock()
	if n != 0 {
		t.Fatalf("logs remain after clear: %d", n)
	}
	data, err := os.ReadFile(requestLogsPath)
	if err != nil {
		t.Fatalf("persisted file missing: %v", err)
	}
	var persisted []RequestLog
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("persisted file unreadable: %v (%q)", err, data)
	}
	if len(persisted) != 0 {
		t.Fatalf("persisted logs = %d, want 0", len(persisted))
	}
}
