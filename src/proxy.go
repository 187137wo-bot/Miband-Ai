package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------- 状态 ----------------

type State struct {
	mu       sync.Mutex
	device   map[string]bool
	hijacked map[string]bool
}

func NewState() *State {
	return &State{device: map[string]bool{}, hijacked: map[string]bool{}}
}

func (s *State) MarkDevice(did string) {
	if did == "" {
		return
	}
	s.mu.Lock()
	s.device[did] = true
	s.mu.Unlock()
}

func (s *State) IsDevice(did string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.device[did]
}

func (s *State) MarkHijacked(did string) {
	if did == "" {
		return
	}
	s.mu.Lock()
	s.hijacked[did] = true
	s.mu.Unlock()
}

func (s *State) IsHijacked(did string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hijacked[did]
}

// ---------------- 日志 ----------------

var logMu sync.Mutex

func logf(cfg *Config, format string, args ...any) {
	line := fmt.Sprintf("%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	fmt.Print(line)
	if cfg.LogDir == "" {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	_ = os.MkdirAll(cfg.LogDir, 0o755)
	f, err := os.OpenFile(filepath.Join(cfg.LogDir, "bandai.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		f.WriteString(line)
		f.Close()
	}
}

// ---------------- bufferedConn ----------------
// 关键：http.ReadRequest 会把 CONNECT 之后紧跟的字节吃进 bufio。
// 后面所有读写都必须继续走这个 bufio，否则 TLS ClientHello 会丢，
// 表现就是「非目标域名全部握手失败 → App 整体没网」。

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// ---------------- 改写动作 ----------------

type Action int

const (
	ActNone Action = iota
	ActReplace
	ActDrop
)

func rewriteFrame(payload []byte, cfg *Config, st *State) (Action, []byte) {
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return ActNone, nil
	}

	header, _ := data["header"].(map[string]any)
	if header == nil {
		return ActNone, nil
	}
	ns, _ := header["namespace"].(string)
	name, _ := header["name"].(string)
	did, _ := header["dialog_id"].(string)
	pl, _ := data["payload"].(map[string]any)

	if cfg.IsDeviceNamespace(ns) || (ns == "Template" && cfg.IsDeviceTemplate(name)) {
		st.MarkDevice(did)
		return ActNone, nil
	}

	if ns == "Template" && (name == "Toast" || name == "Text") {
		q, _ := pl["query"].(string)

		if st.IsDevice(did) {
			logf(cfg, "[SKIP] device dialog q=%q", q)
			return ActNone, nil
		}
		if cfg.HasDeviceKeyword(q) {
			st.MarkDevice(did)
			logf(cfg, "[SKIP] keyword q=%q", q)
			return ActNone, nil
		}
		if q == "" {
			return ActNone, nil
		}

		reply := askAPI(q, cfg)
		if reply == "" {
			logf(cfg, "[!] empty reply for q=%q", q)
			return ActNone, nil
		}
		old, _ := pl["text"].(string)
		pl["text"] = reply
		data["payload"] = pl
		out, err := json.Marshal(data)
		if err != nil {
			return ActNone, nil
		}
		st.MarkHijacked(did)
		logf(cfg, "[REWRITE] q=%q\n          old=%q\n          new=%q", q, old, reply)
		return ActReplace, out
	}

	if st.IsHijacked(did) {
		if ns == "AudioPlayer" || (ns == "Template" && name == "PlayInfo") {
			logf(cfg, "[DROP] %s.%s", ns, name)
			return ActDrop, nil
		}
	}
	return ActNone, nil
}

// ---------------- 上游 ----------------

func dialUpstream(host string) (*tls.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, "443"),
		&tls.Config{
			ServerName: host,
			// Go 在 Android 上取不到完整的系统根证书池，这里跳过上游校验。
			// 我们只是把流量转发给真实服务器，风险面仅限本机。
			InsecureSkipVerify: true,
		})
}

// tunnel 纯字节转发。client 必须是 bufferedConn，否则会丢预读数据。
func tunnel(client net.Conn, addr string) {
	upstream, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		client.Close()
		return
	}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		upstream.Close()
		client.Close()
		return
	}
	go func() {
		io.Copy(upstream, client)
		upstream.Close()
	}()
	io.Copy(client, upstream)
	client.Close()
}

// handleMITM 只对目标域名做 TLS 中间人。client 必须是 bufferedConn。
func handleMITM(client net.Conn, host string, cfg *Config, ca *CA, st *State) {
	tlsConn := tls.Server(client, &tls.Config{
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			h := chi.ServerName
			if h == "" {
				h = host
			}
			return ca.Sign(h)
		},
	})
	defer tlsConn.Close()

	if err := tlsConn.Handshake(); err != nil {
		logf(cfg, "[!] handshake with app failed: %v", err)
		return
	}

	br := bufio.NewReader(tlsConn)
	req, err := http.ReadRequest(br)
	if err != nil {
		logf(cfg, "[!] read inner request: %v", err)
		return
	}

	upstream, err := dialUpstream(host)
	if err != nil {
		logf(cfg, "[!] upstream dial: %v", err)
		return
	}
	defer upstream.Close()

	if err := req.Write(upstream); err != nil {
		return
	}

	ur := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(ur, req)
	if err != nil {
		logf(cfg, "[!] upstream response: %v", err)
		return
	}
	if err := resp.Write(tlsConn); err != nil {
		return
	}

	isWS := strings.EqualFold(req.Header.Get("Upgrade"), "websocket") && resp.StatusCode == 101
	if !isWS {
		io.Copy(tlsConn, ur)
		return
	}

	logf(cfg, "[WS] upgraded %s", req.URL.Path)

	go func() {
		io.Copy(upstream, br)
		upstream.Close()
	}()

	for {
		f, err := ReadFrame(ur)
		if err != nil {
			return
		}
		if f.Opcode == opText {
			act, nb := rewriteFrame(f.Payload, cfg, st)
			switch act {
			case ActDrop:
				continue
			case ActReplace:
				f.Payload = nb
			}
		}
		if err := WriteFrame(tlsConn, f); err != nil {
			return
		}
	}
}

// handleConn 处理一条进来的连接。
func handleConn(client net.Conn, cfg *Config, ca *CA, st *State) {
	defer func() {
		if r := recover(); r != nil {
			logf(cfg, "[!] panic: %v", r)
		}
	}()

	br := bufio.NewReader(client)
	req, err := http.ReadRequest(br)
	if err != nil {
		logf(cfg, "[!] bad request from %s: %v", client.RemoteAddr(), err)
		client.Close()
		return
	}
	logf(cfg, "[REQ] %s %s %s", client.RemoteAddr(), req.Method, req.Host)

	if req.Method != http.MethodConnect {
		proxyPlain(client, req, cfg)
		return
	}

	host := req.Host
	if h, _, err := net.SplitHostPort(req.Host); err == nil {
		host = h
	}

	bc := &bufferedConn{Conn: client, r: br}

	if !strings.EqualFold(host, cfg.TargetHost) {
		tunnel(bc, req.Host)
		return
	}

	logf(cfg, "[MITM] %s", host)
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		client.Close()
		return
	}
	handleMITM(bc, host, cfg, ca, st)
}

// proxyPlain 处理非 CONNECT 的普通代理请求。
func proxyPlain(client net.Conn, req *http.Request, cfg *Config) {
	defer client.Close()
	if req.URL == nil || req.URL.Host == "" {
		return
	}
	upstream, err := net.DialTimeout("tcp", req.URL.Host, 10*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	req.Write(upstream)
	io.Copy(client, upstream)
}

// handleTransparent 处理被 iptables REDIRECT 过来的连接。
// 这种连接没有 CONNECT 握手，客户端上来就是 TLS，所以先从内核取回原始目标再分流。
func handleTransparent(client net.Conn, cfg *Config, ca *CA, st *State) {
	defer func() {
		if r := recover(); r != nil {
			logf(cfg, "[!] tproxy panic: %v", r)
		}
	}()

	tcpConn, ok := client.(*net.TCPConn)
	if !ok {
		client.Close()
		return
	}

	dst, err := getOriginalDst(tcpConn)
	if err != nil {
		logf(cfg, "[!] get original dst from %s: %v", client.RemoteAddr(), err)
		client.Close()
		return
	}

	br := bufio.NewReader(client)
	bc := &bufferedConn{Conn: client, r: br}

	// SO_ORIGINAL_DST 只给 IP，拿不到域名；真正要看的是 TLS ClientHello 里的 SNI。
	sni, err := peekClientHelloSNI(br)
	if err != nil {
		logf(cfg, "[TPROXY] no-sni dst=%s (%v) -> tunnel", dst, err)
		tunnelRaw(bc, dst)
		return
	}

	if !strings.EqualFold(sni, cfg.TargetHost) {
		logf(cfg, "[TPROXY] %s -> tunnel", sni)
		tunnelRaw(bc, dst)
		return
	}

	logf(cfg, "[TPROXY] MITM %s (dst=%s)", sni, dst)
	handleMITM(bc, sni, cfg, ca, st)
}

// tunnelRaw 透明转发：不发任何 HTTP 响应头，直接双向抄字节。
func tunnelRaw(client net.Conn, addr string) {
	upstream, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		client.Close()
		return
	}
	go func() {
		io.Copy(upstream, client)
		upstream.Close()
	}()
	io.Copy(client, upstream)
	client.Close()
}
