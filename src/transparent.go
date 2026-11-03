package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	soOriginalDst     = 80
	ip6tSoOriginalDst = 80
	solIPv6           = 41
)

// getOriginalDst 从 iptables REDIRECT 过的连接里取回真实目标（IP:port）。
func getOriginalDst(conn *net.TCPConn) (string, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return "", err
	}

	var dst4, dst6 string
	var e4, e6 error

	cerr := raw.Control(func(fd uintptr) {
		var a4 [128]byte
		l4 := uint32(len(a4))
		_, _, e := syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd,
			syscall.SOL_IP, soOriginalDst,
			uintptr(unsafe.Pointer(&a4[0])),
			uintptr(unsafe.Pointer(&l4)), 0)
		if e != 0 {
			e4 = e
		} else {
			port := int(a4[2])<<8 | int(a4[3])
			ip := net.IPv4(a4[4], a4[5], a4[6], a4[7])
			dst4 = net.JoinHostPort(ip.String(), strconv.Itoa(port))
		}

		var a6 [128]byte
		l6 := uint32(len(a6))
		_, _, e = syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd,
			solIPv6, ip6tSoOriginalDst,
			uintptr(unsafe.Pointer(&a6[0])),
			uintptr(unsafe.Pointer(&l6)), 0)
		if e != 0 {
			e6 = e
		} else {
			port := int(a6[2])<<8 | int(a6[3])
			ip := net.IP(a6[8:24])
			dst6 = net.JoinHostPort(ip.String(), strconv.Itoa(port))
		}
	})
	if cerr != nil {
		return "", cerr
	}

	if dst4 != "" && !strings.HasPrefix(dst4, "0.0.0.0:") {
		return dst4, nil
	}
	if dst6 != "" && !strings.HasPrefix(dst6, "[::]:") {
		return dst6, nil
	}
	return "", fmt.Errorf("no usable dst (v4=%q/%v v6=%q/%v)", dst4, e4, dst6, e6)
}

// peekClientHelloSNI 从 TLS ClientHello 里读出 SNI，不消费数据。
func peekClientHelloSNI(r *bufio.Reader) (string, error) {
	hdr, err := r.Peek(5)
	if err != nil {
		return "", err
	}
	if hdr[0] != 0x16 { // handshake
		return "", fmt.Errorf("not tls handshake")
	}
	recLen := int(hdr[3])<<8 | int(hdr[4])
	if recLen <= 0 || recLen > 16384 {
		return "", fmt.Errorf("bad record len %d", recLen)
	}

	full, err := r.Peek(5 + recLen)
	if err != nil {
		return "", err
	}
	b := full[5:]
	if len(b) < 4 || b[0] != 0x01 { // ClientHello
		return "", fmt.Errorf("not client hello")
	}

	p := 4 + 2 + 32 // type(1)+len(3)+version(2)+random(32)
	if len(b) < p+1 {
		return "", fmt.Errorf("short session id")
	}
	p += 1 + int(b[p])

	if len(b) < p+2 {
		return "", fmt.Errorf("short cipher suites")
	}
	p += 2 + (int(b[p])<<8 | int(b[p+1]))

	if len(b) < p+1 {
		return "", fmt.Errorf("short compression")
	}
	p += 1 + int(b[p])

	if len(b) < p+2 {
		return "", fmt.Errorf("no extensions")
	}
	extLen := int(b[p])<<8 | int(b[p+1])
	p += 2
	end := p + extLen
	if end > len(b) {
		end = len(b)
	}

	for p+4 <= end {
		eType := int(b[p])<<8 | int(b[p+1])
		eLen := int(b[p+2])<<8 | int(b[p+3])
		p += 4
		if p+eLen > end {
			break
		}
		if eType == 0x0000 { // server_name
			if eLen >= 5 {
				nLen := int(b[p+3])<<8 | int(b[p+4])
				if p+5+nLen <= end {
					return string(b[p+5 : p+5+nLen]), nil
				}
			}
		}
		p += eLen
	}
	return "", fmt.Errorf("sni not found")
}
