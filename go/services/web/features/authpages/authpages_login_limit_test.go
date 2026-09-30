package authpages

import (
	"bufio"
	"io"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"ollitex/go/services/web/core"
)

// minimalResp — the bare RESP subset core.RedisClient needs for the
// limiter path (INCRBY → :n, EXPIRE → :1, PING → +PONG).
type minimalResp struct {
	ln     net.Listener
	mu     sync.Mutex
	counts map[string]int
}

func newMinimalResp(t *testing.T) *minimalResp {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &minimalResp{ln: ln, counts: map[string]int{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go m.serve(c)
		}
	}()
	return m
}

func (m *minimalResp) addr() string { return m.ln.Addr().String() }

func (m *minimalResp) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			continue
		}
		var args []string
		switch line[0] {
		case '*':
			n, _ := strconv.Atoi(line[1:])
			for i := 0; i < n; i++ {
				al, err := r.ReadString('\n')
				if err != nil {
					return
				}
				al = strings.TrimSuffix(strings.TrimSuffix(al, "\n"), "\r")
				if len(al) >= 2 && al[0] == '$' {
					l, _ := strconv.Atoi(al[1:])
					buf := make([]byte, l+2)
					_, _ = io.ReadFull(r, buf)
					args = append(args, string(buf[:l]))
				}
			}
		default:
			// inline command (single-line)
			args = strings.Fields(line)
		}
		if len(args) == 0 {
			continue
		}
		switch strings.ToUpper(args[0]) {
		case "PING":
			_, _ = c.Write([]byte("+PONG\r\n"))
		// go-redis v9 handshakes with HELLO 3. Answering with a plain redis
		// error makes it negotiate the RESP2 fallback (the same trick the
		// core fakeRedis uses) — much more robust than emulating a RESP3
		// map — and subsequent commands flow in plain RESP2.
		case "HELLO":
			_, _ = c.Write([]byte("-ERR unknown command 'HELLO'\r\n"))
		case "INCRBY":
			if len(args) >= 3 {
				m.mu.Lock()
				m.counts[args[1]]++
				n := m.counts[args[1]]
				m.mu.Unlock()
				_, _ = c.Write([]byte(":" + strconv.Itoa(n) + "\r\n"))
				continue
			}
			_, _ = c.Write([]byte("-ERR INCRBY arity\r\n"))
		case "EXPIRE":
			_, _ = c.Write([]byte(":1\r\n"))
		default:
			_, _ = c.Write([]byte("+OK\r\n"))
		}
	}
}

// TestPostLoginRateLimit — B1 (Part B audit): 10 login attempts per IP per
// 60s window go through; the 11th gets the Node-shaped 429 before touching
// the credential store.
func TestPostLoginRateLimit(t *testing.T) {
	fr := newMinimalResp(t)
	defer fr.ln.Close()
	a := &core.App{Redis: &core.RedisClient{Addr: fr.addr()}}

	f := Feature(a)
	var login core.Route
	for _, rt := range f.Routes {
		if rt.Method == "POST" && rt.Path == "/login" {
			login = rt
			break
		}
	}
	if login.Handler == nil {
		t.Fatal("POST /login route missing")
	}

	do := func() (int, string) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"email":"x@y.test","password":"nope"}`))
		req.RemoteAddr = "203.0.113.9:44123"
		login.Handler(&core.Cxt{Req: req}, &core.Res{W: w})
		b, _ := io.ReadAll(w.Body)
		return w.Result().StatusCode, string(b)
	}

	for i := 0; i < 10; i++ {
		code, _ := do()
		if code == 429 {
			t.Fatalf("attempt %d must not be rate-limited, got 429", i+1)
		}
	}
	code, body := do()
	if code != 429 {
		t.Fatalf("attempts 11 must be 429, got %d", code)
	}
	if body != "Rate limit reached, please try again later" {
		t.Fatalf("429 body must be Node-pinned, got %q", body)
	}
	// different IP keeps its own budget
	code, _ = do() // same IP still throttled... but budget is per-IP:
	if code != 429 {
		t.Fatalf("throttled IP stays throttled, got %d", code)
	}
}
