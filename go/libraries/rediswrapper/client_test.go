package rediswrapper

// client_test.go — 1:1 mirror of libraries/redis-wrapper/test/unit/src/test.js
// (the Node acceptance spec), the createClient section:
//
//	'should error if lock TTL is wrong type'        (Go: unrepresentable, documented)
//	'should error if lockTTLSeconds is small'      → TestRedisLockerTTL/small
//	'should error if lockTTLSeconds is huge'       → .../huge
//	'should extend the lock' / 'should extend an already timed out lock'
//	  → TestRedisLockerExtend...
//	'should throw if redis-sentinel is used'       → TestCreateClientSentinel
//	'should work with no opts'                     → TestCreateClientNoOpts
//	'should use the ioredis driver in single-instance mode'
//	  / 'should pass standardOpts to the driver'    → TestCreateClientSingleNode
//	'should use the ioredis driver in cluster mode'
//	  / 'should strip key_schema' / 'should pass settings options'
//	  → TestCreateClientCluster...
//	'should return the standard return array' / 'return the standard return array when there is one error'
//	  → TestClientExecUnwrap...
//
// plus the Node-manual-only surface the unit suite does not pin, brought
// under test per the port policy (cleanupTestRedis safety check).

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCreateClientNoOpts(t *testing.T) {
	t.Parallel()
	// Node: `client = createClient()` — works fine, standardOpts default.
	d := &fakeDriver{}
	client, err := CreateClient(nil, d)
	if err != nil {
		t.Fatal(err)
	}
	if !d.configured {
		t.Fatal("driver constructor not called")
	}
	if got := d.configureOpts["retry_max_delay"]; got != 5000 {
		t.Fatalf("retry_max_delay default: got %v want 5000", got)
	}
	if d.configureCluster != nil {
		t.Fatalf("cluster config: got %v want nil", d.configureCluster)
	}
	if client.ClusterConfig != nil {
		t.Fatal("client.ClusterConfig should be nil in single-instance mode")
	}
	if client.Options["retry_max_delay"] != 5000 {
		t.Fatalf("client.options.retry_max_delay: got %v want 5000", client.Options["retry_max_delay"])
	}
}

func TestCreateClientSingleNodeStandardOpts(t *testing.T) {
	t.Parallel()
	// Node settings { host: 'localhost', port: '6379', auth_pass: 'secret' }
	// → `expect(ioredisConstructor.calledWith(sinon.match(standardOpts))).to.be.true`
	//   standardOpts = { host, port, auth_pass, retry_max_delay: 5000 }
	d := &fakeDriver{}
	opts := map[string]any{
		"host":       "localhost",
		"port":       "6379",
		"auth_pass":  "secret",
		"key_schema": map[string]any{"$schema": "redis"},
	}
	client, err := CreateClient(opts, d)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"host":            "localhost",
		"port":            "6379",
		"auth_pass":       "secret",
		"retry_max_delay": 5000,
	}
	if !reflectEqual(d.configureOpts, want) {
		t.Fatalf("driver opts: got %v want %v", d.configureOpts, want)
	}
	if _, hasSchema := d.configureOpts["key_schema"]; hasSchema {
		t.Fatal("key_schema should be stripped from driver opts")
	}
	if !reflectEqual(client.Options, want) {
		t.Fatalf("client.options: got %v want %v", client.Options, want)
	}
}

func TestCreateClientExistingRetryMaxDelay(t *testing.T) {
	t.Parallel()
	// Node: an explicitly-passed retry_max_delay is preserved.
	d := &fakeDriver{}
	_, err := CreateClient(map[string]any{"retry_max_delay": 1234, "host": "h"}, d)
	if err != nil {
		t.Fatal(err)
	}
	if d.configureOpts["retry_max_delay"] != 1234 {
		t.Fatalf("retry_max_delay: got %v want 1234", d.configureOpts["retry_max_delay"])
	}
}

func TestCreateClientSentinel(t *testing.T) {
	t.Parallel()
	// Node: expect createClient({endpoints: this.sentinel}) to be a rejected
	// promise with exact message '@overleaf/redis-wrapper: redis-sentinel is
	// no longer supported'.
	d := &fakeDriver{}
	sentinel := []any{"redis://sentinel-1:26379", "redis://sentinel-2:26379"}
	_, err := CreateClient(map[string]any{"endpoints": sentinel}, d)
	if err == nil {
		t.Fatal("expected error for redis-sentinel endpoints")
	}
	const want = "@overleaf/redis-wrapper: redis-sentinel is no longer supported"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
	if d.configured {
		t.Fatal("driver constructor must not be called when sentinel is rejected")
	}
}

func TestCreateClientCluster(t *testing.T) {
	t.Parallel()
	// Node 'should use the ioredis driver in cluster mode':
	//   { cluster: this.cluster } → client.constructor === ioredis.Cluster,
	//   client.config === cluster.
	cluster := []any{"redis://node-1:6379", "redis://node-2:6379"}
	d := &fakeDriver{}
	client, err := CreateClient(map[string]any{"cluster": cluster}, d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflectEqual(d.configureCluster, cluster) {
		t.Fatalf("cluster config: got %v want %v", d.configureCluster, cluster)
	}
	if client.ClusterConfig == nil {
		t.Fatal("client.ClusterConfig should be the cluster nodes in cluster mode")
	}
	if d.configureOpts["retry_max_delay"] != 5000 {
		t.Fatalf("cluster mode opts.retry_max_delay: got %v want 5000", d.configureOpts["retry_max_delay"])
	}
	if _, hasCluster := d.configureOpts["cluster"]; hasCluster {
		t.Fatal("cluster config must be stripped from the driver opts (Node: delete standardOpts.cluster)")
	}
}

func TestCreateClientClusterStripsKeySchemaPassesOptions(t *testing.T) {
	t.Parallel()
	// Node 'should pass settings options':
	//   settings = { cluster, redisOptions: {keepAlive:100}, key_schema }
	//   expect(client.options).to.deep.equal({ redisOptions: {keepAlive:100},
	//                                          retry_max_delay: 5000 })
	extraOptions := map[string]any{"keepAlive": 100}
	cluster := []any{"redis://node-1:6379"}
	keySchema := map[string]any{
		"$schema":    "redis",
		"key_schema": "foobar",
	}
	d := &fakeDriver{}
	client, err := CreateClient(map[string]any{
		"cluster":      cluster,
		"redisOptions": extraOptions,
		"key_schema":   keySchema,
	}, d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflectEqual(d.configureCluster, cluster) {
		t.Fatalf("cluster config: got %v want %v", d.configureCluster, cluster)
	}
	wantOpts := map[string]any{
		"redisOptions":    extraOptions,
		"retry_max_delay": 5000,
	}
	if !reflectEqual(d.configureOpts, wantOpts) {
		t.Fatalf("cluster driver opts: got %v want %v", d.configureOpts, wantOpts)
	}
	if !reflectEqual(client.Options, wantOpts) {
		t.Fatalf("client.options: got %v want %v", client.Options, wantOpts)
	}
}

// --- client.multi().exec() unwrap (the Node monkey-patch) ------------------

func TestClientExecUnwrapSuccess(t *testing.T) {
	t.Parallel()
	// Node: fake client.multi() result [[null, 42], [null, 'foo']] →
	// resolved with [42, 'foo'].
	d := &fakeDriver{}
	client := &Client{Driver: d}
	d.execRows = [][][]any{
		{{nil, 42}, {nil, "foo"}},
	}
	values, err := client.Exec(context.Background(), []Op{{"GET", "a"}, {"GET", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflectEqual(values, []any{42, "foo"}) {
		t.Fatalf("unwrapped values: got %v want [42 foo]", values)
	}
}

func TestClientExecUnwrapError(t *testing.T) {
	t.Parallel()
	// Node: result [[null, 42], ['error', 'foo']] → rejected with 'error'.
	d := &fakeDriver{}
	client := &Client{Driver: d}
	d.execRows = [][][]any{
		{{nil, 42}, {"error", "foo"}},
	}
	_, err := client.Exec(context.Background(), []Op{{"GET", "a"}, {"GET", "b"}})
	if err == nil {
		t.Fatal("expected the row error to be raised")
	}
	if err.Error() != "error" {
		t.Fatalf("row error: got %q want %q", err.Error(), "error")
	}
}

func TestClientExecDriverError(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{execErr: errors.New("connection refused")}
	client := &Client{Driver: d}
	_, err := client.Exec(context.Background(), []Op{{"GET", "a"}})
	if err == nil || err.Error() != "connection refused" {
		t.Fatalf("driver error propagation: got %v", err)
	}
}

// --- cleanupTestRedis / ensureTestRedis ------------------------------------

func TestCleanupTestRedisRefusesWrongHost(t *testing.T) {
	d := &fakeDriver{host: "redis_prod"}
	client := &Client{Driver: d, Options: map[string]any{"host": "redis_prod"}}
	t.Setenv("NODE_ENV", "test")
	err := CleanupTestRedis(client)
	if err == nil {
		t.Fatal("expected refusal")
	}
	const want = "Refusing to clear Redis instance 'redis_prod' in environment 'test'"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
	if d.flushed {
		t.Fatal("FLUSHALL must not run when refused")
	}
}

func TestCleanupTestRedisRefusesWrongEnv(t *testing.T) {
	d := &fakeDriver{host: "redis_test"}
	client := &Client{Driver: d, Options: map[string]any{"host": "redis_test"}}
	t.Setenv("NODE_ENV", "prod")
	err := CleanupTestRedis(client)
	if err == nil {
		t.Fatal("expected refusal")
	}
	const want = "Refusing to clear Redis instance 'redis_test' in environment 'prod'"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
}

func TestCleanupTestRedisFlushesTestInstance(t *testing.T) {
	d := &fakeDriver{host: "redis_test"}
	client := &Client{Driver: d, Options: map[string]any{"host": "redis_test"}}
	t.Setenv("NODE_ENV", "test")
	if err := CleanupTestRedis(client); err != nil {
		t.Fatal(err)
	}
	if !d.flushed {
		t.Fatal("FLUSHALL should have been called")
	}
}

func TestCleanupTestRedisPropagatesFlushError(t *testing.T) {
	d := &fakeDriver{host: "redis_test", flushErr: fmt.Errorf("flushing: no good")}
	client := &Client{Driver: d, Options: map[string]any{"host": "redis_test"}}
	t.Setenv("NODE_ENV", "test")
	err := CleanupTestRedis(client)
	if err == nil || err.Error() != "flushing: no good" {
		t.Fatalf("flush error propagation: got %v", err)
	}
}
