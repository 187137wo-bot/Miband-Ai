package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

// adminConfig 是暴露给前端的配置子集（不含敏感字段的完整副本）
type adminConfig struct {
	APIURL       string `json:"api_url"`
	APIKey       string `json:"api_key"`
	APIMode      string `json:"api_mode"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	MaxReply     int    `json:"max_reply_chars"`
	TargetHost   string `json:"target_host"`
	Listen       string `json:"listen"`
	TProxyListen string `json:"tproxy_listen"`
}

var adminMu sync.Mutex

func startAdmin(listen string, cfgPath string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", adminPage)
	mux.HandleFunc("/api/config", adminAPI(cfgPath))
	mux.HandleFunc("/api/logs", adminLogs)

	srv := &http.Server{Addr: listen, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[admin] listen %s failed: %v\n", listen, err)
		}
	}()
	fmt.Printf("admin ui listening on %s\n", listen)
}

func adminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminHTML))
}

func adminAPI(cfgPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminMu.Lock()
		defer adminMu.Unlock()

		switch r.Method {
		case http.MethodGet:
			data, err := os.ReadFile(cfgPath)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)

		case http.MethodPost:
			body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
			if err != nil {
				http.Error(w, "read body: "+err.Error(), 400)
				return
			}
			// 验证 JSON 合法性
			var raw json.RawMessage
			if err := json.Unmarshal(body, &raw); err != nil {
				http.Error(w, "invalid json: "+err.Error(), 400)
				return
			}
			if err := os.WriteFile(cfgPath, body, 0644); err != nil {
				http.Error(w, "write config: "+err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))

		default:
			http.Error(w, "method not allowed", 405)
		}
	}
}

func adminLogs(w http.ResponseWriter, r *http.Request) {
	logPath := "/data/adb/band-ai/logs/bandai.log"
	data, err := os.ReadFile(logPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// 只返回最后 8KB
	if len(data) > 8192 {
		data = data[len(data)-8192:]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

const adminHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BandAI 配置</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, sans-serif; background: #1a1a2e; color: #eee; padding: 16px; }
h1 { font-size: 20px; margin-bottom: 16px; color: #e94560; }
.field { margin-bottom: 12px; }
label { display: block; font-size: 13px; color: #aaa; margin-bottom: 4px; }
input, textarea, select {
  width: 100%; padding: 10px; border: 1px solid #333; border-radius: 6px;
  background: #16213e; color: #eee; font-size: 14px; outline: none;
}
input:focus, textarea:focus { border-color: #e94560; }
textarea { min-height: 80px; resize: vertical; font-family: monospace; }
.btn {
  display: inline-block; padding: 12px 24px; border: none; border-radius: 6px;
  font-size: 15px; cursor: pointer; margin-top: 8px; margin-right: 8px;
}
.btn-save { background: #e94560; color: #fff; }
.btn-save:active { background: #c73650; }
.btn-log { background: #0f3460; color: #eee; }
.status { margin-top: 12px; font-size: 13px; min-height: 20px; }
.ok { color: #4ecca3; }
.err { color: #e94560; }
.log-box {
  margin-top: 16px; background: #0d1117; border: 1px solid #333; border-radius: 6px;
  padding: 12px; font-family: monospace; font-size: 12px; white-space: pre-wrap;
  max-height: 300px; overflow-y: auto; display: none; color: #8b949e;
}
.hint { font-size: 11px; color: #666; margin-top: 2px; }
</style>
</head>
<body>
<h1>🎧 BandAI 配置</h1>

<div class="field">
  <label>API URL</label>
  <input id="api_url" placeholder="https://api.xiaomimimo.com/v1/chat/completions">
  <div class="hint">OpenAI 兼容的完整 chat/completions 地址</div>
</div>

<div class="field">
  <label>API Key</label>
  <input id="api_key" type="password" placeholder="sk-...">
</div>

<div class="field">
  <label>模型名称</label>
  <input id="model" placeholder="mimo-v2.6-pro">
</div>

<div class="field">
  <label>API 模式</label>
  <select id="api_mode">
    <option value="openai">OpenAI 兼容</option>
    <option value="custom">自定义桥接</option>
  </select>
</div>

<div class="field">
  <label>System Prompt</label>
  <textarea id="system_prompt"></textarea>
  <div class="hint">手环屏幕小，建议简短、单行、无 Markdown</div>
</div>

<div class="field">
  <label>最大回复字数</label>
  <input id="max_reply_chars" type="number" value="60">
</div>

<button class="btn btn-save" onclick="save()">💾 保存配置</button>
<button class="btn btn-log" onclick="toggleLog()">📋 查看日志</button>
<div id="status" class="status"></div>
<div id="logbox" class="log-box"></div>

<script>
const fields = ['api_url','api_key','model','api_mode','system_prompt','max_reply_chars'];

async function load() {
  try {
    const r = await fetch('/api/config');
    const c = await r.json();
    for (const k of fields) {
      const el = document.getElementById(k);
      if (el && c[k] !== undefined) el.value = c[k];
    }
  } catch(e) { setStatus('加载失败: '+e.message, true); }
}

async function save() {
  const c = {};
  for (const k of fields) {
    const el = document.getElementById(k);
    if (el) c[k] = el.type === 'number' ? parseInt(el.value) : el.value;
  }
  // 保留其他字段
  try {
    const old = await (await fetch('/api/config')).json();
    for (const k in old) { if (!(k in c)) c[k] = old[k]; }
  } catch(e) {}

  try {
    const r = await fetch('/api/config', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(c, null, 2)
    });
    if (r.ok) setStatus('✅ 已保存！重启 bandai 后生效', false);
    else setStatus('❌ 保存失败: ' + await r.text(), true);
  } catch(e) { setStatus('❌ ' + e.message, true); }
}

async function toggleLog() {
  const box = document.getElementById('logbox');
  if (box.style.display === 'block') { box.style.display = 'none'; return; }
  try {
    const r = await fetch('/api/logs');
    box.textContent = await r.text();
    box.style.display = 'block';
    box.scrollTop = box.scrollHeight;
  } catch(e) { box.textContent = '加载失败: '+e.message; box.style.display = 'block'; }
}

function setStatus(msg, isErr) {
  const el = document.getElementById('status');
  el.textContent = msg;
  el.className = 'status ' + (isErr ? 'err' : 'ok');
}

load();
</script>
</body>
</html>`
