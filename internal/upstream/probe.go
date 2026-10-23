package upstream

import (
	"errors"
	"net/http"
)

// IsSessionDead 判断一次上游调用是否明确表示「登录态已被作废」。
//
// 两类都算：
//   - Kind == ErrSessionDead：body 里带 12153（refresh 会话已死）；
//   - Status == 401：上游边缘层（openresty / APISIX）直接返回 HTML 401，body 里
//     没有 12153，Classify 认不出来，但语义完全相同——这个 token 上游不认了。
//
// 为什么单独提一个判断：accessToken 的 exp 可能还有十几天，光看过期时间判不出
// 「被顶号」。2026-10-23 实测：3 个号的 exp 都在 11 月，但所有 /v2/* 端点 401，
// refresh 返回 12153「Refresh token issued before the user session started」——
// 会话被更晚的一次登录顶掉了。
func IsSessionDead(err error) bool {
	var ue *Error
	if !errors.As(err, &ue) {
		return false
	}
	return ue.Kind == ErrSessionDead || ue.Status == http.StatusUnauthorized
}
