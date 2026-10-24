package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ============================================================================
// zen 会话自动压缩（上下文超限摘要）
// 机制：估算请求体 token 超过 模型上下文 - max(输出, 缓冲) 时，
// 序列化历史 → 尾部保留 keepTokens 预算 → 用 zen 模型生成锚定摘要（增量更新）
// → 重组为 [system] + [摘要] + recent 尾部继续会话；摘要失败退回尾部截断。
// ============================================================================

const (
	toolOutputMaxChars = 2000 // 工具结果序列化截断长度
	defaultSummaryOut  = 4096
	// summaryInputMaxChars bounds the transcript sent to the summary model.
	// The omitted history can be hundreds of thousands of characters; free-tier
	// summarizers reject that, which used to make every summary fail and fall
	// back to lossy truncation.
	summaryInputMaxChars = 120000
)

// summaryTemplate 锚定摘要模板：固定章节结构，保证多轮压缩增量可读。
const summaryTemplate = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Objective
- [one or two brief sentences describing what the user is trying to accomplish]

## Important Details
- [constraints/preferences, decisions and why, important facts/assumptions, exact context needed to continue, or "(none)"]

## Work State
### Completed
- [finished work, verified facts, or changes made; otherwise "(none)"]

### Active
- [current work, partial changes, or investigation state; otherwise "(none)"]

### Blocked
- [blockers, failing commands, or unknowns; otherwise "(none)"]

## Next Move
1. [immediate concrete action, or "(none)"]
2. [next action if known, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, symbols, commands, error strings, URLs, and identifiers when known.
- Do not mention the summary process or that context was compacted.`

// compactState 会话压缩状态（跨轮次记忆，支持增量摘要）。
type compactState struct {
	summary string // 上一次生成的锚定摘要
	recent  string // 上一次保留的 recent 尾部文本
	updated time.Time
}

var (
	compactStates   = make(map[string]*compactState)
	compactStatesMu sync.Mutex
)

// startCompactCleanup 定期清理 24 小时未更新的会话压缩状态。
func startCompactCleanup() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-24 * time.Hour)
			compactStatesMu.Lock()
			for k, v := range compactStates {
				if v.updated.Before(cutoff) {
					delete(compactStates, k)
				}
			}
			compactStatesMu.Unlock()
		}
	}()
}

// ============ 消息序列化 ============

func strField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func msgText(m map[string]any) string {
	switch c := m["content"].(type) {
	case string:
		return c
	case []any:
		parts := []string{}
		for _, block := range c {
			if b, ok := block.(map[string]any); ok {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func truncateMsg(s string) string {
	if len(s) <= toolOutputMaxChars {
		return s
	}
	return s[:toolOutputMaxChars] + "\n[truncated]"
}

// serializeMsg 单条消息 → 一行式文本表示。
func serializeMsg(m map[string]any) string {
	switch strField(m, "role") {
	case "user":
		return "[User]: " + msgText(m)
	case "system":
		return "[System update]: " + msgText(m)
	case "tool":
		return "[Tool result]: " + truncateMsg(msgText(m))
	case "assistant":
		lines := []string{}
		if t := msgText(m); t != "" {
			lines = append(lines, "[Assistant]: "+t)
		}
		if r := strField(m, "reasoning_content"); r != "" {
			lines = append(lines, "[Assistant reasoning]: "+r)
		}
		if tc, ok := m["tool_calls"].([]any); ok {
			for _, c := range tc {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := cm["function"].(map[string]any)
				name, args := "", ""
				if fn != nil {
					name, _ = fn["name"].(string)
					args, _ = fn["arguments"].(string)
				}
				if name == "" {
					continue
				}
				lines = append(lines, fmt.Sprintf("[Assistant tool call]: %s(%s)", name, args))
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

// ============ 尾部预算选择 ============

type selectResult struct {
	head   []string // 待摘要的历史部分
	recent []string // 原样保留的尾部
	split  int      // 原始消息的切分索引
}

// selectRecent 从尾部往前累计 token 预算；放不下的消息拆成前缀进 head、后缀进 recent。
func selectRecent(serialized []string, keepTokens int) *selectResult {
	if len(serialized) == 0 || keepTokens <= 0 {
		return nil
	}
	total := 0
	split := len(serialized)
	var splitPrefix, splitSuffix string
	for i := len(serialized) - 1; i >= 0; i-- {
		next := total + estimateText(serialized[i])
		if next > keepTokens {
			remaining := keepTokens - total
			if remaining > 0 {
				remainingChars := remaining * 3
				rs := []rune(serialized[i])
				if len(rs) > remainingChars {
					splitPrefix = string(rs[:len(rs)-remainingChars])
					splitSuffix = string(rs[len(rs)-remainingChars:])
				} else {
					splitSuffix = serialized[i]
				}
				split = i + 1
			}
			break
		}
		total = next
		split = i
	}
	if split == 0 {
		return nil
	}
	head := make([]string, 0, split+1)
	head = append(head, serialized[:split]...)
	if splitPrefix != "" {
		head = append(head, splitPrefix)
	}
	recent := make([]string, 0, len(serialized)-split+1)
	if splitSuffix != "" {
		recent = append(recent, splitSuffix)
	}
	recent = append(recent, serialized[split:]...)
	return &selectResult{head: head, recent: recent, split: split}
}

// buildSummaryPrompt 组装摘要提示词（有旧摘要时增量更新）。
func buildSummaryPrompt(previousSummary string, context []string) string {
	var prefix string
	if previousSummary != "" {
		prefix = "Update the anchored summary below using the conversation history above.\n" +
			"Preserve still-true details, remove stale details, and merge in the new facts.\n" +
			"<previous-summary>\n" + previousSummary + "\n</previous-summary>"
	} else {
		prefix = "Create a new anchored summary from the conversation history."
	}
	parts := append([]string{prefix, summaryTemplate}, context...)
	return strings.Join(parts, "\n\n")
}

// generateSummary builds the summary through a small route chain instead of
// zen only. Zen's free tier is routinely rate-limited (429), and treating that
// as fatal made every compaction fall back to truncation — i.e. "auto compact
// never works". The chain tries the configured/request model, a free zen model
// while zen is healthy, and finally the Cline free chain.
func generateSummary(modelID, prompt string, maxSummary int) (string, error) {
	candidates := summaryModelCandidates(modelID)
	var failures []string
	for _, model := range candidates {
		text, err := callSummaryModel(model, prompt, maxSummary)
		if err == nil && strings.TrimSpace(text) != "" {
			if model != modelID {
				log.Printf("  compact: summary served by fallback model %q", model)
			}
			return strings.TrimSpace(text), nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", model, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s: empty summary", model))
		}
	}
	if len(failures) == 0 {
		return "", fmt.Errorf("no summary backend available")
	}
	return "", fmt.Errorf("all summary backends failed (%s)", strings.Join(failures, "; "))
}

// summaryModelCandidates returns the summary backends in preference order.
// When zen is in its failover window every zen candidate is skipped: each
// attempt costs several retries of backoff before the same 429.
func summaryModelCandidates(preferred string) []string {
	cfg := getZenConfig()
	zenDown := cfg.Failover && zenFailedNow()
	var out []string
	seen := map[string]bool{}
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		out = append(out, m)
	}
	if !zenDown {
		add(preferred)
	}
	add("free")
	if !zenDown {
		for _, m := range currentZenModels() {
			if isZenFreeModel(m) {
				add(m.ID)
				break
			}
		}
	}
	return out
}

// callSummaryModel routes one summary request: zen free models go through
// callZenAPI, everything else reuses the regular Cline/provider path (which
// also owns the free-chain fallback).
func callSummaryModel(model, prompt string, maxSummary int) (string, error) {
	body := map[string]any{
		"model":      model,
		"messages":   []any{map[string]any{"role": "user", "content": prompt}},
		"max_tokens": maxSummary,
		"stream":     false,
		// Summaries are mechanical: skip upstream reasoning to keep them fast
		// and cheap on free tiers ("none" is stripped from the Cline body).
		"reasoning_effort": "none",
	}
	var resp *http.Response
	var err error
	if zm, ok := resolveZenInfo(model); ok && isZenFreeModel(zm) {
		resp, err = callZenAPI(body, false)
	} else {
		resp, _, err = callClineAPI(body, false)
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", err
	}
	out := raw
	if data, ok := raw["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			out = d
		}
	}
	if choices, ok := out["choices"].([]any); ok && len(choices) > 0 {
		if ch, ok := choices[0].(map[string]any); ok {
			if msg, ok := ch["message"].(map[string]any); ok {
				if s, ok := msg["content"].(string); ok {
					return strings.TrimSpace(s), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no content in summary response")
}

// ============ Token 估算 ============
//
// The estimator is deliberately conservative (same 3-chars-per-token assumption
// ExtremeRouter uses): JSON-bytes/4 is ~20% optimistic for dense code, so
// requests reached the upstream context window before compaction fired.

// countTokenWeight returns a pessimistic character weight: non-whitespace
// ASCII counts 1 while every non-ASCII rune counts 3 (one CJK char ≈ 1 token).
func countTokenWeight(s string) int {
	weight := 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if r < utf8.RuneSelf {
			weight++
		} else {
			weight += 3
		}
	}
	return weight
}

// estimateText estimates the token count of plain text.
func estimateText(s string) int { return countTokenWeight(s) / 3 }

func estimateJSON(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b) / 4
}

// estimateRequestTokens estimates the input tokens of a chat request, counting
// message text, reasoning text, tool-call arguments, tool schemas and a
// top-level system prompt. It falls back to estimateJSON for shapes without
// recognisable messages.
func estimateRequestTokens(params map[string]any) int {
	weight := 0
	counted := false
	if msgs, ok := params["messages"].([]any); ok {
		for _, raw := range msgs {
			mm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			counted = true
			weight += countTokenWeight(msgText(mm))
			weight += countTokenWeight(strField(mm, "reasoning_content"))
			if tcs, ok := mm["tool_calls"].([]any); ok {
				for _, tc := range tcs {
					tcm, _ := tc.(map[string]any)
					fn, _ := tcm["function"].(map[string]any)
					if fn == nil {
						continue
					}
					weight += countTokenWeight(strField(fn, "name"))
					weight += countTokenWeight(strField(fn, "arguments"))
				}
			}
		}
	}
	if system := strField(params, "system"); system != "" {
		counted = true
		weight += countTokenWeight(system)
	}
	for _, key := range []string{"tools", "functions"} {
		v, ok := params[key]
		if !ok {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		weight += countTokenWeight(string(b))
	}
	if !counted {
		return estimateJSON(params)
	}
	return weight / 3
}

// requestSessionID 会话标识：优先 x-opencode-session 头，其次 body.session_id。
func requestSessionID(params map[string]any, hdr http.Header) string {
	if hdr != nil {
		if sid := hdr.Get("x-opencode-session"); sid != "" {
			return sid
		}
	}
	return strField(params, "session_id")
}

// compactOutcome 压缩结果描述。
type compactOutcome struct {
	changed bool
	note    string
}

// defaultCompactContextCeiling 压缩预算的保守封顶：cline-pool 远程同步会给未知模型
// 默认 1M 上下文（models_sync），但真实窗口往往小得多 —— 不封顶的话压缩阈值高达
// ~4MB，永远不触发，等上游先报错。此封顶只影响压缩/截断预算，展示值不变。
const defaultCompactContextCeiling = 128000

// effectiveCompactContext 返回用于压缩/截断预算的有效上下文窗口：
// 管理页显式锁定（MetaLocked）的值优先；否则把解析出的窗口封顶在保守上限。
func effectiveCompactContext(zm Model) int {
	if zm.MetaLocked && zm.Context > 0 {
		return zm.Context
	}
	context := zm.Context
	if context <= 0 {
		context = 200000
	}
	if context > defaultCompactContextCeiling {
		return defaultCompactContextCeiling
	}
	return context
}

// modelForCompaction resolves the best-known context/output metadata for a
// model so compaction budgets apply to every route (zen, Cline pool, custom
// provider), not only to zen models.
func modelForCompaction(model string) Model {
	model = strings.TrimSpace(model)
	if model == "" {
		return Model{}
	}
	if zm, ok := resolveZenInfo(model); ok {
		return zm
	}
	p := loadPool()
	poolMu.Lock()
	for _, m := range p.Models {
		if m.ID == model {
			poolMu.Unlock()
			return m
		}
	}
	poolMu.Unlock()
	if meta, ok := lookupClineModelMeta(model); ok {
		return Model{ID: model, Context: meta.Context, Output: meta.Output}
	}
	return Model{ID: model}
}

// compactForRequest applies proactive compaction and logs the outcome under the
// caller's label ("chat", "anthropic", "responses", "combo").
func compactForRequest(params map[string]any, zm Model, sessionID, label string) {
	if out := maybeCompact(params, zm, sessionID); out.changed {
		log.Printf("  %s %s", label, out.note)
	}
}

// maybeCompact 估算超限时执行摘要压缩并原地改写 params["messages"]。
func maybeCompact(params map[string]any, zm Model, sessionID string) compactOutcome {
	cfg := getZenConfig()
	if !cfg.Compaction.Auto {
		return compactOutcome{}
	}
	// Clients often send no session header (Anthropic /v1/messages has none), so
	// derive a stable per-conversation key; otherwise the previous summary was
	// never reused and every turn re-summarised the whole history.
	sessionID = compactSessionKey(params, sessionID)
	context := effectiveCompactContext(zm)
	buffer := cfg.Compaction.Buffer
	if buffer <= 0 {
		buffer = 20000
	}
	keep := cfg.Compaction.KeepTokens
	if keep <= 0 {
		keep = 8000
	}
	maxSum := cfg.Compaction.MaxSummary
	if maxSum <= 0 {
		maxSum = defaultSummaryOut
	}

	output := zm.Output
	if output < buffer {
		output = buffer
	}
	threshold := context - output
	est := estimateRequestTokens(params)
	if est <= threshold {
		return compactOutcome{}
	}

	messages, _ := params["messages"].([]any)
	if len(messages) == 0 {
		return compactOutcome{}
	}

	serialized := make([]string, 0, len(messages))
	for _, msg := range messages {
		if mm, ok := msg.(map[string]any); ok {
			serialized = append(serialized, serializeMsg(mm))
		}
	}

	sel := selectRecent(serialized, keep)
	if sel == nil || sel.split <= 0 {
		return compactOutcome{}
	}

	previousSummary := loadCompactState(sessionID).summary
	if previousSummary == "" {
		previousSummary = findExistingSummary(messages, sel.split)
	}
	st := loadCompactState(sessionID)

	head := clampSummaryHead(strings.Join(sel.head, "\n\n"))
	var contextParts []string
	if st.recent != "" {
		contextParts = append(contextParts, st.recent)
	}
	if head != "" {
		contextParts = append(contextParts, head)
	}
	if previousSummary == "" && head == "" && st.recent == "" {
		return compactOutcome{}
	}
	prompt := buildSummaryPrompt(previousSummary, contextParts)

	// A configured summary model wins; otherwise the request model is tried
	// first (cheap when it is a zen model) and generateSummary falls back to a
	// free zen model and then the Cline free chain.
	summaryModel := strings.TrimSpace(cfg.Compaction.SummaryModel)
	if summaryModel == "" {
		summaryModel = zm.ID
	}
	log.Printf("  compact: model=%s ctx=%d est=%d threshold=%d keep=%d split@%d/%d summary_model=%s",
		zm.ID, context, est, threshold, keep, sel.split, len(messages), summaryModel)
	summary, err := generateSummary(summaryModel, prompt, maxSum)
	if err != nil {
		log.Printf("  compact: summary generation failed (%v), falling back to truncation", err)
		return fallbackTruncate(params, zm)
	}

	var newMsgs []any
	for _, msg := range messages {
		if mm, ok := msg.(map[string]any); ok && strField(mm, "role") == "system" {
			newMsgs = append(newMsgs, mm)
		}
	}
	newMsgs = append(newMsgs, map[string]any{
		"role":    "system",
		"content": "[Conversation Summary]\n" + summary,
	})
	newMsgs = append(newMsgs, messages[sel.split:]...)

	updateCompactState(sessionID, summary, strings.Join(sel.recent, "\n\n"))
	params["messages"] = newMsgs
	return compactOutcome{
		changed: true,
		note:    fmt.Sprintf("[compacted via summary] summary_model=%s kept=%d msgs", summaryModel, len(messages)-sel.split),
	}
}

func loadCompactState(sessionID string) *compactState {
	if sessionID == "" {
		return &compactState{}
	}
	compactStatesMu.Lock()
	defer compactStatesMu.Unlock()
	st := compactStates[sessionID]
	if st != nil {
		return st
	}
	st = &compactState{}
	compactStates[sessionID] = st
	return st
}

func updateCompactState(sessionID, summary, recent string) {
	if sessionID == "" {
		return
	}
	compactStatesMu.Lock()
	defer compactStatesMu.Unlock()
	compactStates[sessionID] = &compactState{summary: summary, recent: recent, updated: time.Now()}
}

// compactSessionKey prefers the explicit session id and otherwise derives a
// stable fingerprint from the first system/user messages. The fingerprint keeps
// incremental summaries working for clients (Claude Code, Anthropic API) that
// do not send a session header.
func compactSessionKey(params map[string]any, sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	msgs, _ := params["messages"].([]any)
	var b strings.Builder
	haveUser := false
	for _, raw := range msgs {
		mm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strField(mm, "role") {
		case "system":
			if b.Len() == 0 {
				b.WriteString(tailRunes(msgText(mm), 400))
			}
		case "user":
			if haveUser {
				continue
			}
			haveUser = true
			b.WriteString("\x00")
			b.WriteString(tailRunes(msgText(mm), 400))
		}
		if b.Len() > 1600 {
			break
		}
	}
	if b.Len() == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "fp_" + hex.EncodeToString(sum[:12])
}

// clampSummaryHead bounds the transcript handed to the summary model, keeping
// the most recent part. Older facts are expected to live in the previous
// summary that buildSummaryPrompt already merges in.
func clampSummaryHead(head string) string {
	rs := []rune(head)
	if len(rs) <= summaryInputMaxChars {
		return head
	}
	return "[earlier history omitted for summary]\n" + string(rs[len(rs)-summaryInputMaxChars:])
}

func tailRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[len(rs)-n:])
}

// findExistingSummary 从已有消息中寻找客户端带来的历史摘要。
func findExistingSummary(messages []any, upTo int) string {
	if upTo < 0 || upTo > len(messages) {
		upTo = len(messages)
	}
	for i := 0; i < upTo; i++ {
		mm, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		c := msgText(mm)
		for _, prefix := range []string{"[Conversation Summary]", "[Previous Conversation Summary]"} {
			if strings.HasPrefix(c, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(c, prefix))
			}
		}
	}
	return ""
}

// fallbackTruncate 摘要失败时退回尾部截断：保留 system + 尾部消息至 60% 预算。
func fallbackTruncate(params map[string]any, zm Model) compactOutcome {
	messages, _ := params["messages"].([]any)
	if len(messages) == 0 {
		return compactOutcome{}
	}
	// 预算同 maybeCompact 走有效窗口（默认 1M 会被封顶到保守上限），零值 Model 也
	// 会落到 200k 默认并封顶，截断预算始终合理。
	ctxBudget := effectiveCompactContext(zm)
	budget := int(float64(ctxBudget) * 0.6)
	// Tool schemas are sent on every request and cannot be dropped here; leave
	// room for them so truncation does not keep the request over the window
	// anyway (which used to surface as an upstream 400 right after "compaction").
	if v, ok := params["tools"]; ok {
		if b, err := json.Marshal(v); err == nil {
			budget -= countTokenWeight(string(b)) / 3
		}
	}
	if budget < 1000 {
		budget = 1000
	}

	type idxMsg struct {
		idx int
		msg any
	}
	var kept []idxMsg
	used := 0
	for i, msg := range messages {
		if mm, ok := msg.(map[string]any); ok && strField(mm, "role") == "system" {
			kept = append(kept, idxMsg{i, msg})
			used += estimateText(msgText(mm))
		}
	}
	isKept := func(i int) bool {
		for _, k := range kept {
			if k.idx == i {
				return true
			}
		}
		return false
	}
	for i := len(messages) - 1; i >= 0 && used < budget; i-- {
		if isKept(i) {
			continue
		}
		mm, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		t := estimateText(msgText(mm))
		if used+t > budget {
			continue
		}
		kept = append(kept, idxMsg{i, messages[i]})
		used += t
	}
	sort.Slice(kept, func(a, b int) bool { return kept[a].idx < kept[b].idx })

	out := make([]any, 0, len(kept)+1)
	out = append(out, map[string]any{
		"role": "system",
		"content": fmt.Sprintf(
			"[context compaction] Conversation history exceeded the estimated model limit (~%d tokens); older messages were truncated to continue the session.", ctxBudget),
	})
	for _, k := range kept {
		out = append(out, k.msg)
	}
	params["messages"] = out
	return compactOutcome{changed: true, note: "[compacted via truncation]"}
}

// isContextLengthError reports whether err is an upstream 400 indicating the
// request exceeded the model's maximum context window.
func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context length") ||
		strings.Contains(msg, "maximum context") ||
		strings.Contains(msg, "context window") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "token limit")
}
