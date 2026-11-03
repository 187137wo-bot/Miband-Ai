package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// RFC 6455 帧
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

type Frame struct {
	FIN     bool
	Opcode  byte
	Payload []byte
}

// ReadFrame 从流中读出一帧（客户端帧带 mask，服务端帧不带）。
func ReadFrame(r *bufio.Reader) (*Frame, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	f := &Frame{
		FIN:    hdr[0]&0x80 != 0,
		Opcode: hdr[0] & 0x0F,
	}
	masked := hdr[1]&0x80 != 0
	length := int(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint64(ext[:]))
	}
	if length < 0 || length > 64<<20 {
		return nil, fmt.Errorf("frame too large: %d", length)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	f.Payload = payload
	return f, nil
}

// WriteFrame 以服务端身份写帧（不带 mask）。
func WriteFrame(w io.Writer, f *Frame) error {
	var hdr []byte
	b0 := f.Opcode & 0x0F
	if f.FIN {
		b0 |= 0x80
	}
	hdr = append(hdr, b0)

	n := len(f.Payload)
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(hdr, ext[:]...)
	}

	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if n > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}
