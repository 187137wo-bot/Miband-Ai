package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var adminMu sync.Mutex

func startAdmin(listen string, cfgPath string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", adminPage)
	mux.HandleFunc("/api/config", adminAPI(cfgPath))
	mux.HandleFunc("/api/logs", adminLogs)
	mux.HandleFunc("/api/test", adminTest(cfgPath))

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
		// GET 不持锁（读文件是原子的），避免被测试连通性等长任务阻塞
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
			adminMu.Lock()
			defer adminMu.Unlock()
			body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
			if err != nil {
				http.Error(w, "read body: "+err.Error(), 400)
				return
			}
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
			w.Write([]byte(`{"ok":true,"restarting":true}`))

			// 自动重启：退出后 service.sh 的 watchdog 会在 5 秒内拉起新进程
			go func() {
				time.Sleep(500 * time.Millisecond)
				os.Exit(0)
			}()

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
	if len(data) > 8192 {
		data = data[len(data)-8192:]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

func adminTest(cfgPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 不持全局锁：测试最长 10 秒，持锁会让配置页面打不开
		var cfg Config

		// 优先用请求体里的配置（Web UI 表单当前值），没有就读磁盘文件
		if r.Method == http.MethodPost && r.ContentLength > 0 {
			body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
			if err == nil {
				json.Unmarshal(body, &cfg)
			}
		}
		// 如果请求体没提供完整配置，从磁盘补全
		if cfg.APIURL == "" {
			data, err := os.ReadFile(cfgPath)
			if err != nil {
				http.Error(w, `{"ok":false,"error":"read config: `+err.Error()+`"}`, 500)
				return
			}
			json.Unmarshal(data, &cfg)
		}

		// 自动补全 API URL（和 LoadConfig 一样的逻辑）
		if cfg.APIMode == "openai" && cfg.APIURL != "" {
			u := strings.TrimRight(cfg.APIURL, "/")
			if !strings.HasSuffix(u, "/chat/completions") {
				cfg.APIURL = u + "/chat/completions"
			}
		}
		if cfg.MaxReply <= 0 {
			cfg.MaxReply = 60
		}
		if cfg.SystemPrompt == "" {
			cfg.SystemPrompt = defaultSystemPrompt
		}

		t0 := time.Now()

		// 直接发 HTTP 请求，拿到原始错误信息
		payload := map[string]any{
			"model": cfg.Model,
			"messages": []map[string]string{
				{"role": "user", "content": "你好，请用一句话介绍你自己"},
			},
			"max_tokens":  100,
			"temperature": 0.7,
			"stream":      false,
		}
		if !cfg.AllowThinking {
			payload["thinking"] = map[string]string{"type": "disabled"}
			payload["enable_thinking"] = false
			payload["reasoning_effort"] = "none"
			payload["chat_template_kwargs"] = map[string]bool{"thinking": false}
		}
		bodyBytes, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, cfg.APIURL, bytes.NewReader(bodyBytes))
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "构造请求失败: " + err.Error(), "api_url": cfg.APIURL, "model": cfg.Model})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}

		cli := newHTTPClient(10 * time.Second)
		resp, err := cli.Do(req)
		elapsed := time.Since(t0)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "error": "网络错误: " + err.Error(),
				"elapsed": elapsed.String(), "model": cfg.Model, "api_url": cfg.APIURL,
			})
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

		if resp.StatusCode != http.StatusOK {
			json.NewEncoder(w).Encode(map[string]any{
				"ok": false,
				"error": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
				"elapsed": elapsed.String(), "model": cfg.Model, "api_url": cfg.APIURL,
			})
			return
		}

		// 解析回复
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil || len(out.Choices) == 0 {
			json.NewEncoder(w).Encode(map[string]any{
				"ok": false,
				"error": "响应解析失败: " + string(respBody),
				"elapsed": elapsed.String(), "model": cfg.Model, "api_url": cfg.APIURL,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"reply": out.Choices[0].Message.Content,
			"elapsed": elapsed.String(),
			"model": cfg.Model,
			"api_url": cfg.APIURL,
		})
	}
}

const adminHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BandAI 配置</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, sans-serif; background: #1a1a2e; color: #eee; padding: 16px; max-width: 480px; margin: 0 auto; }
h1 { font-size: 20px; margin-bottom: 16px; color: #e94560; }
h2 { font-size: 15px; margin: 20px 0 10px; color: #e94560; border-bottom: 1px solid #333; padding-bottom: 6px; }
.field { margin-bottom: 12px; }
label { display: block; font-size: 13px; color: #aaa; margin-bottom: 4px; }
input, textarea, select {
  width: 100%; padding: 10px; border: 1px solid #333; border-radius: 6px;
  background: #16213e; color: #eee; font-size: 14px; outline: none;
}
input:focus, textarea:focus { border-color: #e94560; }
textarea { min-height: 80px; resize: vertical; font-family: monospace; }
.btn {
  display: inline-block; padding: 10px 20px; border: none; border-radius: 6px;
  font-size: 14px; cursor: pointer; margin-top: 8px; margin-right: 6px;
}
.btn-save { background: #e94560; color: #fff; }
.btn-save:active { background: #c73650; }
.btn-log { background: #0f3460; color: #eee; }
.btn-sm { padding: 6px 12px; font-size: 12px; margin: 2px; }
.btn-use { background: #4ecca3; color: #1a1a2e; }
.btn-del { background: #555; color: #ccc; }
.status { margin-top: 12px; font-size: 13px; min-height: 20px; }
.ok { color: #4ecca3; }
.err { color: #e94560; }
.log-box {
  margin-top: 16px; background: #0d1117; border: 1px solid #333; border-radius: 6px;
  padding: 12px; font-family: monospace; font-size: 12px; white-space: pre-wrap;
  max-height: 300px; overflow-y: auto; display: none; color: #8b949e;
}
.hint { font-size: 11px; color: #666; margin-top: 2px; }
.history-item {
  background: #16213e; border: 1px solid #333; border-radius: 6px;
  padding: 10px; margin-bottom: 8px;
}
.history-item .name { font-size: 14px; font-weight: bold; color: #4ecca3; }
.history-item .detail { font-size: 11px; color: #888; margin-top: 4px; word-break: break-all; }
.history-item .key-preview { font-family: monospace; color: #e94560; }
.empty { color: #555; font-size: 13px; text-align: center; padding: 20px; }
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
  <input id="model" placeholder="deepseek-chat / deepseek-flash / mimo-v2.6-pro">
  <div class="hint">支持推理模型（如 deepseek-flash），请求会自动关闭思考模式，不会输出思考过程</div>
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
<button class="btn btn-log" onclick="savePreset()" style="background:#4ecca3;color:#1a1a2e">📌 存为预设</button>
<button class="btn btn-log" onclick="testAPI()" id="btn-test" style="background:#f0a500;color:#1a1a2e">🔌 测试连通性</button>
<button class="btn btn-log" onclick="toggleLog()">📋 查看日志</button>
<div id="status" class="status"></div>
<div id="test-result" style="display:none;margin-top:10px;padding:12px;background:#16213e;border:1px solid #333;border-radius:6px;font-size:13px"></div>

<h2>📜 历史记录</h2>
<div id="history-list"><div class="empty">暂无历史记录</div></div>

<div id="logbox" class="log-box"></div>

<script>
const FIELDS = ['api_url','api_key','model','api_mode','system_prompt','max_reply_chars'];
const LS_KEY = 'bandai_history';

function getHistory() {
  try { return JSON.parse(localStorage.getItem(LS_KEY)) || []; } catch(e) { return []; }
}

function saveHistory(list) {
  localStorage.setItem(LS_KEY, JSON.stringify(list));
}

function renderHistory() {
  const list = getHistory();
  const el = document.getElementById('history-list');
  if (!list.length) { el.innerHTML = '<div class="empty">暂无历史记录</div>'; return; }
  el.innerHTML = list.map((item, i) => {
    const keyPrev = item.api_key ? item.api_key.substring(0,8) + '...' + item.api_key.slice(-4) : '(空)';
    const ts = item.saved_at || '';
    return '<div class="history-item">' +
      '<div class="name">' + esc(item.label || item.model || '未命名') + '</div>' +
      '<div class="detail">' +
        '模型: ' + esc(item.model) + '<br>' +
        'URL: ' + esc(item.api_url) + '<br>' +
        'Key: <span class="key-preview">' + esc(keyPrev) + '</span><br>' +
        '保存时间: ' + esc(ts) +
      '</div>' +
      '<button class="btn btn-sm btn-use" onclick="useHistory(' + i + ')">🔄 使用</button>' +
      '<button class="btn btn-sm btn-del" onclick="delHistory(' + i + ')">🗑️ 删除</button>' +
    '</div>';
  }).join('');
}

function esc(s) {
  const d = document.createElement('div');
  d.textContent = s || '';
  return d.innerHTML;
}

function useHistory(idx) {
  const list = getHistory();
  if (!list[idx]) return;
  const item = list[idx];
  for (const k of FIELDS) {
    const el = document.getElementById(k);
    if (el && item[k] !== undefined) el.value = item[k];
  }
  setStatus('已填入「' + (item.label || item.model) + '」，点保存生效', false);
}

function delHistory(idx) {
  const list = getHistory();
  const name = list[idx]?.label || list[idx]?.model || '该记录';
  if (!confirm('删除「' + name + '」？')) return;
  list.splice(idx, 1);
  saveHistory(list);
  renderHistory();
  setStatus('已删除', false);
}

async function load() {
  try {
    const r = await fetch('/api/config');
    const c = await r.json();
    for (const k of FIELDS) {
      const el = document.getElementById(k);
      if (el && c[k] !== undefined) el.value = c[k];
    }
  } catch(e) { setStatus('加载失败: '+e.message, true); }
  renderHistory();
}

async function save() {
  const c = {};
  for (const k of FIELDS) {
    const el = document.getElementById(k);
    if (el) c[k] = el.type === 'number' ? parseInt(el.value) : el.value;
  }
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
    if (r.ok) {
      setStatus('✅ 已保存！正在自动重启，约 5 秒后生效', false);
    } else {
      setStatus('❌ 保存失败: ' + await r.text(), true);
    }
  } catch(e) { setStatus('❌ ' + e.message, true); }
}

async function savePreset() {
  const c = {};
  for (const k of FIELDS) {
    const el = document.getElementById(k);
    if (el) c[k] = el.type === 'number' ? parseInt(el.value) : el.value;
  }
  const label = prompt('给这个配置起个名字:', c.model || '新配置');
  if (label === null) return;
  const list = getHistory();
  const existIdx = list.findIndex(h => h.api_url === c.api_url && h.model === c.model && h.api_key === c.api_key);
  const entry = { ...c, label: label || c.model, saved_at: new Date().toLocaleString('zh-CN') };
  if (existIdx >= 0) {
    list[existIdx] = entry;
  } else {
    list.unshift(entry);
  }
  if (list.length > 20) list.length = 20;
  saveHistory(list);
  renderHistory();
  setStatus('📌 已存为预设「' + (label || c.model) + '」', false);
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

async function testAPI() {
  const btn = document.getElementById('btn-test');
  const box = document.getElementById('test-result');
  btn.disabled = true;
  btn.textContent = '⏳ 测试中...';
  box.style.display = 'block';
  box.innerHTML = '<span style="color:#f0a500">正在调用 API，请稍候（最多 10 秒）...</span>';
  try {
    // 把表单当前值发给后端测试，不需要先保存
    const c = {};
    for (const k of FIELDS) {
      const el = document.getElementById(k);
      if (el) c[k] = el.type === 'number' ? parseInt(el.value) : el.value;
    }
    const r = await fetch('/api/test', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(c)
    });
    const d = await r.json();
    if (d.ok) {
      box.innerHTML =
        '<span style="color:#4ecca3;font-size:16px">✅ 连通成功</span><br>' +
        '<b>模型:</b> ' + esc(d.model) + '<br>' +
        '<b>耗时:</b> ' + esc(d.elapsed) + '<br>' +
        '<b>回复:</b> ' + esc(d.reply);
    } else {
      box.innerHTML =
        '<span style="color:#e94560;font-size:16px">❌ 连通失败</span><br>' +
        '<b>模型:</b> ' + esc(d.model || '-') + '<br>' +
        '<b>URL:</b> ' + esc(d.api_url || '-') + '<br>' +
        '<b>耗时:</b> ' + esc(d.elapsed || '-') + '<br>' +
        '<b>错误:</b> ' + esc(d.error);
    }
  } catch(e) {
    box.innerHTML = '<span style="color:#e94560">❌ 请求失败: ' + esc(e.message) + '</span>';
  }
  btn.disabled = false;
  btn.textContent = '🔌 测试连通性';
}

load();
</script>
</body>
</html>`
