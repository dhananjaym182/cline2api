package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sseUpstream(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func testRequestLog(model string) *RequestLog {
	return &RequestLog{StartedAt: time.Now(), Model: model}
}

func runChatStream(t *testing.T, body string, reqLog *RequestLog) (*httptest.ResponseRecorder, streamResult) {
	t.Helper()
	rec := httptest.NewRecorder()
	res := handleStreamResponse(context.Background(), rec, sseUpstream(body), nil, reqLog)
	return rec, res
}

func TestChatStreamTerminalCompletesCleanly(t *testing.T) {
	reqLog := testRequestLog("m")
	body := "data: {\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	rec, res := runChatStream(t, body, reqLog)

	if !res.terminal || res.truncated || res.empty || res.clientGone {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !res.produced || res.firstOutputAt.IsZero() {
		t.Fatalf("expected produced output with first-output timestamp: %+v", res)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("missing content or sentinel in output: %q", out)
	}
	if n := strings.Count(out, "data: [DONE]"); n != 1 {
		t.Fatalf("upstream [DONE] must not be duplicated, got %d", n)
	}
	if strings.Contains(out, "api_error") {
		t.Fatalf("clean terminal stream must not emit an error frame: %q", out)
	}
	if !reqLog.Completed {
		t.Fatalf("terminal stream should finalize the log as completed")
	}
}

func TestChatStreamFinishReasonWithoutDoneIsTerminal(t *testing.T) {
	reqLog := testRequestLog("m")
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"

	rec, res := runChatStream(t, body, reqLog)

	if !res.terminal || res.truncated {
		t.Fatalf("finish_reason alone must satisfy the terminal contract: %+v", res)
	}
	if strings.Contains(reqLog.Error, "truncated") {
		t.Fatalf("terminal stream must not be logged as truncated: %q", reqLog.Error)
	}
	if n := strings.Count(rec.Body.String(), "data: [DONE]"); n != 1 {
		t.Fatalf("expected exactly one synthesized [DONE] sentinel, got %d", n)
	}
}

func TestChatStreamTruncationReportsError(t *testing.T) {
	reqLog := testRequestLog("m")
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"

	rec, res := runChatStream(t, body, reqLog)

	if !res.truncated || !res.produced || res.terminal || res.empty {
		t.Fatalf("expected truncation after output: %+v", res)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "upstream stream ended before completion") {
		t.Fatalf("missing in-band truncation error: %q", out)
	}
	if strings.Contains(out, "data: [DONE]") {
		t.Fatalf("truncated stream must not send the [DONE] sentinel: %q", out)
	}
	if reqLog.Completed || !strings.Contains(reqLog.Error, "truncated") {
		t.Fatalf("truncated stream must be logged as a failure: completed=%v err=%q", reqLog.Completed, reqLog.Error)
	}
}

func TestChatStreamEmptyIsNotFinalized(t *testing.T) {
	reqLog := testRequestLog("m")

	rec, res := runChatStream(t, "", reqLog)

	if !res.empty || res.produced || res.terminal || res.truncated {
		t.Fatalf("expected an empty attempt: %+v", res)
	}
	if !reqLog.FinishedAt.IsZero() {
		t.Fatalf("an empty attempt must stay unfinalized so the caller can retry")
	}
	if out := rec.Body.String(); strings.Contains(out, "data:") {
		t.Fatalf("empty attempt must not forward data frames: %q", out)
	}
}

func TestChatStreamHeartbeatWhileUpstreamSilent(t *testing.T) {
	old := streamHeartbeatInterval
	streamHeartbeatInterval = 10 * time.Millisecond
	defer func() { streamHeartbeatInterval = old }()

	pr, pw := io.Pipe()
	rec := httptest.NewRecorder()
	reqLog := testRequestLog("m")
	done := make(chan streamResult, 1)
	go func() {
		done <- handleStreamResponse(context.Background(), rec, &http.Response{Body: pr}, nil, reqLog)
	}()

	time.Sleep(60 * time.Millisecond)
	fmt.Fprint(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	pw.Close()

	select {
	case res := <-done:
		if !res.terminal {
			t.Fatalf("expected terminal result after heartbeat: %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not finish")
	}
	if out := rec.Body.String(); !strings.Contains(out, ": ping") {
		t.Fatalf("expected keepalive comments while the upstream was silent: %q", out)
	}
}

func TestResponsesStreamSuccessEmitsCompleted(t *testing.T) {
	reqLog := testRequestLog("m")
	body := "data: {\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	rec := httptest.NewRecorder()
	serveResponsesStream(context.Background(), rec, sseUpstream(body), reqLog, nil, nil)

	out := rec.Body.String()
	for _, want := range []string{"event: response.created", "event: response.output_text.delta", "hello", "event: response.completed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in responses stream: %q", want, out)
		}
	}
	if strings.Contains(out, "event: error") {
		t.Fatalf("clean stream must not emit an error event: %q", out)
	}
	if !reqLog.Completed {
		t.Fatalf("terminal responses stream should finalize the log as completed")
	}
}

func TestResponsesStreamTruncationEmitsError(t *testing.T) {
	reqLog := testRequestLog("m")
	body := "data: {\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"

	rec := httptest.NewRecorder()
	serveResponsesStream(context.Background(), rec, sseUpstream(body), reqLog, nil, nil)

	out := rec.Body.String()
	if !strings.Contains(out, "event: error") || !strings.Contains(out, "upstream_stream_truncated") {
		t.Fatalf("missing terminal error event: %q", out)
	}
	if strings.Contains(out, "event: response.completed") {
		t.Fatalf("truncated stream must not report completion: %q", out)
	}
	if reqLog.Completed {
		t.Fatalf("truncated responses stream must be logged as a failure")
	}
}

func TestResponsesStreamEmptyEmitsErrorWithoutEvents(t *testing.T) {
	reqLog := testRequestLog("m")

	rec := httptest.NewRecorder()
	serveResponsesStream(context.Background(), rec, sseUpstream(""), reqLog, nil, nil)

	out := rec.Body.String()
	if !strings.Contains(out, "event: error") || !strings.Contains(out, "empty stream") {
		t.Fatalf("expected an empty-stream error event: %q", out)
	}
	if strings.Contains(out, "event: response.created") {
		t.Fatalf("an empty attempt must not fabricate response lifecycle events: %q", out)
	}
	if reqLog.Completed {
		t.Fatalf("empty responses stream must be logged as a failure")
	}
}

func TestServeChatStreamRetriesOnceOnEmpty(t *testing.T) {
	reqLog := testRequestLog("m")
	good := "data: {\"choices\":[{\"delta\":{\"content\":\"from-retry\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	retries := 0
	retry := func() (*http.Response, *Account, error) {
		retries++
		return sseUpstream(good), nil, nil
	}

	rec := httptest.NewRecorder()
	serveChatStream(context.Background(), rec, sseUpstream(""), nil, reqLog, retry)

	if retries != 1 {
		t.Fatalf("expected exactly one retry, got %d", retries)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "from-retry") {
		t.Fatalf("retry content missing: %q", out)
	}
	if strings.Contains(out, "empty stream") {
		t.Fatalf("successful retry must not surface an empty-stream error: %q", out)
	}
	if !reqLog.Completed {
		t.Fatalf("successful retry should finalize the log as completed")
	}
}

func TestServeChatStreamDoesNotRetryAfterOutput(t *testing.T) {
	reqLog := testRequestLog("m")
	partial := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
	retries := 0
	retry := func() (*http.Response, *Account, error) {
		retries++
		return sseUpstream("data: [DONE]\n\n"), nil, nil
	}

	rec := httptest.NewRecorder()
	serveChatStream(context.Background(), rec, sseUpstream(partial), nil, reqLog, retry)

	if retries != 0 {
		t.Fatalf("must not replay a stream that already forwarded content (retries=%d)", retries)
	}
	if out := rec.Body.String(); !strings.Contains(out, "upstream stream ended before completion") {
		t.Fatalf("expected truncation error instead of a retry: %q", out)
	}
}

func TestServeResponsesStreamRetriesOnceOnEmpty(t *testing.T) {
	reqLog := testRequestLog("m")
	good := "data: {\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"from-retry\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	retries := 0
	retry := func() (*http.Response, *Account, error) {
		retries++
		return sseUpstream(good), nil, nil
	}

	rec := httptest.NewRecorder()
	serveResponsesStream(context.Background(), rec, sseUpstream(""), reqLog, nil, retry)

	if retries != 1 {
		t.Fatalf("expected exactly one retry, got %d", retries)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "from-retry") || !strings.Contains(out, "event: response.completed") {
		t.Fatalf("retry content or completion missing: %q", out)
	}
	if n := strings.Count(out, "event: response.created"); n != 1 {
		t.Fatalf("empty attempt must not emit lifecycle events before the retry: created=%d", n)
	}
	if strings.Contains(out, "event: error") {
		t.Fatalf("successful retry must not emit an error event: %q", out)
	}
}

func TestChatChunkHasOutputCountsReasoning(t *testing.T) {
	obj := map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{"reasoning_content": "thinking"},
		}},
	}
	if !chatChunkHasOutput(obj) {
		t.Fatal("reasoning-only deltas must count as produced output")
	}
}
