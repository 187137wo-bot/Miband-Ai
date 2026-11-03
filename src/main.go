package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
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
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	cli := newHTTPClient(25 * time.Second)
	t0 := time.Now()
	resp, err := cli.Do(req)
	if err != nil {
		logf(cfg, "[!] api: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 200)
		n, _ := resp.Body.Read(buf)
		logf(cfg, "[!] api %d: %s", resp.StatusCode, string(buf[:n]))
		return ""
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		logf(cfg, "[!] api decode: %v", err)
		return ""
	}
	if len(out.Choices) == 0 {
		return ""
	}
	reply := trimReply(out.Choices[0].Message.Content, cfg)
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
