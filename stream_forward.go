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
	"time"

	responsesconv "cline-go-proxy/internal/protocol/responses"
)

// streamHeartbeatInterval is a variable so tests can shorten it.
var streamHeartbeatInterval = 15 * time.Second

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
}

func newSSESink(w http.ResponseWriter) *sseSink {
	f, _ := w.(http.Flusher)
	return &sseSink{w: w, flusher: f}
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
	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()

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

		case <-heartbeat.C:
			if !sink.heartbeat() {
				return gone()
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

// serveChatStream runs one chat stream attempt, retrying once when the upstream
// closed before producing any content. A nil retry means the caller has no
// failover path; the failure is then reported in-band.
func serveChatStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, acc *Account, reqLog *RequestLog, retry func() (*http.Response, *Account, error)) {
	newSSESink(w).commitSSEHeaders()
	res := handleStreamResponse(ctx, w, resp, acc, reqLog)
	if !res.empty || res.clientGone {
		return
	}
	if retry == nil {
		emitChatStreamError(w, "upstream returned an empty stream")
		finalizeRequestLog(reqLog, res.usage, res.firstOutputAt, reqLog.StartedAt, false, "empty upstream stream")
		return
	}
	if acc != nil && reqLog.Model != "" {
		log.Printf("  chat stream: model %q produced no content, cooling %s and retrying once", reqLog.Model, streamFailoverCooldown)
		setModelCooldown(acc, reqLog.Model, time.Now().Add(streamFailoverCooldown))
	}
	retryResp, retryAcc, err := retry()
	if err != nil || retryResp == nil || retryResp.Body == nil {
		if err == nil {
			err = fmt.Errorf("retry returned no response")
		}
		emitChatStreamError(w, "upstream returned an empty stream: "+err.Error())
		finalizeRequestLog(reqLog, res.usage, res.firstOutputAt, reqLog.StartedAt, false, "empty upstream stream")
		return
	}
	defer retryResp.Body.Close()
	if retryAcc != nil {
		reqLog.AccountID = retryAcc.AccountID
		reqLog.AccountEmail = retryAcc.Email
	}
	res2 := handleStreamResponse(ctx, w, retryResp, retryAcc, reqLog)
	if res2.empty && !res2.clientGone {
		emitChatStreamError(w, "upstream returned an empty stream")
		finalizeRequestLog(reqLog, res2.usage, res2.firstOutputAt, reqLog.StartedAt, false, "empty upstream stream")
	}
}

// serveResponsesStream runs one Responses stream attempt with the same
// pre-commit retry semantics as serveChatStream.
func serveResponsesStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, reqLog *RequestLog, acc *Account, retry func() (*http.Response, *Account, error)) {
	newSSESink(w).commitSSEHeaders()
	res := chatStreamToResponses(ctx, w, resp, reqLog, acc)
	if !res.empty || res.clientGone {
		return
	}
	if retry == nil {
		emitResponsesStreamError(w, "upstream_stream_empty", "upstream returned an empty stream")
		finalizeRequestLog(reqLog, res.usage, res.firstOutputAt, reqLog.StartedAt, false, "empty upstream stream")
		return
	}
	if acc != nil && reqLog.Model != "" {
		log.Printf("  responses stream: model %q produced no content, cooling %s and retrying once", reqLog.Model, streamFailoverCooldown)
		setModelCooldown(acc, reqLog.Model, time.Now().Add(streamFailoverCooldown))
	}
	retryResp, retryAcc, err := retry()
	if err != nil || retryResp == nil || retryResp.Body == nil {
		if err == nil {
			err = fmt.Errorf("retry returned no response")
		}
		emitResponsesStreamError(w, "upstream_stream_empty", "upstream returned an empty stream: "+err.Error())
		finalizeRequestLog(reqLog, res.usage, res.firstOutputAt, reqLog.StartedAt, false, "empty upstream stream")
		return
	}
	defer retryResp.Body.Close()
	if retryAcc != nil {
		reqLog.AccountID = retryAcc.AccountID
		reqLog.AccountEmail = retryAcc.Email
	}
	res2 := chatStreamToResponses(ctx, w, retryResp, reqLog, retryAcc)
	if res2.empty && !res2.clientGone {
		emitResponsesStreamError(w, "upstream_stream_empty", "upstream returned an empty stream")
		finalizeRequestLog(reqLog, res2.usage, res2.firstOutputAt, reqLog.StartedAt, false, "empty upstream stream")
	}
}
