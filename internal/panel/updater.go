// updater.go 面板「版本更新」页签的后端：把宿主机更新服务的能力透出给前端。
//
// 为什么不在面板进程里直接更新：panel 跑在容器里（uid 10001，无 git、无 docker CLI、
// 无 docker.sock），而更新 = 拉源码 + 重建镜像 + 重启容器，只能在宿主机执行。
// 所以这里只做一个**受限的 HTTP 客户端**：目标地址与令牌都来自 config.json 的
// updater 段，服务端固定，不接受前端传入 URL —— 避免变成 SSRF/任意调用入口。
package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// updaterCfg 当前生效的更新服务参数：livecfg 快照优先（面板改完即生效），
// 回落静态字段（测试或未装配 livecfg 的场景）。
func (p *Panel) updaterCfg() UpdaterConfig {
	if p.cfg.Live != nil {
		s := p.cfg.Live.Load()
		return UpdaterConfig{
			Enabled: s.UpdaterEnabled,
			URL:     s.UpdaterURL,
			Token:   s.UpdaterToken,
			Timeout: s.UpdaterTimeout,
		}
	}
	return p.cfg.Updater
}

// updaterBaseURL 归一化配置里的地址：去掉尾部斜杠；空 = 未配置。
func (p *Panel) updaterBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(p.updaterCfg().URL), "/")
}

// errUpdaterUnset 未配置宿主机更新服务。
var errUpdaterUnset = fmt.Errorf("未配置更新服务（config.json 的 updater.url）")

// updaterTimeout 单次调用超时：check 要跑一次 git fetch，慢网络下可能十几秒，
// 默认 60s；配置里可用 Go duration 字符串（"30s"/"2m"）覆盖，非法值回落默认。
func (p *Panel) updaterTimeout() time.Duration {
	if s := strings.TrimSpace(p.updaterCfg().Timeout); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return 60 * time.Second
}

// updaterCall 向宿主机更新服务发一次请求，把上游 JSON 原样带回。
//
// 三类失败分开报：未配置（errUpdaterUnset）、连不上（带 base 的错误）、
// 上游返回非 200（状态码与 body 一起带回）。前端据此区分"没装更新服务"
// 和"更新服务挂了"，不必自己猜。
func (p *Panel) updaterCall(method, path string) (int, map[string]any, error) {
	base := p.updaterBaseURL()
	if base == "" {
		return 0, nil, errUpdaterUnset
	}
	req, err := http.NewRequest(method, base+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if tok := strings.TrimSpace(p.updaterCfg().Token); tok != "" {
		req.Header.Set("X-WB-Token", tok)
	}
	cl := &http.Client{Timeout: p.updaterTimeout()}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("连接更新服务失败（%s）：%w", base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		out["raw"] = string(body)
	}
	return resp.StatusCode, out, nil
}

// updaterStatus GET /panel/api/updater/status
//
// 合并两块信息：宿主机服务可达性 + 上次任务结果。服务不可达时仍返回 200，
// 把 reachable=false 与具体原因带给前端 —— "更新服务没装"是正常状态，不是接口错误。
func (p *Panel) updaterStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"ok":        true,
		"enabled":   p.updaterCfg().Enabled,
		"url":       p.updaterBaseURL(),
		"version":   p.cfg.Version,
		"timeout":   p.updaterTimeout().String(),
		"reachable": false,
	}
	if !p.updaterCfg().Enabled {
		out["error"] = "更新功能已关闭（config.json 的 updater.enabled = false）"
		writeJSON(w, http.StatusOK, out)
		return
	}
	code, body, err := p.updaterCall(http.MethodGet, "/status")
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if code != http.StatusOK {
		out["error"] = fmt.Sprintf("更新服务返回 HTTP %d", code)
		out["upstream"] = body
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["reachable"] = true
	out["status"] = body
	writeJSON(w, http.StatusOK, out)
}

// updaterCheck POST /panel/api/updater/check —— 让宿主机拉一次远端并比对。
func (p *Panel) updaterCheck(w http.ResponseWriter, r *http.Request) {
	code, body, err := p.updaterCall(http.MethodGet, "/check")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, code, body)
}

// updaterRun POST /panel/api/updater/run —— 启动更新任务（宿主机侧异步执行）。
//
// 两个可选参数，都只是转发给宿主机更新服务，最终闸门在那边：
//   - force=1   允许丢弃宿主机工作区的未提交改动（服务端还要 allow_force=true）；
//   - rebuild=1 源码已是远端版本时也重建镜像并重启（用于镜像与源码不同步）。
func (p *Panel) updaterRun(w http.ResponseWriter, r *http.Request) {
	path := "/update"
	q := r.URL.Query()
	args := make([]string, 0, 2)
	if v := strings.TrimSpace(q.Get("force")); v == "1" || v == "true" {
		args = append(args, "force=1")
	}
	if v := strings.TrimSpace(q.Get("rebuild")); v == "1" || v == "true" {
		args = append(args, "rebuild=1")
	}
	if len(args) > 0 {
		path += "?" + strings.Join(args, "&")
	}
	code, body, err := p.updaterCall(http.MethodPost, path)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, code, body)
}

// updaterLog GET /panel/api/updater/log?n=200 —— 任务日志尾部。
//
// 日志存在宿主机侧，所以容器重启（更新完必然发生）之后仍读得到上一次的记录。
func (p *Panel) updaterLog(w http.ResponseWriter, r *http.Request) {
	n := 200
	if q := strings.TrimSpace(r.URL.Query().Get("n")); q != "" {
		if v, err := strconv.Atoi(q); err == nil {
			n = v
		}
	}
	if n <= 0 || n > 2000 {
		n = 200
	}
	code, body, err := p.updaterCall(http.MethodGet, fmt.Sprintf("/log?n=%d", n))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, code, body)
}
