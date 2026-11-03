package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// newHTTPClient 构造 API 客户端。
// Go 在 Android 上取不到完整的系统根证书池，这里跳过校验；
// 流量只在本机与固定 API 之间，风险面可控。
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			ForceAttemptHTTP2: true,
		},
	}
}

const (
	baseDir    = "/data/adb/band-ai"
	configPath = baseDir + "/config.json"
	caCertPath = baseDir + "/ca.crt"
	caKeyPath  = baseDir + "/ca.key"
)

// trimReply 压成单行并截断到手环能显示的长度。
func trimReply(s string, cfg *Config) string {
	reply := strings.Join(strings.Fields(s), " ")
	if n := cfg.MaxReply; n > 0 && len([]rune(reply)) > n {
		reply = string([]rune(reply)[:n]) + "…"
	}
	return reply
}

// askAPI 按 api_mode 分流：openai 直连大模型，custom 走自建桥。
func askAPI(query string, cfg *Config) string {
	if cfg.APIMode == "custom" {
		return askCustom(query, cfg)
	}
	return askOpenAI(query, cfg)
}

// askOpenAI 走 OpenAI 兼容端点（DeepSeek / SiliconFlow / Ollama / vLLM 都行）。
func askOpenAI(query string, cfg *Config) string {
	if cfg.APIKey == "" {
		logf(cfg, "[!] api_key 为空，无法调用 %s", cfg.APIURL)
		return ""
	}
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": cfg.SystemPrompt},
			{"role": "user", "content": query},
		},
		"max_tokens":  160,
		"temperature": 0.7,
		"stream":      false,
	}
	// 显式关闭思考模式：不同厂商的参数名不同，全都带上，API 会忽略不认识的
	if !cfg.AllowThinking {
		payload["thinking"] = map[string]string{"type": "disabled"}
		payload["enable_thinking"] = false
		payload["reasoning_effort"] = "none"
		payload["chat_template_kwargs"] = map[string]bool{"thinking": false}
	}
	body, _ := json.Marshal(payload)
	_ = body

	cli := newHTTPClient(25 * time.Second)
	t0 := time.Now()

	// 单次请求；withThinking 控制是否携带 thinking 相关参数
	tryOnce := func(withThinking bool) (int, []byte, error) {
		p := make(map[string]any, len(payload))
		for k, v := range payload {
			p[k] = v
		}
		if !withThinking {
			delete(p, "thinking")
			delete(p, "enable_thinking")
			delete(p, "reasoning_effort")
			delete(p, "chat_template_kwargs")
		}
		b, _ := json.Marshal(p)
		rq, e := http.NewRequest(http.MethodPost, cfg.APIURL, bytes.NewReader(b))
		if e != nil {
			return 0, nil, e
		}
		rq.Header.Set("Content-Type", "application/json")
		rq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		rs, e := cli.Do(rq)
		if e != nil {
			return 0, nil, e
		}
		defer rs.Body.Close()
		bs, _ := io.ReadAll(io.LimitReader(rs.Body, 64*1024))
		return rs.StatusCode, bs, nil
	}

	code, respBytes, err := tryOnce(!cfg.AllowThinking)
	if err != nil {
		logf(cfg, "[!] api: %v", err)
		return ""
	}
	// 400 可能是 API 不认识 thinking 参数，去掉后重试一次
	if code == http.StatusBadRequest && !cfg.AllowThinking {
		logf(cfg, "[api] 400 with thinking params, retry without: %s", string(respBytes[:min(200, len(respBytes))]))
		code, respBytes, err = tryOnce(false)
		if err != nil {
			logf(cfg, "[!] api retry: %v", err)
			return ""
		}
	}
	if code != http.StatusOK {
		logf(cfg, "[!] api %d: %s", code, string(respBytes[:min(300, len(respBytes))]))
		return ""
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &out); err != nil {
		logf(cfg, "[!] api decode: %v body=%s", err, string(respBytes[:min(200, len(respBytes))]))
		return ""
	}
	if len(out.Choices) == 0 {
		logf(cfg, "[!] api no choices: %s", string(respBytes[:min(300, len(respBytes))]))
		return ""
	}

	// content 可能是字符串或数组（某些模型返回 [{"type":"text","text":"..."}]）
	var reply string
	raw := out.Choices[0].Message.Content
	if len(raw) > 0 {
		// 先试字符串
		var s string
		if json.Unmarshal(raw, &s) == nil {
			reply = s
		} else {
			// 试数组格式
			var arr []struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
				reply = arr[0].Text
			} else {
				reply = string(raw)
			}
		}
	}
	// fallback: delta（流式响应的非流式兼容）
	if reply == "" {
		reply = out.Choices[0].Delta.Content
	}
	// fallback: reasoning_content（推理模型）—— 只取最后一段作为结论
	if reply == "" {
		rc := out.Choices[0].Message.ReasoningContent
		if rc != "" {
			// 推理模型的思考链通常用换行分段，最后一段是结论
			lines := strings.Split(strings.TrimSpace(rc), "\n")
			// 从后往前找第一个非空行
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if line != "" {
					reply = line
					break
				}
			}
			if reply == "" {
				reply = rc // 兜底：整段都用
			}
		}
	}
	if reply == "" {
		logf(cfg, "[!] api empty content: %s", string(respBytes[:min(300, len(respBytes))]))
		return ""
	}
	reply = trimReply(reply, cfg)
	logf(cfg, "[api] %.1fs model=%s", time.Since(t0).Seconds(), cfg.Model)
	return reply
}

// askCustom 走自建桥：POST {"q": ...} -> {"reply": ...}
func askCustom(query string, cfg *Config) string {
	body, _ := json.Marshal(map[string]string{"q": query})
	req, err := http.NewRequest(http.MethodPost, cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")

	cli := newHTTPClient(10 * time.Second)
	resp, err := cli.Do(req)
	if err != nil {
		logf(cfg, "[!] api: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var out struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		logf(cfg, "[!] api decode: %v", err)
		return ""
	}
	return trimReply(out.Reply, cfg)
}

func main() {
	os.MkdirAll(baseDir, 0o755)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	ca, err := LoadOrCreateCA(caCertPath, caKeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ca: %v\n", err)
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen %s: %v\n", cfg.Listen, err)
		os.Exit(1)
	}

	st := NewState()
	logf(cfg, "bandai proxy listening on %s -> %s", cfg.Listen, cfg.TargetHost)
	logf(cfg, "api: %s  max_reply: %d", cfg.APIURL, cfg.MaxReply)

	// 透明代理监听（iptables REDIRECT 入口）
	if cfg.TProxyListen != "" {
		go func() {
			lnT, err := net.Listen("tcp", cfg.TProxyListen)
			if err != nil {
				logf(cfg, "[!] tproxy listen %s: %v", cfg.TProxyListen, err)
				return
			}
			logf(cfg, "tproxy listening on %s", cfg.TProxyListen)
			for {
				c, err := lnT.Accept()
				if err != nil {
					logf(cfg, "[!] tproxy accept: %v", err)
					continue
				}
				go handleTransparent(c, cfg, ca, st)
			}
		}()
	}

	// 管理界面（浏览器打开 http://127.0.0.1:8082 配置 API key / 模型等）
	startAdmin("127.0.0.1:8082", configPath)

	for {
		conn, err := ln.Accept()
		if err != nil {
			logf(cfg, "[!] accept: %v", err)
			continue
		}
		go handleConn(conn, cfg, ca, st)
	}
}
