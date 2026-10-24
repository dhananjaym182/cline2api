package protocol

import (
	"errors"
	"strings"
	"testing"
)

func TestScanSSE(t *testing.T) {
	src := strings.Join([]string{
		": comment",
		"event: response.created",
		`data: {"type":"one"}`,
		"data:",
		"data: [DONE]",
		"data: {not-json",
		`data: {"type":"two"}`,
		"",
	}, "\n")
	var got []string
	if err := ScanSSE(strings.NewReader(src), func(obj map[string]any) {
		got = append(got, obj["type"].(string))
	}); err != nil {
		t.Fatalf("ScanSSE: %v", err)
	}
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("events = %v", got)
	}
}

type failingReader struct{ served bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.served {
		return 0, errors.New("boom")
	}
	r.served = true
	copy(p, "data: {\"type\":\"x\"}\n")
	return len("data: {\"type\":\"x\"}\n"), nil
}

func TestScanSSEPropagatesReadError(t *testing.T) {
	called := 0
	err := ScanSSE(&failingReader{}, func(map[string]any) { called++ })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected read error, got %v", err)
	}
	if called != 1 {
		t.Fatalf("handler calls = %d", called)
	}
}
