package session

import "testing"

// X-Conversation-ID 派生：显式值优先，否则取首条 user 消息（跨轮稳定）。
// 这是 2026-10-22 的新行为：此前缺失时一律回 ""，由 headers 层随机生成；
// 随机生成会把同一次会话在上游用量里拆成很多次（magpie packages/workbuddy/README.md）。
func TestResolveConversationIDFromFirstUser(t *testing.T) {
	explicit := []byte(`{"conversation_id":"abc","messages":[{"role":"user","content":"hi"}]}`)
	if got := ResolveConversationID(explicit); got != "abc" {
		t.Errorf("显式 conversation_id 应优先，实际 %q", got)
	}

	one := []byte(`{"messages":[{"role":"system","content":"s"},{"role":"user","content":"hello world"}]}`)
	a := ResolveConversationID(one)
	if len(a) != 32 {
		t.Fatalf("派生值应为 32 hex，实际 %q", a)
	}
	two := []byte(`{"messages":[{"role":"system","content":"s"},{"role":"user","content":"hello world"},{"role":"assistant","content":"x"},{"role":"user","content":"again"}]}`)
	if b := ResolveConversationID(two); b != a {
		t.Errorf("跨轮必须稳定: %q != %q", a, b)
	}
	three := []byte(`{"messages":[{"role":"user","content":"something else"}]}`)
	if c := ResolveConversationID(three); c == a {
		t.Error("不同会话不该同 id")
	}
	if got := ResolveConversationID([]byte(`{"messages":[{"role":"system","content":"s"}]}`)); got != "" {
		t.Errorf("无 user 消息时不该造 id，实际 %q", got)
	}
}
