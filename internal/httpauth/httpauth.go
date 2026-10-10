// Package httpauth 网关与面板共用的 Bearer 鉴权原语。
//
// 单独成包的原因：server（/v1/*、/status）与 panel（/panel/api/*）两处鉴权
// 必须完全同口径——此前各自复制了一份"字符串直接比较"的实现，既容易漂移，
// 又都带计时侧信道。统一到这里后，口径只有一份，且天然常量时间比较。
package httpauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerPrefix 认证方案前缀（大小写敏感，与 HTTP 规范及既有实现一致）。
const bearerPrefix = "Bearer "

// VerifyBearer 校验请求头是否携带正确的 Bearer 密钥。
//
// key 为空表示"未启用鉴权"，恒返回 true（调用方据此放行）。
// 比较用 SHA-256 摘要 + subtle.ConstantTimeCompare：
//   - 常量时间，不因前缀匹配长度而泄露信息；
//   - 先摘要再比较，长度差异被吸收进摘要（不会因长度不同提前返回）；
//   - 摘要本身不可逆，即便有侧信道也拿不到密钥原文。
func VerifyBearer(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, bearerPrefix) {
		// 缺头/方案不对：仍走一次摘要比较，保持耗时形状一致。
		subtle.ConstantTimeCompare(digest(""), digest(key))
		return false
	}
	tok := authz[len(bearerPrefix):]
	return subtle.ConstantTimeCompare(digest(tok), digest(key)) == 1
}

// VerifyBearerAny 校验 Authorization 是否匹配 keys 中【任意一把】。
//
// keys 为空或全是空串时视为未启用鉴权，恒返回 true —— 与 VerifyBearer(r, "") 同语义，
// 调用方可据此放行。空串元素会被忽略：面板签发的密钥不允许为空。
//
// 与 VerifyBearer 一样先摘要再常量时间比较；区别是对【每一把都执行比较、不提前返回】，
// 因此耗时与命中第几把无关，不因位置泄露信息。密钥把数是个位数，代价可忽略。
func VerifyBearerAny(r *http.Request, keys []string) bool {
	valid := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "" {
			valid = append(valid, k)
		}
	}
	if len(valid) == 0 {
		return true
	}
	authz := r.Header.Get("Authorization")
	tok := ""
	if strings.HasPrefix(authz, bearerPrefix) {
		tok = authz[len(bearerPrefix):]
	}
	dt := digest(tok)
	ok := 0
	for _, k := range valid {
		// 逐把比较且不 break：命中位置不影响耗时。
		ok |= subtle.ConstantTimeCompare(dt, digest(k))
	}
	return ok == 1
}

// digest 返回 s 的 SHA-256（定长 32 字节，供常量时间比较）。
func digest(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
