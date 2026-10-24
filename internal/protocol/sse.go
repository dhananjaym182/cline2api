package protocol

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// ScanSSE reads Server-Sent Events from src and invokes handle for each
// well-formed JSON `data:` payload. Non-data lines, blank payloads, "[DONE]"
// markers, and malformed JSON are skipped. The scanner accepts lines up to
// 8 MiB.
func ScanSSE(src io.Reader, handle func(obj map[string]any)) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(payload), &obj) != nil {
			continue
		}
		handle(obj)
	}
	return scanner.Err()
}
