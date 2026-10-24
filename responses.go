package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cline-go-proxy/internal/protocol/openaichat"
	responsesconv "cline-go-proxy/internal/protocol/responses"
)

// ============================================================================
// OpenAI Responses API (/v1/responses) ↔ chat/completions 双向转换
// 所有上游生效：zen 免费模型与 Cline 账号池均可通过该端点访问（Cursor 等客户端直连）。
// ============================================================================

func responsesToChat(body map[string]any) map[string]any {
	return responsesconv.ToChat(body)
}

func responsesInputToMessages(input any) []any {
	return responsesconv.InputToMessages(input)
}

func stringifyResponsesContent(content any) string {
	return responsesconv.StringifyContent(content)
}

func responsesToolsToChat(tools []any) []any {
	return responsesconv.ToolsToChat(tools)
}

// ============ 非流式响应转换 ============

func newResponseID(prefix string) string {
	return responsesconv.NewID(prefix)
}

func chatToResponses(chat map[string]any) map[string]any {
	return responsesconv.ChatToResponses(chat)
}

// ============ 流式响应转换（chat SSE → Responses SSE） ============

// chatStreamToResponses 逐行读取上游 chat SSE，转换为完整 Responses 事件生命周期。
// 请求日志/账号统计等编排留在根包，事件构造由 protocol/responses 负责。
//
// 与 chat 路径共用同一套流契约：上游静默时发心跳注释；必须看到终止哨兵
// （finish_reason 或 [DONE]）才算正常结束；已输出内容后断流会补发 error
// 事件而不是伪装成 response.completed；完全没有输出的尝试不写任何事件，
// 由调用方决定是否重试（预提交重试）。
func chatStreamToResponses(ctx context.Context, w http.ResponseWriter, upstream *http.Response, reqLog *RequestLog, acc *Account) streamResult {
	defer upstream.Body.Close()
	// Cancelling on return releases the pump goroutine even when this attempt
	// stops early (completed event, write failure) with lines still buffered.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	res := streamResult{}
	s := responsesconv.NewSSEWriter(w)
	st := &responsesconv.StreamState{}
	var latestUsage tokenUsage
	sawTerminal := false
	started := false
	start := func() {
		if !started {
			started = true
			s.Start()
		}
	}

	startedAt := time.Now()
	if reqLog != nil {
		startedAt = reqLog.StartedAt
	}
	finalize := func(ok bool, reason string) {
		res.usage = latestUsage
		if reqLog == nil {
			return
		}
		recordUsage(acc, upstream, reqLog, reqLog.Model, latestUsage)
		finalizeRequestLog(reqLog, latestUsage, res.firstOutputAt, startedAt, ok, reason)
	}
	gone := func() streamResult {
		upstream.Body.Close()
		res.clientGone = true
		res.usage = latestUsage
		if reqLog != nil {
			finalizeRequestLog(reqLog, latestUsage, res.firstOutputAt, startedAt, false, "client disconnected")
		}
		return res
	}

	lines := pumpSSE(ctx, upstream.Body)
	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()
	sink := newSSESink(w)

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
				if sawTerminal {
					start()
					s.Complete(st, responsesconv.Usage{
						Prompt: latestUsage.Prompt, Completion: latestUsage.Completion, Cached: latestUsage.Cached, Total: latestUsage.Total,
					})
					res.terminal = true
					finalize(true, "")
					return res
				}
				if res.produced {
					res.truncated = true
					s.Fail("upstream stream ended before completion", "upstream_stream_truncated")
					finalize(false, "upstream stream truncated before completion")
					return res
				}
				res.empty = true
				return res
			}

			line := ev.line
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(line[5:])
			if payload == "" {
				continue
			}
			if payload == "[DONE]" {
				sawTerminal = true
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(payload), &obj) != nil {
				continue
			}
			obj = unwrapDataEnvelope(obj)
			if reqLog != nil {
				restoreToolCallsInResponse(obj, reqLog.ToolNames)
			}
			if m, ok := obj["model"].(string); ok && m != "" {
				s.Model = m
			}
			if usage := parseTokenUsage(obj["usage"]); usage.Valid {
				latestUsage = mergeTokenUsage(latestUsage, usage)
			}
			if chatChunkFinishReason(obj) != "" {
				sawTerminal = true
			}
			delta := getNested(obj, "choices", 0, "delta")
			if delta == nil {
				delta = getNested(obj, "choices", 0)
			}
			if d, ok := delta.(map[string]any); ok {
				if chatChunkHasOutput(obj) {
					res.produced = true
					if res.firstOutputAt.IsZero() {
						res.firstOutputAt = time.Now()
					}
				}
				start()
				s.EmitDelta(d, st)
			}
		}
	}
}

// usageToResponses 把聚合的 tokenUsage 转成 Responses usage 结构。
func usageToResponses(u tokenUsage) map[string]any {
	return responsesconv.UsageToResponses(responsesconv.Usage{
		Prompt: u.Prompt, Completion: u.Completion, Cached: u.Cached, Total: u.Total,
	})
}

// ============ /v1/responses 入口 ============

func handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	model, _ := params["model"].(string)
	isStream, _ := params["stream"].(bool)
	log.Printf("  responses: model=%s stream=%v", model, isStream)

	// 管理员手动停用的模型在入口直接拒绝（不进列表，也不可点名调用）
	if model != "" && isModelDisabled(model) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "model " + model + " is disabled",
		})
		return
	}

	reqLog := RequestLog{StartedAt: time.Now(), Protocol: "responses", Model: model, Stream: isStream}

	if msg := freeOnlyRejection(model); msg != "" {
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": msg, "type": "invalid_request_error"},
		})
		return
	}

	chat := responsesToChat(params)
	// 清洗畸形 tool_calls（空 function.name / 孤儿 tool 结果），避免上游 400
	if msgs, ok := chat["messages"].([]any); ok {
		chat["messages"] = sanitizeMessages(msgs)
	}
	chatModel, _ := chat["model"].(string)

	// 模型组合：在普通路由之前分流（组合 ID 不是真实上游模型）。按配置顺序回退，
	// 默认不触发全局 ModelChain / free 链；流式在连接级（尚未向客户端写字节）可
	// 回退，建流后若上游空流则补发 in-band 错误，不再伪装成 response.completed。
	if combo, ok := findCombo(chatModel); ok {
		if !combo.Enabled {
			msg := fmt.Sprintf("model combo %q is disabled", chatModel)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		}
		reqLog.ToolNames = clampParamsToolNames(chat)
		run := runCombo(chat, combo, isStream, requestSessionID(chat, r.Header))
		applyComboTrace(&reqLog, combo, run)
		stampUpstream(&reqLog, chat)
		if run.Acc != nil {
			reqLog.AccountID = run.Acc.AccountID
			reqLog.AccountEmail = run.Acc.Email
		}
		if run.Err != nil {
			log.Printf("  responses combo %q error: %v", combo.ID, run.Err)
			msg := comboTraceSummary(combo, run)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, clineErrorHTTPStatus(run.Err), map[string]any{
				"error": map[string]string{"message": msg, "type": "api_error"},
			})
			return
		}
		defer run.Resp.Body.Close()
		if isStream {
			serveResponsesStream(r.Context(), w, run.Resp, &reqLog, run.Acc, nil)
			return
		}
		var raw map[string]any
		if err := json.NewDecoder(run.Resp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
		restoreToolCallsInResponse(out2, reqLog.ToolNames)
		usage := parseTokenUsage(out2["usage"])
		recordUsage(run.Acc, run.Resp, &reqLog, reqLog.Model, usage)
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		writeJSON(w, http.StatusOK, chatToResponses(out2))
		return
	}

	route := routeModel(chatModel)

	switch route {
	case "reject":
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "paid zen model rejected")
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", chatModel),
				"type":    "invalid_request_error",
			},
		})
		return
	case "zen":
		reqLog.Upstream = upstreamOpenCode
		zm, _ := resolveZenInfo(chatModel)
		out := maybeCompact(chat, zm, requestSessionID(chat, r.Header))
		if out.changed {
			log.Printf("  responses %s", out.note)
		}
		upResp, err := callZenAPI(chat, isStream)
		if err != nil {
			// zen 故障转移到 Cline 池：先截短超长工具名（Meta 后端硬限 ≤64）
			reqLog.ToolNames = clampParamsToolNames(chat)
			if fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(chat, isStream); attempted {
				if fbErr == nil {
					log.Printf("  responses failover: serving %q via cline pool", chatModel)
					stampUpstream(&reqLog, chat) // 归因实际服务方（provider / cline）
					if fm, ok := chat["model"].(string); ok && fm != "" {
						reqLog.Model = fm // zen 故障转移后记录实际服务模型
					}
					if fbAcc != nil {
						reqLog.AccountID = fbAcc.AccountID
						reqLog.AccountEmail = fbAcc.Email
					}
					defer fbResp.Body.Close()
					if isStream {
						serveResponsesStream(r.Context(), w, fbResp, &reqLog, fbAcc, func() (*http.Response, *Account, error) {
							rr, ra, rerr := callClineAPI(chat, true)
							if rerr == nil {
								stampUpstream(&reqLog, chat)
							}
							if m, ok := chat["model"].(string); ok && m != "" {
								reqLog.Model = m
							}
							return rr, ra, rerr
						})
						return
					}
					var raw map[string]any
					if err := json.NewDecoder(fbResp.Body).Decode(&raw); err != nil {
						finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
						writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
						return
					}
					out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
					restoreToolCallsInResponse(out2, reqLog.ToolNames)
					usage := parseTokenUsage(out2["usage"])
					recordUsage(fbAcc, fbResp, &reqLog, reqLog.Model, usage)
					finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
					writeJSON(w, http.StatusOK, chatToResponses(out2))
					return
				}
				// 兜底也失败：zen 的真实失败原因不能被吞 —— 最终错误带上两段，
				// 日志/UI 能直接看到 zen 为什么挂、Cline 兜底为什么也没接住
				err = fmt.Errorf("zen: %v; cline free chain: %w", err, fbErr)
			}
			log.Printf("  responses api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer upResp.Body.Close()

		if isStream {
			serveResponsesStream(r.Context(), w, upResp, &reqLog, nil, func() (*http.Response, *Account, error) {
				fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(chat, true)
				if !attempted {
					return nil, nil, fmt.Errorf("no cline fallback available")
				}
				if fbErr == nil {
					stampUpstream(&reqLog, chat)
					if m, ok := chat["model"].(string); ok && m != "" {
						reqLog.Model = m
					}
				}
				return fbResp, fbAcc, fbErr
			})
			return
		}
		var raw map[string]any
		if err := json.NewDecoder(upResp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
		usage := parseTokenUsage(out2["usage"])
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		writeJSON(w, http.StatusOK, chatToResponses(out2))

	default: // cline
		// Cline 通道：截短超长工具名并记录映射，回程由响应处理器还原（Meta 后端硬限 ≤64）
		// 主动压缩对 cline/provider 路由同样生效。
		zm := modelForCompaction(chatModel)
		compactForRequest(chat, zm, requestSessionID(chat, r.Header), "responses")
		reqLog.ToolNames = clampParamsToolNames(chat)
		upResp, acc, err := callClineAPI(chat, isStream)
		if err != nil && isContextLengthError(err) {
			if out := fallbackTruncate(chat, zm); out.changed {
				log.Printf("  responses %s", out.note)
				upResp, acc, err = callClineAPI(chat, isStream)
			}
		}
		if effectiveModel, ok := chat["model"].(string); ok && effectiveModel != "" {
			reqLog.Model = effectiveModel // 含回退后的实际服务模型
		}
		stampUpstream(&reqLog, chat) // 归因实际服务方（provider / opencode / cline）
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email // 失败也要记账号：否则日志显示不出实际被 402/429 的账号
		}
		if err != nil {
			log.Printf("  responses api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, clineErrorHTTPStatus(err), map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer upResp.Body.Close()
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}

		if isStream {
			serveResponsesStream(r.Context(), w, upResp, &reqLog, acc, func() (*http.Response, *Account, error) {
				rr, ra, rerr := callClineAPI(chat, true)
				if rerr == nil {
					stampUpstream(&reqLog, chat)
				}
				if m, ok := chat["model"].(string); ok && m != "" {
					reqLog.Model = m
				}
				return rr, ra, rerr
			})
			return
		}
		var raw map[string]any
		if err := json.NewDecoder(upResp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
		restoreToolCallsInResponse(out2, reqLog.ToolNames)
		usage := parseTokenUsage(out2["usage"])
		recordUsage(acc, upResp, &reqLog, reqLog.Model, usage)
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		writeJSON(w, http.StatusOK, chatToResponses(out2))
	}
}

// unwrapDataEnvelope 剥掉部分上游返回的 {data:{...}} 信封。
func unwrapDataEnvelope(obj map[string]any) map[string]any {
	return openaichat.UnwrapDataEnvelope(obj)
}
