package upstream

import (
	"net/http"
	"testing"
)

// 回归（2026-10-16）：上游对国际版 chat 强制校验 X-Conversation-* 三件套，
// 缺 X-Conversation-ID 直接 400 / code 11-128 / "conversation id missing"。
// 旧逻辑「客户端没给就不发」会让 DSH / Codex / Trae / ZCode 这类不带该字段的
// 客户端每一轮都失败，故改为「没给就自己生成一个」。
func TestConversationIDFabricatedWhenMissing(t *testing.T) {
	c := &Client{}
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v2/chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.injectConversationHeaders(req, ChatMeta{})

	for _, h := range []string{
		"X-Conversation-ID",
		"X-Conversation-Request-ID",
		"X-Conversation-Message-ID",
	} {
		if v := req.Header.Get(h); v == "" {
			t.Errorf("%s 未发送（三件套必须齐发）", h)
		}
	}
	if got := req.Header.Get("X-Conversation-ID"); len(got) != 32 {
		t.Errorf("X-Conversation-ID 应为 32 位 hex，实际 %q", got)
	}
}

// 客户端给了 conversationId 就原样透传，不被覆盖。
func TestConversationIDPassthrough(t *testing.T) {
	c := &Client{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v2/chat", nil)
	const given = "abc123abc123abc123abc123abc12345"
	c.injectConversationHeaders(req, ChatMeta{ConversationID: given})
	if got := req.Header.Get("X-Conversation-ID"); got != given {
		t.Errorf("透传值被改写: %q != %q", got, given)
	}
}
