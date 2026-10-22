package prompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func msgsOf(t *testing.T, body []byte) []any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	m, _ := obj["messages"].([]any)
	return m
}

// 11128 二级兜底：system 必须整段搬进首条 user，且原文逐字保留。
func TestMoveIntoUser(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are an AI agent powered by DeepSeek Harness."},{"role":"user","content":"hi"}]}`)
	out, ok := MoveIntoUser(body, "OFFICIAL")
	if !ok {
		t.Fatal("未发生搬移")
	}
	msgs := msgsOf(t, out)
	if len(msgs) != 2 {
		t.Fatalf("消息数应为 2，实际 %d", len(msgs))
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "OFFICIAL" {
		t.Errorf("system 位置应为官方默认提示词，实际 %v", first)
	}
	second := msgs[1].(map[string]any)
	c, _ := second["content"].(string)
	if !strings.Contains(c, "<instructions>") {
		t.Errorf("未包 <instructions>: %q", c)
	}
	if !strings.Contains(c, "You are an AI agent powered by DeepSeek Harness.") {
		t.Errorf("system 原文丢失: %q", c)
	}
	if !strings.HasSuffix(c, "hi") {
		t.Errorf("用户原文应保持在尾部: %q", c)
	}
}

// 多段 system（system + developer）都要搬。
func TestMoveIntoUserMultipleSystem(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"developer","content":"B"},{"role":"user","content":"hi"}]}`)
	out, ok := MoveIntoUser(body, "OFFICIAL")
	if !ok {
		t.Fatal("未发生搬移")
	}
	msgs := msgsOf(t, out)
	if len(msgs) != 2 {
		t.Fatalf("两条 system 应合并为一条官方 system，实际消息数 %d", len(msgs))
	}
	c, _ := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(c, "A") || !strings.Contains(c, "B") {
		t.Errorf("合并内容缺失: %q", c)
	}
}

// 没有 system / 没有 user / 坏 JSON → 原样返回 false。
func TestMoveIntoUserNoop(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		[]byte(`{"messages":[{"role":"system","content":"A"}]}`),
		[]byte(`not json`),
		[]byte(``),
	}
	for _, b := range cases {
		if _, ok := MoveIntoUser(b, "OFFICIAL"); ok {
			t.Errorf("不该发生搬移: %s", string(b))
		}
	}
}
