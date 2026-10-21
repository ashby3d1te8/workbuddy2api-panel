package upstream

import (
	"strings"
	"testing"
)

// issue #533：DSH（DeepSeek Harness）等客户端的系统提示词开头句式被上游按整句拦截
// （400 code=11128）。这三句是「句式」而不是具体品牌，故按句式泛化覆盖。
func TestDSHSystemFingerprintRewritten(t *testing.T) {
	cases := []struct{ name, in string }{
		{"ai-agent", "You are an AI agent powered by DeepSeek Harness."},
		{"coding-agent-dotted-model", "You are a coding agent powered by the deepseek-v4.1-flash model."},
		{"built-on", "You are an AI assistant built on Acme Brand."},
		{"lower-case", "you are an ai agent powered by deepseek harness."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := []any{map[string]any{"role": "system", "content": c.in}}
			if !sanitizeMessages(msgs) {
				t.Fatal("sanitizeMessages 未报告改动")
			}
			got := msgs[0].(map[string]any)["content"].(string)
			if got != "You are a helpful AI assistant." {
				t.Errorf("改写不符合预期: %q", got)
			}
		})
	}
}

// developer 角色与 system 同等对待（OpenAI 新版把系统提示拆到 developer）。
func TestDSHDeveloperRewritten(t *testing.T) {
	msgs := []any{map[string]any{"role": "developer", "content": "You are an AI agent powered by X."}}
	if !sanitizeMessages(msgs) {
		t.Fatal("developer 角色未改写")
	}
	got := msgs[0].(map[string]any)["content"].(string)
	if strings.Contains(strings.ToLower(got), "powered by") {
		t.Errorf("指纹仍在: %q", got)
	}
}

// user / assistant / tool 里的同名文本是用户数据，不得改写。
func TestDSHNotTouchedOutsideSystemRoles(t *testing.T) {
	for _, role := range []string{"user", "assistant", "tool"} {
		t.Run(role, func(t *testing.T) {
			in := "You are an AI agent powered by DeepSeek Harness."
			msgs := []any{map[string]any{"role": role, "content": in}}
			if sanitizeMessages(msgs) {
				t.Fatal("非系统角色不该被改写")
			}
			if got := msgs[0].(map[string]any)["content"].(string); got != in {
				t.Errorf("内容被改动: %q", got)
			}
		})
	}
}

// 内嵌在大段 system prompt 里：只替换该句，前后文原样保留。
func TestDSHEmbeddedContextPreserved(t *testing.T) {
	in := "Intro line.\nYou are an AI agent powered by Foo.\nRest of prompt stays."
	msgs := []any{map[string]any{"role": "system", "content": in}}
	sanitizeMessages(msgs)
	got := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(got, "Intro line.") || !strings.Contains(got, "Rest of prompt stays.") {
		t.Errorf("上下文被破坏: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "powered by") {
		t.Errorf("指纹仍在: %q", got)
	}
}

// 幂等：改写后的文本再跑一次不该再变（否则每轮都可能抖动）。
func TestDSHIdempotent(t *testing.T) {
	in := "You are an AI agent powered by Foo.\nTail."
	msgs := []any{map[string]any{"role": "system", "content": in}}
	sanitizeMessages(msgs)
	first := msgs[0].(map[string]any)["content"].(string)
	msgs2 := []any{map[string]any{"role": "system", "content": first}}
	if sanitizeMessages(msgs2) {
		t.Fatal("二次净化报告了改动，说明不幂等")
	}
	if second := msgs2[0].(map[string]any)["content"].(string); second != first {
		t.Errorf("二次净化改了内容: %q -> %q", first, second)
	}
}

// 不相干的「You are ...」句子不能被误伤。
func TestDSHNonFingerprintUntouched(t *testing.T) {
	in := "You are a helpful assistant. Follow the user's instructions carefully."
	msgs := []any{map[string]any{"role": "system", "content": in}}
	if sanitizeMessages(msgs) {
		t.Fatal("无关文本被改写")
	}
}
