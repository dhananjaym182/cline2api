package main

// 本地测试（按仓库策略 *_test.go 不提交）：跨测试共享的路由状态复位助手。
// combo_pin_test 等会话级测试依赖 resetRoutingState，把测试原子操作留下的
// 粘性会话/provider 冷却等在 t.Cleanup 中清空，避免测试间相互污染。如需
// 非流式 mock 传输，另见 free_model_test.go 的 freeModelRoundTripper。

// resetRoutingState 复位路由相关的全局可变状态：粘性会话钉（provider:model）
// 与 provider 级冷却表。仅在测试 Cleanup 中调用。
func resetRoutingState() {
	stickyMu.Lock()
	stickySessions = map[string]stickyPin{}
	stickyMu.Unlock()

	providersMu.Lock()
	for k := range providerCooldowns {
		delete(providerCooldowns, k)
	}
	providersMu.Unlock()
}
