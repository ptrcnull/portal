package main

import (
	"fmt"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"io"
	"log"
	rand2 "math/rand"
	"net"
	"strconv"
	"sync"
)

type Handler struct {
	sync.Mutex
	forwards map[string]net.Listener
}

func (h *Handler) Init() {
	h.Lock()
	if h.forwards == nil {
		h.forwards = make(map[string]net.Listener)
	}
	h.Unlock()
}

func WriteTo(ctx ssh.Context, data []byte) (int, error) {
	var s ssh.Session
	for {
		maybeSession := ctx.Value(ContextKeySession)
		if maybeSession != nil {
			s = maybeSession.(ssh.Session)
			break
		}
	}
	return s.Write(data)
}

func (h *Handler) Forward(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (ok bool, payload []byte) {
	log.Println("Forward request received!")
	h.Init()

	remote := ctx.RemoteAddr().String()
	h.Lock()
	if _, ok := h.forwards[remote]; ok {
		return false, []byte{}
	}
	h.Unlock()

	var reqPayload remoteForwardRequest
	if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
		return false, []byte{}
	}

	bindPort := int(30000 + rand2.Int31n(1000))
	log.Println("Allocated port", bindPort)
	ln, err := net.Listen("tcp", "127.0.0.1:" + strconv.Itoa(bindPort))
	if err != nil {
		return false, []byte{}
	}

	go WriteTo(ctx, []byte(fmt.Sprintf("Port %d has been exposed on %s:%d\n", reqPayload.BindPort, *hostname, bindPort)))

	h.Lock()
	h.forwards[remote] = ln
	h.Unlock()

	conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)

	go func() {
		<-ctx.Done()
		h.Lock()
		ln, ok := h.forwards[remote]
		h.Unlock()
		if ok {
			ln.Close()
		}
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				break
			}
			originAddr, originPortStr, _ := net.SplitHostPort(c.RemoteAddr().String())
			originPort, _ := strconv.Atoi(originPortStr)
			payload := gossh.Marshal(&remoteForwardChannelData{
				DestAddr:   reqPayload.BindAddr,
				DestPort:   reqPayload.BindPort,
				OriginAddr: originAddr,
				OriginPort: uint32(originPort),
			})
			go func() {
				ch, reqs, err := conn.OpenChannel("forwarded-tcpip", payload)
				if err != nil {
					// TODO: log failure to open channel
					log.Println(err)
					c.Close()
					return
				}
				go gossh.DiscardRequests(reqs)
				go func() {
					defer ch.Close()
					defer c.Close()
					io.Copy(ch, c)
				}()
				go func() {
					defer ch.Close()
					defer c.Close()
					io.Copy(c, ch)
				}()
			}()
		}
		h.Lock()
		delete(h.forwards, remote)
		h.Unlock()
	}()

	return true, gossh.Marshal(&remoteForwardSuccess{reqPayload.BindPort})
}

func (h *Handler) Cancel(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (ok bool, payload []byte) {
	log.Println("Cancel request received!")
	h.Init()

	remote := ctx.RemoteAddr().String()

	var reqPayload remoteForwardCancelRequest
	if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
		return false, []byte{}
	}

	h.Lock()
	ln, ok := h.forwards[remote]
	h.Unlock()

	if ok {
		ln.Close()
	}
	return true, nil
}

type remoteForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type remoteForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardSuccess struct {
	BindPort uint32
}

type remoteForwardCancelRequest struct {
	BindAddr string
	BindPort uint32
}

