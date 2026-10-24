package main

// Unified SSE forwarding contract for the chat and Responses endpoints.
//
// The Anthropic handler already interleaves upstream reads with a heartbeat
// ticker and distinguishes "nothing committed yet" from "content delivered".
// This file gives the other two streaming paths the same guarantees:
//
//   - a heartbeat comment keeps intermediaries from idling out a silent stream;
//   - a terminal sentinel ([DONE] or finish_reason) is required, so a stream
//     that dies mid-answer is reported as an error instead of a clean stop;
//   - an attempt that produced no semantic output stays retryable by the
//     caller because only comments were written downstream.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	responsesconv "cline-go-proxy/internal/protocol/responses"
)

// streamHeartbeatInterval is a variable so tests can shorten it.
var streamHeartbeatInterval = 15 * time.Second

// streamHeartbeatActive reports whether heartbeats should run, honoring
// StreamPolicy.KeepaliveSeconds: 0/absent keeps the built-in interval, -1
// disables heartbeats entirely.
func streamHeartbeatActive() bool {
	if s := getProxyConfig().Stream.KeepaliveSeconds; s < 0 {
		return false
	}
	return streamHeartbeatInterval > 0
}

// heartbeatInterval returns the ticker period: the built-in default when
// KeepaliveSeconds is unset, otherwise the configured interval.
func heartbeatIntervalOrDefault() time.Duration {
	if s := getProxyConfig().Stream.KeepaliveSeconds; s > 0 {
		return time.Duration(s) * time.Second
	}
	return streamHeartbeatInterval
}

// effectiveBootstrapRetries: StreamPolicy.BootstrapRetries=0 must preserve the
// historic single retry, so it maps to 1; N maps to N attempts after the first.
func effectiveBootstrapRetries() int {
	if n := getProxyConfig().Stream.BootstrapRetries; n > 0 {
		return n
	}
	return 1
}

// newHeartbeat returns a ticking channel for stream keepalive, or a nil
// channel when heartbeats are disabled (a nil channel never fires in select).
func newHeartbeat() (<-chan time.Time, func()) {
	if !streamHeartbeatActive() {
		return nil, func() {}
	}
	t := time.NewTicker(heartbeatIntervalOrDefault())
	return t.C, t.Stop
}

// keepaliveWriter serializes a background non-stream keepalive with the
// handler's own writes to http.ResponseWriter (which is not safe for
// concurrent use). It periodically writes a single space while the handler
// blocks on the upstream call, so byte-level read timeouts in intermediaries
// do not fire on long generations; JSON parsers ignore the leading whitespace.
// The first real (non-keepalive) write commits the response and permanently
// stops the keepalive. Cooperative writers (writeJSON, httpx.WriteAPI,
// copyPassthroughHeaders) hold the mutex across their whole header+status+
// body sequence via WithLock, so the keepalive tick can never interleave
// with a header-map mutation. Note the documented tradeoff: a space written
// before an upstream error commits the status as 200 with the JSON error as
// body.
type keepaliveWriter struct {
	w         http.ResponseWriter
	mu        sync.Mutex
	committed bool
	held      bool // mu is held by the current body writer (WithLock)
	done      chan struct{}
	closeOnce sync.Once
}

// newKeepaliveWriter wraps w when ProxyConfig.NonstreamKeepaliveSeconds > 0,
// otherwise returns nil (no wrapping, historic behavior).
func newKeepaliveWriter(w http.ResponseWriter) *keepaliveWriter {
	secs := getProxyConfig().NonstreamKeepaliveSeconds
	if secs <= 0 {
		return nil
	}
	k := &keepaliveWriter{w: w, done: make(chan struct{})}
	interval := time.Duration(secs) * time.Second
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-k.done:
				return
			case <-t.C:
				k.tick()
			}
		}
	}()
	return k
}

func (k *keepaliveWriter) tick() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.committed {
		return
	}
	if _, err := io.WriteString(k.w, " "); err == nil {
		if f, ok := k.w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// stop terminates the keepalive goroutine; safe to call multiple times and
// from a defer even after the body committed.
func (k *keepaliveWriter) stop() {
	k.closeOnce.Do(func() { close(k.done) })
	k.mu.Lock()
	k.committed = true
	k.mu.Unlock()
}

// WithLock lets a cooperative writer hold the mutex across the whole
// header-mutation + WriteHeader + Write sequence. Inner Write/WriteHeader/
// Flush calls become pass-through while the lock is held.
func (k *keepaliveWriter) WithLock(f func()) {
	k.mu.Lock()
	k.committed = true
	k.held = true
	f()
	k.held = false
	k.mu.Unlock()
}

func (k *keepaliveWriter) Header() http.Header { return k.w.Header() }

func (k *keepaliveWriter) WriteHeader(code int) {
	if k.held {
		k.w.WriteHeader(code)
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.committed = true
	k.w.WriteHeader(code)
}

func (k *keepaliveWriter) Write(p []byte) (int, error) {
	if k.held {
		return k.w.Write(p)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.committed = true
	return k.w.Write(p)
}

func (k *keepaliveWriter) Flush() {
	if k.held {
		if f, ok := k.w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if f, ok := k.w.(http.Flusher); ok {
		f.Flush()
	}
}

// firstContentDeadline returns a timer channel that fires when a bootstrap
// attempt has produced no semantic content within FirstContentTimeoutSeconds.
// Zero config or an unbuffered sink yields a nil channel (never fires).
func firstContentDeadline(sink *sseSink) (<-chan time.Time, func()) {
	secs := getProxyConfig().Stream.FirstContentTimeoutSeconds
	if secs <= 0 || sink.boot == nil {
		return nil, func() {}
	}
	t := time.NewTimer(time.Duration(secs) * time.Second)
	return t.C, func() { t.Stop() }
}

// streamResult describes how one upstream SSE attempt ended.
type streamResult struct {
	// produced is true once a semantic delta (text, reasoning, tool calls) was
	// forwarded. Protocol envelopes and role-only deltas do not count.
	produced bool
	// terminal is true when the upstream sent its protocol terminator.
	terminal bool
	// empty means the upstream ended with neither output nor a terminator, so
	// the caller may retry on another attempt/credential.
	empty bool
	// truncated means output was already forwarded and the stream then ended
	// without a terminator.
	truncated     bool
	clientGone    bool
	usage         tokenUsage
	firstOutputAt time.Time
	// stalled means the bootstrap first-content deadline expired while the
	// attempt had produced nothing; like empty, it stays retryable.
	stalled bool
}

type sseRead struct {
	line string
	err  error
}

// pumpSSE reads complete lines off body in a goroutine so the forwarding loop
// can interleave heartbeats and cancellation without blocking on Read.
func pumpSSE(ctx context.Context, body io.ReadCloser) <-chan sseRead {
	out := make(chan sseRead, 64)
	go func() {
		defer close(out)
		reader := bufio.NewReader(body)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				select {
				case out <- sseRead{line: strings.TrimRight(line, "\r\n")}:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					select {
					case out <- sseRead{err: err}:
					case <-ctx.Done():
					}
				}
				return
			}
		}
	}()
	return out
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
	boot    *bootstrapBuffer
}

func newSSESink(w http.ResponseWriter) *sseSink {
	f, _ := w.(http.Flusher)
	s := &sseSink{w: w, flusher: f}
	if bw, ok := w.(*bootstrapBuffer); ok {
		s.boot = bw
	}
	return s
}

// release flushes any held pre-content frames (terminal attempt, budget
// overflow commit point). No-op for unbuffered sinks.
func (s *sseSink) release() {
	if s.boot != nil {
		s.boot.Commit()
	}
}

// bootstrapBuffer wraps the downstream ResponseWriter and holds data frames
// written before the first semantic content. A failed attempt then discards
// its frames instead of leaking upstream duplicates (role envelopes, stray
// lifecycle events) before a retry. SSE comments (: ping) are forwarded
// immediately — keeping intermediaries warm — unless
// Stream.SuppressKeepaliveBeforeFirstToken is set. The buffer self-commits
// when the held bytes or frames exceed the configured bootstrap budget.
type bootstrapBuffer struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	buf       []byte
	frames    int
	maxBytes  int
	maxFrames int
	suppress  bool
	committed bool
}

func newBootstrapBuffer(w http.ResponseWriter) *bootstrapBuffer {
	cfg := getProxyConfig().Stream
	f, _ := w.(http.Flusher)
	maxBytes := cfg.BootstrapBufferBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	maxFrames := cfg.BootstrapBufferFrames
	if maxFrames <= 0 {
		maxFrames = 64
	}
	return &bootstrapBuffer{
		w: w, flusher: f,
		maxBytes:  maxBytes,
		maxFrames: maxFrames,
		suppress:  cfg.SuppressKeepaliveBeforeFirstToken,
	}
}

func (b *bootstrapBuffer) Header() http.Header { return b.w.Header() }

func (b *bootstrapBuffer) WriteHeader(code int) { b.w.WriteHeader(code) }

func (b *bootstrapBuffer) Write(p []byte) (int, error) {
	if b.committed || len(p) == 0 {
		return b.w.Write(p)
	}
	if p[0] == ':' {
		if b.suppress {
			return len(p), nil
		}
		if _, err := b.w.Write(p); err != nil {
			return len(p), err
		}
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	b.frames++
	if len(b.buf) > b.maxBytes || b.frames > b.maxFrames {
		b.Commit()
	}
	return len(p), nil
}

func (b *bootstrapBuffer) Flush() {
	if b.committed || !b.suppress {
		if b.flusher != nil {
			b.flusher.Flush()
		}
	}
}

// Commit flushes every held frame downstream and switches to pass-through.
func (b *bootstrapBuffer) Commit() {
	if b.committed {
		return
	}
	b.committed = true
	if len(b.buf) > 0 {
		_, _ = b.w.Write(b.buf)
		b.buf = nil
	}
	if b.flusher != nil {
		b.flusher.Flush()
	}
}

// discard drops held frames between bootstrap attempts. Comments already
// delivered cannot be recalled, which is intentional: they carry no content
// and SSE parsers ignore them.
func (b *bootstrapBuffer) discard() {
	if !b.committed {
		b.buf = nil
		b.frames = 0
	}
}

func (s *sseSink) write(p string) error {
	if _, err := io.WriteString(s.w, p); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func (s *sseSink) heartbeat() bool { return s.write(": ping\n\n") == nil }

func (s *sseSink) commitSSEHeaders() {
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("Access-Control-Allow-Origin", "*")
	s.w.WriteHeader(http.StatusOK)
}

// chatChunkHasOutput reports whether a chat.completion chunk carries generated
// content (text, reasoning, or tool calls) that a retry would duplicate.
func chatChunkHasOutput(obj map[string]any) bool {
	if hasFirstOutput(obj) {
		return true
	}
	if delta, ok := getNested(obj, "choices", 0, "delta").(map[string]any); ok {
		if r, _ := delta["reasoning_content"].(string); r != "" {
			return true
		}
	}
	if msg, ok := getNested(obj, "choices", 0, "message").(map[string]any); ok {
		if r, _ := msg["reasoning_content"].(string); r != "" {
			return true
		}
	}
	return false
}

// chatChunkFinishReason returns the first non-empty finish_reason in a chunk.
func chatChunkFinishReason(obj map[string]any) string {
	choices, _ := getNested(obj, "choices").([]any)
	for _, c := range choices {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		if fr, _ := cm["finish_reason"].(string); fr != "" {
			return fr
		}
	}
	return ""
}

// emitChatStreamError reports a post-commit failure in-band. The client sees an
// error object instead of a silent clean stop, and no [DONE] is sent.
func emitChatStreamError(w http.ResponseWriter, message string) {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{"message": message, "type": "api_error"},
	})
	_ = newSSESink(w).write("data: " + string(payload) + "\n\n")
}

// emitResponsesStreamError reports a post-commit failure in-band for the
// Responses endpoint. No response.completed event is emitted.
func emitResponsesStreamError(w http.ResponseWriter, code, message string) {
	responsesconv.NewSSEWriter(w).Fail(message, code)
}

// handleStreamResponse forwards an OpenAI chat SSE stream.
//
// The caller commits SSE headers first so heartbeat comments can flow while the
// upstream is silent. This function reports whether anything semantic was
// produced and finalizes the request log once an attempt is terminal,
// truncated, or the client leaves. An empty attempt is not finalized so the
// caller can retry on the same downstream response.
func handleStreamResponse(ctx context.Context, w http.ResponseWriter, upstream *http.Response, acc *Account, reqLog *RequestLog) streamResult {
	defer upstream.Body.Close()
	// Cancelling on return releases the pump goroutine even when this attempt
	// stops early (sentinel seen, write failure) with lines still buffered.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	res := streamResult{}
	sink := newSSESink(w)

	lines := pumpSSE(ctx, upstream.Body)
	hb, stopHB := newHeartbeat()
	defer stopHB()
	stallC, stopStall := firstContentDeadline(sink)
	defer stopStall()

	var latestUsage tokenUsage
	sawTerminal := false
	sawDone := false

	finalize := func(ok bool, reason string) {
		res.usage = latestUsage
		if reqLog == nil {
			return
		}
		recordUsage(acc, upstream, reqLog, reqLog.Model, latestUsage)
		finalizeRequestLog(reqLog, latestUsage, res.firstOutputAt, reqLog.StartedAt, ok, reason)
	}

	gone := func() streamResult {
		upstream.Body.Close()
		res.clientGone = true
		res.usage = latestUsage
		if reqLog != nil {
			finalizeRequestLog(reqLog, latestUsage, res.firstOutputAt, reqLog.StartedAt, false, "client disconnected")
		}
		return res
	}

	write := func(p string) bool {
		if err := sink.write(p); err != nil {
			return false
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return gone()

		case <-hb:
			if !sink.heartbeat() {
				return gone()
			}

		case <-stallC:
			// Bootstrap window without first content: abort this attempt as
			// retryable (same contract as empty). Once content committed the
			// deadline is inert — the timer already fired or was stopped.
			if sink.boot != nil && !sink.boot.committed {
				upstream.Body.Close()
				res.stalled = true
				return res
			}

		case ev, ok := <-lines:
			if !ok || ev.err != nil {
				if ev.err != nil && !sawTerminal {
					log.Printf("  chat stream broken: %v", ev.err)
				}
				if sawTerminal {
					// Some upstreams close after the finish_reason chunk without
					// the [DONE] sentinel. Synthesize it so clients that wait for
					// the sentinel still terminate cleanly.
					if !sawDone {
						_ = write("data: [DONE]\n\n")
					}
					sink.release()
					res.terminal = true
					finalize(true, "")
					return res
				}
				if res.produced {
					res.truncated = true
					emitChatStreamError(w, "upstream stream ended before completion")
					finalize(false, "upstream stream truncated before completion")
					return res
				}
				res.empty = true
				return res
			}

			line := ev.line
			if !strings.HasPrefix(line, "data:") {
				// Comments and other non-data lines are forwarded verbatim.
				if !write(line + "\n") {
					return gone()
				}
				continue
			}

			payload := strings.TrimSpace(line[5:])
			if payload == "" {
				if !write(line + "\n\n") {
					return gone()
				}
				continue
			}
			if payload == "[DONE]" {
				if !write("data: [DONE]\n\n") {
					return gone()
				}
				sawDone = true
				sawTerminal = true
				sink.release()
				res.terminal = true
				finalize(true, "")
				return res
			}

			var obj map[string]any
			if err := json.Unmarshal([]byte(payload), &obj); err == nil {
				obj = unwrapDataEnvelope(obj)
				normalized := normalizeOpenAIResponse(obj)
				if reqLog != nil {
					restoreToolCallsInResponse(normalized, reqLog.ToolNames)
				}
				if usage := parseTokenUsage(normalized["usage"]); usage.Valid {
					latestUsage = mergeTokenUsage(latestUsage, usage)
				}
				if chatChunkHasOutput(normalized) {
					res.produced = true
					if res.firstOutputAt.IsZero() {
						res.firstOutputAt = time.Now()
					}
				}
				if chatChunkFinishReason(normalized) != "" {
					sawTerminal = true
				}
				if normBytes, err := json.Marshal(normalized); err == nil {
					if !write("data: " + string(normBytes) + "\n\n") {
						return gone()
					}
					if res.produced {
						// First semantic content: everything held so far (and this
						// frame) goes downstream together; later frames stream
						// through directly.
						sink.release()
					}
					continue
				}
			}
			// Unparseable data frames are forwarded unchanged.
			if !write(line + "\n") {
				return gone()
			}
		}
	}
}

// serveChatStream forwards a chat SSE stream with bootstrap retries: an
// attempt that ends without any semantic content (upstream break, empty
// completion, first-content stall) discards its buffered frames and retries
// through the retry callback up to Stream.BootstrapRetries times (default 1,
// the historic behavior). A nil retry means the caller has no failover path;
// the failure is then reported in-band.
func serveChatStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, acc *Account, reqLog *RequestLog, retry func() (*http.Response, *Account, error)) {
	copyPassthroughHeaders(w, resp.Header)
	newSSESink(w).commitSSEHeaders()
	run := func(ctx context.Context, bw *bootstrapBuffer, current *http.Response, currentAcc *Account) streamResult {
		return handleStreamResponse(ctx, bw, current, currentAcc, reqLog)
	}
	serveWithBootstrap(ctx, w, resp, acc, reqLog, retry, "chat stream",
		"upstream returned an empty stream", emitChatStreamError, run)
}

// serveResponsesStream runs a Responses stream attempt with the same bootstrap
// retry semantics as serveChatStream.
func serveResponsesStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, reqLog *RequestLog, acc *Account, retry func() (*http.Response, *Account, error)) {
	copyPassthroughHeaders(w, resp.Header)
	newSSESink(w).commitSSEHeaders()
	run := func(ctx context.Context, bw *bootstrapBuffer, current *http.Response, currentAcc *Account) streamResult {
		return chatStreamToResponses(ctx, bw, current, reqLog, currentAcc)
	}
	serveWithBootstrap(ctx, w, resp, acc, reqLog, retry, "responses stream",
		"upstream returned an empty stream",
		func(w http.ResponseWriter, msg string) { emitResponsesStreamError(w, "upstream_stream_empty", msg) },
		run)
}

// serveWithBootstrap is the shared retry loop for the chat and Responses
// streaming paths. Each attempt writes into a fresh bootstrap buffer, so an
// abandoned attempt leaves no upstream frames downstream (headers and
// heartbeat comments aside). exhaustedAttempts produce a single in-band error
// on the committed response.
func serveWithBootstrap(
	ctx context.Context,
	w http.ResponseWriter,
	resp *http.Response,
	acc *Account,
	reqLog *RequestLog,
	retry func() (*http.Response, *Account, error),
	label, emptyMsg string,
	emitError func(http.ResponseWriter, string),
	run func(context.Context, *bootstrapBuffer, *http.Response, *Account) streamResult,
) {
	maxBoot := effectiveBootstrapRetries()
	current, currentAcc := resp, acc
	for attempt := 0; ; attempt++ {
		bw := newBootstrapBuffer(w)
		res := run(ctx, bw, current, currentAcc)
		if res.clientGone {
			return
		}
		if !res.empty && !res.stalled {
			return // terminal or truncated: already delivered downstream
		}
		bw.discard()
		message := emptyMsg
		if res.stalled {
			message = "upstream produced no content before the bootstrap deadline"
		}
		if retry == nil || attempt >= maxBoot {
			emitError(w, message)
			finalizeRequestLog(reqLog, res.usage, res.firstOutputAt, reqLog.StartedAt, false, message)
			return
		}
		if currentAcc != nil && reqLog.Model != "" {
			log.Printf("  %s: model %q produced no content, cooling %s and retrying (%d/%d)", label, reqLog.Model, streamFailoverCooldown, attempt+1, maxBoot)
			setModelCooldown(currentAcc, reqLog.Model, time.Now().Add(streamFailoverCooldown))
		}
		retryResp, retryAcc, err := retry()
		if err != nil || retryResp == nil || retryResp.Body == nil {
			if err == nil {
				err = fmt.Errorf("retry returned no response")
			}
			emitError(w, message+": "+err.Error())
			finalizeRequestLog(reqLog, res.usage, res.firstOutputAt, reqLog.StartedAt, false, message)
			return
		}
		current, currentAcc = retryResp, retryAcc
		if retryAcc != nil {
			reqLog.AccountID = retryAcc.AccountID
			reqLog.AccountEmail = retryAcc.Email
		}
	}
}
