package upstream

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// IsSessionDead 的判定边界：401（含 HTML 边缘层）与 12153 算，其余一律不算。
func TestIsSessionDead(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"401 html 边缘层", &Error{Kind: ErrClient, Status: http.StatusUnauthorized, Msg: "<html>401 Authorization Required</html>"}, true},
		{"12153 kind", &Error{Kind: ErrSessionDead, Status: http.StatusUnauthorized, Msg: "code=12153"}, true},
		{"12153 但状态码 200（不应发生，仍按 kind 认）", &Error{Kind: ErrSessionDead, Status: http.StatusOK}, true},
		{"429 限流", &Error{Kind: ErrSoftRate, Status: http.StatusTooManyRequests}, false},
		{"400 参数", &Error{Kind: ErrBadParams, Status: http.StatusBadRequest}, false},
		{"500", &Error{Kind: ErrServer, Status: http.StatusInternalServerError}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSessionDead(c.err); got != c.want {
				t.Errorf("IsSessionDead() = %v, want %v", got, c.want)
			}
		})
	}

	// 被包装过的错误也要认（调度器里 err 常常被 %w 包一层）。
	wrapped := fmt.Errorf("balance uid=x: %w", &Error{Kind: ErrClient, Status: 401})
	if !IsSessionDead(wrapped) {
		t.Error("包装后的 401 应被识别")
	}
}
