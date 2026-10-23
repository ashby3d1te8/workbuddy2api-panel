package upstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// 端到端验证「巡检能认出被顶号」：用 httptest 造一个返回 openresty HTML 401 的
// 上游，走真实的 UserResourceDetailedWithExpiry → billingJSON → doJSON → *Error
// 链路，断言最终被 IsSessionDead 判为会话失效。
//
// 这是调度器那条分支的真实依据：余额刷新拿到的 err 必须带 Status=401，否则
// IsSessionDead 永远为假，整条防护就是死代码。
func TestSessionDeadThroughBillingCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "<html><head><title>401 Authorization Required</title></head><body><center>openresty</center></body></html>")
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	a := &auth.Auth{AccessToken: "tok", RefreshToken: "ref", UID: "uid-test", Domain: "www.codebuddy.cn"}

	_, _, _, _, _, err := c.UserResourceDetailedWithExpiry(a, time.Hour)
	if err == nil {
		t.Fatal("401 时不该返回 nil error")
	}
	if !IsSessionDead(err) {
		t.Fatalf("401 HTML 应判为会话失效，实际 err=%v", err)
	}
}

// 对照组：402（余额耗尽）不能被误判成会话失效，否则会把还能用的号禁掉。
func TestNotSessionDeadOn402(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		io.WriteString(w, `{"code":14018,"msg":"Credits exhausted"}`)
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	a := &auth.Auth{AccessToken: "tok", RefreshToken: "ref", UID: "uid-test", Domain: "www.codebuddy.cn"}

	_, _, _, _, _, err := c.UserResourceDetailedWithExpiry(a, time.Hour)
	if err == nil {
		t.Fatal("402 时不该返回 nil error")
	}
	if IsSessionDead(err) {
		t.Fatalf("402 余额耗尽不该判为会话失效，实际 err=%v", err)
	}
}
