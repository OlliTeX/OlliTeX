package redismanager

import (
	"fmt"
	"strings"
)

// fakeTx mirrors the oracle's this.multi stub (a single recording stub
// object shared by every multi() call on the client — call history is
// cumulative, Exec replies are scripted per-Exec-order, onCall(n)).
type fakeTx struct {
	c *fakeClient

	// cumulative command recordings (typed caps, for oracle pins).
	Existses []string
	SAdds    []saddCap
	SReMs    []saddCap
	MSets    []map[string]any
	Dels     []delCap
	Sets     []setCap
	RPushs   []rpushCap
	Expires  []expireCap
	LTrims   []ltrimCap
	GetSets  []getsetCap
	ZAdds    []zaddCap
	ZRanges  []zrangeCap
	ZRems    []zremrangeCap
	StrLens  []string
	SetExs   []setexCap
	SCards   []string
}

type saddCap struct {
	key     string
	members []string
}

type delCap struct{ keys []string }

type setCap struct {
	key   string
	value any
	opts  []any
}

type rpushCap struct {
	key  string
	vals []string
}

type expireCap struct {
	key  string
	secs int
}

type ltrimCap struct {
	key   string
	start int
	stop  int
}

type getsetCap struct {
	key   string
	value string
}

type zaddCap struct {
	key    string
	score  float64
	member string
}

type zrangeCap struct {
	key   string
	start int
	stop  int
}

type zremrangeCap struct {
	key   string
	start int
	stop  int
}

type setexCap struct {
	key   string
	secs  int
	value string
}

func (t *fakeTx) Exists(key string) Tx {
	t.Existses = append(t.Existses, key)
	return t
}

func (t *fakeTx) SAdd(key string, members ...string) Tx {
	t.SAdds = append(t.SAdds, saddCap{key, members})
	return t
}

func (t *fakeTx) MSet(values map[string]any) Tx {
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	t.MSets = append(t.MSets, out)
	return t
}

func (t *fakeTx) Del(keys ...string) Tx {
	t.Dels = append(t.Dels, delCap{keys})
	return t
}

func (t *fakeTx) Set(key string, value any, opts ...any) Tx {
	optsCopy := make([]any, len(opts), len(opts)+1)
	copy(optsCopy, opts)
	t.Sets = append(t.Sets, setCap{key, value, optsCopy})
	return t
}

func (t *fakeTx) RPush(key string, values ...string) Tx {
	valsCopy := make([]string, len(values))
	copy(valsCopy, values)
	t.RPushs = append(t.RPushs, rpushCap{key, valsCopy})
	return t
}

func (t *fakeTx) Expire(key string, seconds int) Tx {
	t.Expires = append(t.Expires, expireCap{key, seconds})
	return t
}

func (t *fakeTx) LTrim(key string, start, stop int) Tx {
	t.LTrims = append(t.LTrims, ltrimCap{key, start, stop})
	return t
}

func (t *fakeTx) GetSet(key string, value string) Tx {
	t.GetSets = append(t.GetSets, getsetCap{key, value})
	return t
}

func (t *fakeTx) ZAdd(key string, score float64, member string) Tx {
	t.ZAdds = append(t.ZAdds, zaddCap{key, score, member})
	return t
}

func (t *fakeTx) ZRange(key string, start, stop int) Tx {
	t.ZRanges = append(t.ZRanges, zrangeCap{key, start, stop})
	return t
}

func (t *fakeTx) ZRemRangeByRank(key string, start, stop int) Tx {
	t.ZRems = append(t.ZRems, zremrangeCap{key, start, stop})
	return t
}

func (t *fakeTx) ZCard(key string) Tx { return t }

func (t *fakeTx) SRem(key string, members ...string) Tx {
	membersCopy := make([]string, len(members))
	copy(membersCopy, members)
	t.SReMs = append(t.SReMs, saddCap{key, membersCopy})
	return t
}

func (t *fakeTx) StrLen(key string) Tx {
	t.StrLens = append(t.StrLens, key)
	return t
}

func (t *fakeTx) SetEx(key string, seconds int, value string) Tx {
	t.SetExs = append(t.SetExs, setexCap{key, seconds, value})
	return t
}

func (t *fakeTx) SCard(key string) Tx {
	t.SCards = append(t.SCards, key)
	return t
}

// Exec returns the Nth scripted reply (onCall semantics); unscripted
// Execs return nil (the oracle's `.resolves()` default).
func (t *fakeTx) Exec() []any {
	n := t.c.execs
	if n < len(t.c.onExecReplies) {
		t.c.execs++
		return t.c.onExecReplies[n]
	}
	t.c.execs++
	return nil
}

func q(v any) string { return fmt.Sprintf(" %v", v) }

// fakeClient mirrors the oracle's this.rclient stub (direct commands
// with call-recording + per-key scripted replies + the shared MULTI).
type fakeClient struct {
	calls []string
	Tx    *fakeTx
	// scripted per-key replies; unset keys reply empty/zero per command.
	MGetReply      []any
	GetReply       map[string]string
	LLenReply      map[string]int
	LRangeReply    map[string][]string
	SIsMemberReply map[string]int
	SMembersReply  map[string][]string
	DelReply       int
	// MULTI onCall(n) replies.
	onExecReplies [][]any
	execs         int
	// error injection.
	CallErr error
}

func NewFakeClient() *fakeClient {
	c := &fakeClient{onExecReplies: [][]any{}}
	c.Tx = &fakeTx{c: c}
	return c
}

func (c *fakeClient) MGet(keys ...string) ([]any, error) {
	c.calls = append(c.calls, "mget"+q(strings.Join(keys, ",")))
	if c.CallErr != nil {
		return nil, c.CallErr
	}
	return c.MGetReply, nil
}

func (c *fakeClient) Get(key string) (string, error) {
	c.calls = append(c.calls, "get"+q(key))
	if v, ok := c.GetReply[key]; ok {
		return v, nil
	}
	return "", nil
}

func (c *fakeClient) Set(key string, value any, opts ...any) error {
	c.calls = append(c.calls, "set"+q(key)+q(fmt.Sprint(value, opts)))
	return nil
}

func (c *fakeClient) Del(key string) (int, error) {
	c.calls = append(c.calls, "del"+q(key))
	return c.DelReply, nil
}

func (c *fakeClient) SAdd(key string, members ...string) error {
	c.calls = append(c.calls, "sadd"+q(key)+q(strings.Join(members, ",")))
	return nil
}

func (c *fakeClient) SRem(key string, members ...string) error {
	c.calls = append(c.calls, "srem"+q(key)+q(strings.Join(members, ",")))
	return nil
}

func (c *fakeClient) SIsMember(key, member string) (int, error) {
	c.calls = append(c.calls, "sismember"+q(key)+q(member))
	if v, ok := c.SIsMemberReply[key]; ok {
		return v, nil
	}
	return 0, nil
}

func (c *fakeClient) SMembers(key string) ([]string, error) {
	c.calls = append(c.calls, "smembers"+q(key))
	if v, ok := c.SMembersReply[key]; ok {
		return v, nil
	}
	return nil, nil
}

func (c *fakeClient) LLen(key string) (int, error) {
	c.calls = append(c.calls, "llen"+q(key))
	if v, ok := c.LLenReply[key]; ok {
		return v, nil
	}
	return 0, nil
}

func (c *fakeClient) LRange(key string, start, stop int) ([]string, error) {
	c.calls = append(c.calls, "lrange"+q(key)+q(start)+q(stop))
	if v, ok := c.LRangeReply[key]; ok {
		return v, nil
	}
	return nil, nil
}

func (c *fakeClient) RPush(key string, values ...string) (int, error) {
	c.calls = append(c.calls, "rpush"+q(key)+q(strings.Join(values, ",")))
	return len(values), nil
}

func (c *fakeClient) ZAdd(key string, score float64, member string) error {
	c.calls = append(c.calls, "zadd"+q(key)+q(score)+q(member))
	return nil
}

func (c *fakeClient) ZRangeByScore(key string, min, max any) ([]any, error) {
	c.calls = append(c.calls, "zrangebyscore"+q(key)+q(min)+q(max))
	return nil, nil
}

func (c *fakeClient) MultiFunc() Tx {
	c.calls = append(c.calls, "multi")
	return c.Tx
}

// scriptExec scripts the Nth Exec reply (onCall semantics).
func (c *fakeClient) scriptExec(n int, reply []any) {
	for len(c.onExecReplies) <= n {
		c.onExecReplies = append(c.onExecReplies, nil)
	}
	c.onExecReplies[n] = reply
}

func (c *fakeClient) clientCalls() []string { return c.calls }
