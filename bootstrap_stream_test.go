package main

// Tests for the stream bootstrap buffer (task #66): pre-content frames are
// held per attempt and discarded on a pre-content retry, heartbeat comments
// pass through (or are suppressed), the frame/byte budget commits early, and a
// stalled attempt (first-content deadline) stays retryable.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cline-go-proxy/internal/config"
)

func installStreamConfig(t *testing.T, s config.StreamPolicy) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.Stream = s
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })
}

func TestBootstrapBufferHoldsDataAndForwardsComments(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{})
	rec := httptest.NewRecorder()
	bw := newBootstrapBuffer(rec)

	if _, err := bw.Write([]byte("data: held\n\n")); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != "" {
		t.Fatalf("data frame must stay buffered, got %q", got)
	}
	if _, err := bw.Write([]byte(": ping\n\n")); err != nil {
		t.Fatal(err)
	}
	pingOut := rec.Body.String()
	if !strings.Contains(pingOut, ": ping") {
		t.Fatalf("comments must flow immediately by default, got %q", pingOut)
	}
	if strings.Contains(pingOut, "held") {
		t.Fatalf("comment must not flush held data: %q", pingOut)
	}
	bw.Commit()
	if got := rec.Body.String(); !strings.Contains(got, "data: held") {
		t.Fatalf("commit must flush held frames, got %q", got)
	}
	// After commit everything passes through.
	bw.Write([]byte("data: live\n\n"))
	if got := rec.Body.String(); !strings.Contains(got, "data: live") {
		t.Fatalf("post-commit writes must pass through, got %q", got)
	}
}

func TestBootstrapBufferSuppressesCommentsWhenConfigured(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{SuppressKeepaliveBeforeFirstToken: true})
	rec := httptest.NewRecorder()
	bw := newBootstrapBuffer(rec)

	bw.Write([]byte(": ping\n\n"))
	if got := rec.Body.String(); got != "" {
		t.Fatalf("comments must be suppressed before first token, got %q", got)
	}
	bw.Commit()
	if _, err := bw.Write([]byte(": ping2\n\n")); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); !strings.Contains(got, ": ping2") {
		t.Fatalf("comments must pass after commit, got %q", got)
	}
}

func TestBootstrapBufferFrameBudgetCommits(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{BootstrapBufferFrames: 1})
	rec := httptest.NewRecorder()
	bw := newBootstrapBuffer(rec)

	bw.Write([]byte("data: one\n\n"))
	if got := rec.Body.String(); got != "" {
		t.Fatalf("first frame within budget must be held, got %q", got)
	}
	bw.Write([]byte("data: two\n\n"))
	got := rec.Body.String()
	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("budget overflow must commit held frames, got %q", got)
	}
	if !bw.committed {
		t.Fatal("buffer must be committed after overflow")
	}
}

func TestBootstrapBufferDiscardDropsHeldFrames(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{})
	rec := httptest.NewRecorder()
	bw := newBootstrapBuffer(rec)

	bw.Write([]byte("data: junk\n\n"))
	bw.discard()
	bw.Commit()
	if got := rec.Body.String(); strings.Contains(got, "junk") {
		t.Fatalf("discarded frames must never be written, got %q", got)
	}
}

const junkDeltaChunk = "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"
const goodStreamBody = "data: {\"choices\":[{\"delta\":{\"content\":\"from-retry\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

func TestServeChatStreamDiscardsJunkFromEmptyAttempt(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{})
	reqLog := testRequestLog("m")
	retry := func() (*http.Response, *Account, error) {
		return sseUpstream(goodStreamBody), nil, nil
	}

	rec := httptest.NewRecorder()
	serveChatStream(context.Background(), rec, sseUpstream(junkDeltaChunk), nil, reqLog, retry)

	out := rec.Body.String()
	if !strings.Contains(out, "from-retry") {
		t.Fatalf("retry content missing: %q", out)
	}
	if strings.Contains(out, "assistant") {
		t.Fatalf("abandoned attempt frames must be discarded: %q", out)
	}
	if n := strings.Count(out, "data: [DONE]"); n != 1 {
		t.Fatalf("expected exactly one [DONE], got %d", n)
	}
	if strings.Contains(out, "empty stream") {
		t.Fatalf("successful retry must not surface an empty-stream error: %q", out)
	}
	if !reqLog.Completed {
		t.Fatal("successful retry should finalize the log as completed")
	}
}

func TestServeResponsesStreamDiscardsLifecycleFromEmptyAttempt(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{})
	reqLog := testRequestLog("m")
	retry := func() (*http.Response, *Account, error) {
		return sseUpstream(goodStreamBody), nil, nil
	}

	rec := httptest.NewRecorder()
	serveResponsesStream(context.Background(), rec, sseUpstream(junkDeltaChunk), reqLog, nil, retry)

	out := rec.Body.String()
	if !strings.Contains(out, "from-retry") {
		t.Fatalf("retry content missing: %q", out)
	}
	if n := strings.Count(out, "event: response.created"); n != 1 {
		t.Fatalf("abandoned attempt lifecycle events must be discarded, created=%d in %q", n, out)
	}
	if !strings.Contains(out, "event: response.completed") {
		t.Fatalf("retry must complete cleanly: %q", out)
	}
}

func TestServeChatStreamRetriesStalledAttempt(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{FirstContentTimeoutSeconds: 1})
	reqLog := testRequestLog("m")

	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	stalled := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(pr),
	}
	retries := 0
	retry := func() (*http.Response, *Account, error) {
		retries++
		return sseUpstream(goodStreamBody), nil, nil
	}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveChatStream(context.Background(), rec, stalled, nil, reqLog, retry)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled attempt did not resolve")
	}
	if retries != 1 {
		t.Fatalf("expected exactly one retry, got %d", retries)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "from-retry") {
		t.Fatalf("retry content missing: %q", out)
	}
	if strings.Contains(out, "bootstrap deadline") {
		t.Fatalf("successful retry must not report the stall: %q", out)
	}
}

func TestFirstContentDeadlineInertWithoutBuffer(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{FirstContentTimeoutSeconds: 30})
	rec := httptest.NewRecorder()
	sink := newSSESink(rec)
	if sink.boot != nil {
		t.Fatal("plain ResponseWriter must not create a bootstrap buffer")
	}
	c, stop := firstContentDeadline(sink)
	defer stop()
	if c != nil {
		t.Fatal("first-content deadline must stay disabled for unbuffered sinks")
	}
}

func TestHeartbeatConfigKnobs(t *testing.T) {
	installStreamConfig(t, config.StreamPolicy{})
	if !streamHeartbeatActive() {
		t.Fatal("default config must keep heartbeats active")
	}
	if got := heartbeatIntervalOrDefault(); got != streamHeartbeatInterval {
		t.Fatalf("default interval = %v, want %v", got, streamHeartbeatInterval)
	}
	if _, stop := newHeartbeat(); stop == nil {
		t.Fatal("enabled heartbeat must return a stop function")
	} else {
		stop()
	}

	installStreamConfig(t, config.StreamPolicy{KeepaliveSeconds: 7})
	if got := heartbeatIntervalOrDefault(); got != 7*time.Second {
		t.Fatalf("configured interval = %v, want 7s", got)
	}

	installStreamConfig(t, config.StreamPolicy{KeepaliveSeconds: -1})
	if streamHeartbeatActive() {
		t.Fatal("KeepaliveSeconds=-1 must disable heartbeats")
	}
	c, stop := newHeartbeat()
	defer stop()
	if c != nil {
		t.Fatal("disabled heartbeat must return a nil channel")
	}
}

func TestServeChatStreamNoHeartbeatWhenDisabled(t *testing.T) {
	old := streamHeartbeatInterval
	streamHeartbeatInterval = 10 * time.Millisecond
	defer func() { streamHeartbeatInterval = old }()
	installStreamConfig(t, config.StreamPolicy{KeepaliveSeconds: -1})

	pr, pw := io.Pipe()
	rec := httptest.NewRecorder()
	reqLog := testRequestLog("m")
	done := make(chan streamResult, 1)
	go func() {
		done <- handleStreamResponse(context.Background(), rec, sseUpstreamBody(pr), nil, reqLog)
	}()
	time.Sleep(60 * time.Millisecond)
	fmt.Fprintf(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	pw.Close()

	select {
	case res := <-done:
		if !res.terminal {
			t.Fatalf("expected terminal result: %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not finish")
	}
	if out := rec.Body.String(); strings.Contains(out, ": ping") {
		t.Fatalf("disabled heartbeats must not emit ping comments: %q", out)
	}
}

func sseUpstreamBody(r io.Reader) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(r)}
}

// --- nonstream keepalive ---

type bodyWriteRecorder struct {
	mu      sync.Mutex
	writes  int
	headers http.Header
}

func (b *bodyWriteRecorder) Header() http.Header {
	if b.headers == nil {
		b.headers = http.Header{}
	}
	return b.headers
}

func (b *bodyWriteRecorder) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes += len(p)
	return len(p), nil
}

func (b *bodyWriteRecorder) WriteHeader(int) {}

func (b *bodyWriteRecorder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writes
}

func installNonstreamConfig(t *testing.T, secs int) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.NonstreamKeepaliveSeconds = secs
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })
}

func TestNonstreamKeepaliveDisabledByDefault(t *testing.T) {
	installNonstreamConfig(t, 0)
	rec := httptest.NewRecorder()
	if kw := newKeepaliveWriter(rec); kw != nil {
		kw.stop()
		t.Fatal("zero NonstreamKeepaliveSeconds must not wrap the writer")
	}
	time.Sleep(50 * time.Millisecond)
	if got := rec.Body.String(); got != "" {
		t.Fatalf("disabled keepalive must not write anything, got %q", got)
	}
}

func TestNonstreamKeepaliveWritesSpacesAndStops(t *testing.T) {
	installNonstreamConfig(t, 1)
	rec := &bodyWriteRecorder{}
	kw := newKeepaliveWriter(rec)
	if kw == nil {
		t.Fatal("expected a keepalive wrapper")
	}
	time.Sleep(1200 * time.Millisecond)
	kw.stop()
	kw.stop() // idempotent
	after := rec.count()
	if after == 0 {
		t.Fatal("expected at least one keepalive space while waiting")
	}
	time.Sleep(1200 * time.Millisecond)
	if got := rec.count(); got != after {
		t.Fatalf("writes must stop after stop(): before=%d after=%d", after, got)
	}
}

func TestNonstreamKeepaliveCommitsOnBodyWrite(t *testing.T) {
	installNonstreamConfig(t, 1)
	rec := &bodyWriteRecorder{}
	kw := newKeepaliveWriter(rec)
	if kw == nil {
		t.Fatal("expected a keepalive wrapper")
	}
	defer kw.stop()

	// First real body write commits; later ticks must not append spaces.
	if _, err := kw.Write([]byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	after := rec.count()
	if after != len(`{"ok":true}`) {
		t.Fatalf("body write must pass through exactly: %d", after)
	}
	time.Sleep(1200 * time.Millisecond)
	if got := rec.count(); got != after {
		t.Fatalf("committed writer must not receive keepalive spaces: before=%d after=%d", after, got)
	}
}
