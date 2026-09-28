// Package redisx is a minimal pure-stdlib Redis client (RESP protocol over TCP)
// implementing exactly the ops project-history uses: GET/SET/DEL/LPUSH/LRANGE/
// LLEN/EXISTS/EXPIRE/PING. Test-driven via the Client interface.
package redisx

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Client interface — minimal set used by project-history.
type Client interface {
	Get(k string) (string, bool, error)
	Set(k, v string, ttlSeconds ...int) error
	SetNX(k, v string) (bool, error) // SET k v NX (set if absent).
	// SetNXWithTTL: SET k v EX ttl NX → true if the key is new.
	SetNXWithTTL(k, v string, ttlSeconds int) (bool, error)
	Del(keys ...string) (int64, error)  // DEL key [key ...] — multi-arg.
	Exists(keys ...string) (int, error) // EXISTS multi-arg → count.
	Expire(k string, ttlSeconds int) error
	LRange(k string, start, stop int) ([]string, error)
	LRem(k string, n int, value string) (int64, error)
	LLen(k string) (int64, error)
	// Scan iterates matching keys; limit caps results (0 = unlimited).
	Scan(pattern string, limit int) ([]string, error)
	MGet(keys ...string) ([]string, error)
	Ping() error
	Close() error
}

type Dialer struct {
	Host     string
	Port     int
	Password string
}

func (d Dialer) Open() (Client, error) {
	conn, err := net.Dial("tcp", d.Host+":"+strconv.Itoa(d.Port))
	if err != nil {
		return nil, err
	}
	c := &client{conn: conn, rd: bufio.NewReader(conn)}
	if d.Password != "" {
		if _, err := c.exec("AUTH", d.Password); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return c, nil
}

type client struct {
	conn net.Conn
	rd   *bufio.Reader
	mu   sync.Mutex
}

func (c *client) Close() error { return c.conn.Close() }

func (c *client) exec(cmd string, args ...string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.execLocked(cmd, args...)
}

// execLocked writes a RESP command request and reads the reply.
func (c *client) execLocked(cmd string, args ...string) (any, error) {
	all := append([]string{cmd}, args...)
	var buf []byte
	buf = append(buf, '*')
	buf = append(buf, []byte(strconv.Itoa(len(all)))...)
	buf = append(buf, '\r', '\n')
	for _, a := range all {
		buf = append(buf, '$')
		buf = append(buf, []byte(strconv.Itoa(len(a)))...)
		buf = append(buf, '\r', '\n') // RESP: $<len>\r\n<data>\r\n (the CRLF after the
		// length is mandatory — without it the server rejects the request and
		// drops the connection).
		buf = append(buf, a...)
		buf = append(buf, '\r', '\n')
	}
	if _, err := c.conn.Write(buf); err != nil {
		return nil, err
	}
	return c.readReply()
}

func (c *client) readReply() (any, error) {
	line, err := c.rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, fmt.Errorf("empty redis line")
	}
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, fmt.Errorf("redis error: %s", line[1:])
	case ':':
		n, _ := strconv.ParseInt(line[1:], 10, 64)
		return n, nil
	case '$':
		n, _ := strconv.Atoi(line[1:])
		if n == -1 {
			return nil, nil
		}
		b := make([]byte, n+2)
		if _, err := io.ReadFull(c.rd, b); err != nil {
			return nil, err
		}
		return string(b[:n]), nil
	case '*':
		n, _ := strconv.Atoi(line[1:])
		if n == -1 {
			return []string{}, nil
		}
		arr := make([]string, n)
		for i := 0; i < n; i++ {
			item, err := c.readReply()
			if err != nil {
				return nil, err
			}
			arr[i], _ = item.(string)
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected redis reply prefix %q", line[0])
	}
}

func (c *client) Get(k string) (string, bool, error) {
	v, err := c.exec("GET", k)
	if err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	s, _ := v.(string)
	return s, true, nil
}

func (c *client) Set(k, v string, ttlSeconds ...int) error {
	cmd := "SET"
	args := []string{k, v}
	if len(ttlSeconds) > 0 {
		args = append(args, "EX", strconv.Itoa(ttlSeconds[0]))
	}
	_, err := c.exec(cmd, args...)
	return err
}

// SetNXWithTTL implements SET k v EX ttl NX → true if the key is new, else
// false. This is the lock-acquire primitive: set a unique value with a
// 360s expiry, atomically, if the key does not already exist.
func (c *client) SetNXWithTTL(key, value string, ttlSeconds int) (bool, error) {
	reply, err := c.exec("SET", key, value, "EX", strconv.Itoa(ttlSeconds), "NX")
	if err != nil {
		return false, err
	}
	s, ok := reply.(string)
	return ok && strings.EqualFold(s, "OK"), nil
}

// SetNX implements SET k v NX → true if the key is new, false if already present.
func (c *client) SetNX(key, val string) (bool, error) {
	reply, err := c.exec("SET", key, val, "NX")
	if err != nil {
		return false, err
	}
	s, ok := reply.(string)
	return ok && strings.EqualFold(s, "OK"), nil
}

func (c *client) Del(keys ...string) (int64, error) {
	args := make([]string, len(keys))
	copy(args, keys)
	v, err := c.exec("DEL", args...)
	if err != nil {
		return 0, err
	}
	n, _ := v.(int64)
	return n, nil
}

func (c *client) Exists(keys ...string) (int, error) {
	args := make([]string, len(keys))
	copy(args, keys)
	v, err := c.exec("EXISTS", args...)
	if err != nil {
		return 0, err
	}
	n, _ := v.(int64)
	return int(n), nil
}

func (c *client) Expire(k string, ttl int) error {
	_, err := c.exec("EXPIRE", k, strconv.Itoa(ttl))
	return err
}

func (c *client) LPush(k string, vals ...string) (int64, error) {
	args := append([]string{k}, vals...)
	v, err := c.exec("LPUSH", args...)
	if err != nil {
		return 0, err
	}
	n, _ := v.(int64)
	return n, nil
}

func (c *client) LRange(k string, start, stop int) ([]string, error) {
	v, err := c.exec("LRANGE", k, strconv.Itoa(start), strconv.Itoa(stop))
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return v.([]string), nil
}

func (c *client) LRem(k string, count int, value string) (int64, error) {
	reply, err := c.exec("LREM", k, strconv.Itoa(count), value)
	if err != nil {
		return 0, err
	}
	n, _ := reply.(int64)
	return n, nil
}

func (c *client) MGet(keys ...string) ([]string, error) {
	args := make([]string, len(keys))
	copy(args, keys)
	v, err := c.exec("MGET", args...)
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

// Scan iterates with SCAN MATCH pattern COUNT batch, collecting matches until
// the cursor wraps or limit is reached (limit<=0 unlimited).
func (c *client) Scan(pattern string, limit int) ([]string, error) {
	seen := map[string]bool{}
	cursor := "0"
	batch := 1000
	for {
		v, err := c.exec("SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(batch))
		if err != nil {
			return nil, err
		}
		arr, _ := v.([]string)
		if len(arr) < 2 {
			return nil, fmt.Errorf("short SCAN reply")
		}
		cursor = arr[0]
		for _, k := range arr[1:] {
			if !seen[k] {
				seen[k] = true
			}
		}
		if cursor == "0" {
			break
		}
		if limit > 0 && len(seen) >= limit {
			break
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (c *client) LLen(k string) (int64, error) {
	v, err := c.exec("LLEN", k)
	if err != nil {
		return 0, err
	}
	n, _ := v.(int64)
	return n, nil
}

func (c *client) Ping() error {
	_, err := c.exec("PING")
	return err
}
