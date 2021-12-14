package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"io"
	"log"
	rand2 "math/rand"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

type contextKey struct { key string }
var ContextKeySession = &contextKey{"session"}

func init() {
	rand2.Seed(time.Now().UnixNano())
}

var bindFlag = flag.String("bind", "0.0.0.0:8022", "host and port to bind on")
var hostname = flag.String("hostname", "localhost", "host to present to clients")

func main() {
	flag.Parse()

	_, privkey, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromKey(privkey)

	handler := &Handler{}

	server := ssh.Server{
		Addr: *bindFlag,
		Handler: func(s ssh.Session) {
			io.WriteString(s, "Hi!\n")

			s.Context().(ssh.Context).SetValue(ContextKeySession, s)

			buf := make([]byte, 1)
			for {
				_, err := s.Read(buf)
				if err != nil {
					log.Println(err)
					break
				}
				if buf[0] == 3 || buf[0] == 4 {
					break
				}
			}
			s.Close()
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        handler.Forward,
			"cancel-tcpip-forward": handler.Cancel,
		},
		HostSigners: []ssh.Signer{signer},
	}

	log.Println("Listening on", *bindFlag)
	log.Fatal(server.ListenAndServe())
}
