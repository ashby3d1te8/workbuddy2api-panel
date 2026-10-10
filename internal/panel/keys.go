// keys.go 面板「密钥」页：签发 / 停用 / 吊销可分发的调用密钥。
//
// 分工与 config.go 相同：cmd/server 持有 Config 类型与校验落盘逻辑，此处只做 HTTP
// 编排 —— 读出当前 keys、改一处、把 {"keys": [...]} 交给注入的 SaveConfig 闭包。
// saveConfig 是【合并】语义（把提交的键叠加到磁盘上的完整配置），所以这里可以只
// 提交 keys 一个字段，不必回传整份配置，也不会把用户手写的未知键洗掉。
//
// 权限边界（重要）：这些接口挂在 withAuth 下，用的仍是面板主密钥 api_key。
// 签发出来的密钥【只对 /v1 生效】，不能访问本页 —— 否则拿到分发密钥的人就能
// 停用/删除账号。
package panel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiKeyEntry 与 cmd/server 的 APIKeyEntry 同形（此处不 import main：那会造成
// cmd/server → internal/panel 的反向依赖）。JSON 字段名必须保持一致。
type apiKeyEntry struct {
	Name      string `json:"name"`
	Secret    string `json:"secret"`
	Enabled   bool   `json:"enabled"`
	Note      string `json:"note,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

const (
	keyPrefixMax = 64  // 名称长度上限
	keyNoteMax   = 200 // 备注长度上限
	keyCountMax  = 200 // 密钥把数上限（防手滑一次签发几百把）
)

// loadKeys 读出当前配置里的 keys 数组。
//
// LoadConfig 返回的是 cmd/server 的 *Config，这里用 JSON 往返取字段，避免依赖它。
func (p *Panel) loadKeys() ([]apiKeyEntry, error) {
	cfg, err := p.cfg.LoadConfig()
	if err != nil {
		return nil, err
	}
	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Keys []apiKeyEntry `json:"keys"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		return nil, err
	}
	return doc.Keys, nil
}

// saveKeys 只提交 keys 一个字段，由 SaveConfig 合并进完整配置并热生效。
//
// 返回值里的 restartRequired 被【有意丢弃】：saveConfig 是合并语义，只提交 keys 也会
// 把它跟磁盘上的完整配置比对一遍，于是把 listen/auth_dir 这类本来就没改的字段一并报成
// 「需重启」。密钥本身经 livecfg 快照热生效（实测：签发后立刻可用，无需重启），
// 把这串噪音透给前端只会让人以为要重启。
func (p *Panel) saveKeys(keys []apiKeyEntry) error {
	raw, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		return err
	}
	_, err = p.cfg.SaveConfig(raw)
	return err
}

// newSecret 生成一把新密钥：sk- + 32 位十六进制（16 字节 CSPRNG）。
func newSecret() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(buf), nil
}

// maskSecret 列表页不回显完整密钥，只给前 8 位 + 长度。
// 完整值只在创建那一次返回（见 keysCreate）。
func maskSecret(s string) string {
	if len(s) <= 10 {
		return "****"
	}
	return s[:8] + "…" + s[len(s)-4:]
}

func (p *Panel) keysReady(w http.ResponseWriter) bool {
	if p.cfg.LoadConfig == nil || p.cfg.SaveConfig == nil {
		writeErr(w, http.StatusNotImplemented, "config api not available")
		return false
	}
	return true
}

// keysList 返回密钥列表。默认【不回显】完整密钥，只给掩码；
// 加 ?reveal=1 才给全量（面板本身已由 api_key 把关，这是给「要复制发给别人」用的）。
func (p *Panel) keysList(w http.ResponseWriter, r *http.Request) {
	if !p.keysReady(w) {
		return
	}
	keys, err := p.loadKeys()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load keys: "+err.Error())
		return
	}
	reveal := r.URL.Query().Get("reveal") == "1"
	out := make([]apiKeyEntry, 0, len(keys))
	for _, k := range keys {
		if !reveal {
			k.Secret = maskSecret(k.Secret)
		}
		out = append(out, k)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"keys":      out,
		"revealed":  reveal,
		"path":      p.cfg.ConfigPath,
		"max_count": keyCountMax,
	})
}

// keysCreate 签发一把新密钥。**完整密钥只在这里返回一次**，之后列表只给掩码。
func (p *Panel) keysCreate(w http.ResponseWriter, r *http.Request) {
	if !p.keysReady(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "parse body: "+err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Note = strings.TrimSpace(req.Note)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	if len([]rune(req.Name)) > keyPrefixMax {
		writeErr(w, http.StatusBadRequest, "name too long")
		return
	}
	if len([]rune(req.Note)) > keyNoteMax {
		writeErr(w, http.StatusBadRequest, "note too long")
		return
	}

	keys, err := p.loadKeys()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load keys: "+err.Error())
		return
	}
	if len(keys) >= keyCountMax {
		writeErr(w, http.StatusBadRequest, "key count limit reached")
		return
	}
	for _, k := range keys {
		if k.Name == req.Name {
			writeErr(w, http.StatusConflict, "name already exists")
			return
		}
	}

	secret, err := newSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gen secret: "+err.Error())
		return
	}
	entry := apiKeyEntry{
		Name:      req.Name,
		Secret:    secret,
		Enabled:   true,
		Note:      req.Note,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	keys = append(keys, entry)
	if err := p.saveKeys(keys); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// 显式回传完整密钥：列表接口默认掩码，这是唯一一次能看到全量的地方。
	// 不回传 restart_required —— 密钥热生效，返回它只会误导。
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"key":    entry,
		"secret": secret,
		"hot":    true,
	})
}

// keysToggle 停用/启用一把密钥（按 name 定位）。停用立即生效，无需重启。
func (p *Panel) keysToggle(w http.ResponseWriter, r *http.Request) {
	if !p.keysReady(w) {
		return
	}
	name, ok := p.readKeyName(w, r)
	if !ok {
		return
	}
	keys, err := p.loadKeys()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load keys: "+err.Error())
		return
	}
	found := false
	nowEnabled := false
	for i := range keys {
		if keys[i].Name == name {
			keys[i].Enabled = !keys[i].Enabled
			nowEnabled = keys[i].Enabled
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, "no such key")
		return
	}
	if err := p.saveKeys(keys); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "name": name, "enabled": nowEnabled, "hot": true,
	})
}

// keysRemove 永久删除一把密钥（按 name 定位）。
func (p *Panel) keysRemove(w http.ResponseWriter, r *http.Request) {
	if !p.keysReady(w) {
		return
	}
	name, ok := p.readKeyName(w, r)
	if !ok {
		return
	}
	keys, err := p.loadKeys()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load keys: "+err.Error())
		return
	}
	out := make([]apiKeyEntry, 0, len(keys))
	found := false
	for _, k := range keys {
		if k.Name == name {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		writeErr(w, http.StatusNotFound, "no such key")
		return
	}
	if err := p.saveKeys(out); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "name": name, "hot": true,
	})
}

// readKeyName 从请求体取 name（POST JSON {"name": "..."}）。
func (p *Panel) readKeyName(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return "", false
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "parse body: "+err.Error())
		return "", false
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return "", false
	}
	return name, true
}
