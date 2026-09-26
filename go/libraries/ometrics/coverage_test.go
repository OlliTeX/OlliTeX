package ometrics

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// ============================================================================
// memory.go
// ============================================================================

type fixedMem struct{ usage MemUsage }

func (f fixedMem) MemoryUsage() MemUsage { return f.usage }

type memLogger struct {
	calls []string
}

func (m *memLogger) Debug(info any, msg string, args ...any) {
	_ = info
	_ = args
	m.calls = append(m.calls, msg)
}

func TestMemoryCheck(t *testing.T) {
	oldSrc, oldGC := memorySource, ForceGC
	oldBucket, oldGI, oldCSL := CpuTimeBucket, gcInterval, countSinceLastGc
	defer func() {
		memorySource, ForceGC = oldSrc, oldGC
		CpuTimeBucket, gcInterval, countSinceLastGc = oldBucket, oldGI, oldCSL
	}()

	DefaultRegistry.Clear()
	mb := int64(1024 * 1024)
	memorySource = fixedMem{usage: MemUsage{RSS: 100 * mb, HeapTotal: 50 * mb, HeapUsed: 40 * mb}}

	t.Run("without gc", func(t *testing.T) {
		ForceGC = nil
		CpuTimeBucket, gcInterval, countSinceLastGc = 100, 1, 0
		lg := &memLogger{}
		MemoryMonitor{}.Check(lg)
		gotVal, _ := getMetricValue("memory_rss")
		if gotVal != 100 {
			t.Fatalf("memory_rss = %v, want 100", gotVal)
		}
	})

	t.Run("with gc", func(t *testing.T) {
		ForceGC = func() {}
		CpuTimeBucket, gcInterval, countSinceLastGc = 50, 1, 10 // readyToGc → true
		lg := &memLogger{}
		MemoryMonitor{}.Check(lg)
		if len(lg.calls) < 2 {
			t.Fatalf("expected >=2 log calls, got %d", len(lg.calls))
		}
	})
}

func TestMemoryMonitor(t *testing.T) {
	oldRD := RegisterDestructor
	called := false
	RegisterDestructor = func(func()) { called = true }
	defer func() {
		RegisterDestructor = oldRD
		Close()
	}()
	MemoryMonitor{}.Monitor()
	if !called {
		t.Fatal("registerDestructor not called")
	}
}

func TestMaxAndInMegaBytes(t *testing.T) {
	if got := max(3, 2); got != 3 {
		t.Fatalf("max(3,2)=%d", got)
	}
	if got := max(2, 3); got != 3 {
		t.Fatalf("max(2,3)=%d", got)
	}
	mb := int64(1024 * 1024)
	b := inMegaBytes(MemUsage{RSS: 2 * mb, HeapTotal: 3 * mb, HeapUsed: 4 * mb})
	if b["rss"] != 2 || b["heapTotal"] != 3 || b["heapUsed"] != 4 {
		t.Fatalf("inMegaBytes = %v", b)
	}
}

// ============================================================================
// leaked_sockets.go
// ============================================================================

func TestFlattenHeaders(t *testing.T) {
	if got := FlattenHeaders([]any{"a", "1", "b", "2"}); got != "a: 1\r\nb: 2\r\n" {
		t.Fatalf("array headers = %q", got)
	}
	if got := FlattenHeaders(map[string]any{"x": "y"}); got != "x: y\r\n" {
		t.Fatalf("map headers = %q", got)
	}
	if got := FlattenHeaders("raw"); got != "raw" {
		t.Fatalf("string headers = %q", got)
	}
	if got := FlattenHeaders(nil); got != "" {
		t.Fatalf("nil headers = %q", got)
	}
}

func TestRedact(t *testing.T) {
	out := Redact(map[string]any{
		"method":  "GET",
		"headers": []any{"authorization", "Bearer secret", "x", "y"},
	})
	if got, _ := out["headers"].(string); got != "authorization: REDACTED\r\nx: y\r\n" {
		t.Fatalf("redacted headers = %q", got)
	}
	nested := Redact(map[string]any{
		"request": map[string]any{"headers": []any{"cookie", "s=1"}},
	})
	req := nested["request"].(map[string]any)
	if got, _ := req["headers"].(string); got != "cookie: REDACTED\r\n" {
		t.Fatalf("nested redacted = %q", got)
	}
}

func TestIsOldSocket(t *testing.T) {
	SetLeakThreshold(1 * time.Millisecond)
	old := &SocketDebug{Request: &SocketPhase{TS: time.Now().Add(-100 * time.Millisecond)}}
	if !isOldSocket(old) {
		t.Fatal("expected old socket")
	}
	fresh := &SocketDebug{Request: &SocketPhase{TS: time.Now()}}
	if isOldSocket(fresh) {
		t.Fatal("expected fresh socket")
	}
}

func TestProcNetTcp(t *testing.T) {
	// local 127.0.0.1:30002 (0100007F:7532), remote 10.0.0.1:443 (0100000A:01BB)
	content := "  sl: local_address rem_address\n   0: 0100007F:7532 0100000A:01BB 01\n"
	sockets := parseProcNetTcp(content)
	key := "127.0.0.1:30002 -> 10.0.0.1:443"
	if _, ok := sockets[key]; !ok {
		t.Fatalf("missing key %q in %v", key, sockets)
	}
	if got := matchProcNetTcpLine("   0: 0100007F:7532 0100000A:01BB 01"); got != key {
		t.Fatalf("matchProcNetTcpLine = %q, want %q", got, key)
	}
	if got, _ := hexAddr("0100007F"); got != "127.0.0.1" {
		t.Fatalf("hexAddr = %q", got)
	}
	if got := hexPort("7532"); got != 30002 {
		t.Fatalf("hexPort = %d", got)
	}
	if got := itoaInt(30002); got != "30002" {
		t.Fatalf("itoaInt = %q", got)
	}
}

type leakLogger struct {
	msgs  []string
	infos []map[string]any
}

func (l *leakLogger) Error(info map[string]any, msg string, args ...any) {
	l.msgs = append(l.msgs, msg)
	l.infos = append(l.infos, info)
}

func (l *leakLogger) Warn(info map[string]any, msg string, args ...any) {
	l.msgs = append(l.msgs, msg)
	l.infos = append(l.infos, info)
}

func TestScanSockets(t *testing.T) {
	oldAH, oldPNT := activeHandles, readProcNetTcp
	defer func() { activeHandles, readProcNetTcp = oldAH, oldPNT }()
	SetLeakThreshold(1 * time.Millisecond)

	h := SocketHandle{
		LocalAddress:  "127.0.0.1",
		LocalPort:     30002,
		RemoteAddress: "10.0.0.1",
		RemotePort:    443,
		Debug:         &SocketDebug{Method: "GET", Protocol: "http", URL: "http://x", Request: &SocketPhase{TS: time.Now().Add(-100 * time.Millisecond), Headers: []any{"authorization", "Bearer secret"}}},
	}
	activeHandles = func() []SocketHandle { return []SocketHandle{h} }
	readProcNetTcp = func() (string, error) {
		return "   0: 0100007F:7532 0100000A:01BB 01\n", nil
	}
	lg := &leakLogger{}
	SocketLeakedMonitor{}.ScanSockets(lg)
	if len(lg.msgs) != 1 || lg.msgs[0] != "old socket handle - tcp socket" {
		t.Fatalf("msgs = %v", lg.msgs)
	}
	if lg.infos[0]["tcpinfo"] == nil {
		t.Fatal("expected tcpinfo")
	}
	if lg.infos[0]["age"] == nil {
		t.Fatal("expected age")
	}
}

func TestScanSocketsNoTCP(t *testing.T) {
	oldAH, oldPNT := activeHandles, readProcNetTcp
	defer func() { activeHandles, readProcNetTcp = oldAH, oldPNT }()
	SetLeakThreshold(1 * time.Millisecond)
	h := SocketHandle{
		LocalAddress:  "127.0.0.1",
		LocalPort:     9,
		RemoteAddress: "10.0.0.1",
		RemotePort:    443,
		Debug:         &SocketDebug{Request: &SocketPhase{TS: time.Now().Add(-100 * time.Millisecond)}},
	}
	activeHandles = func() []SocketHandle { return []SocketHandle{h} }
	readProcNetTcp = func() (string, error) { return "", nil }
	lg := &leakLogger{}
	SocketLeakedMonitor{}.ScanSockets(lg)
	if len(lg.msgs) != 1 || lg.msgs[0] != "stale socket handle - no entry in /proc/net/tcp" {
		t.Fatalf("msgs = %v", lg.msgs)
	}
}

func TestScanSocketsErrors(t *testing.T) {
	oldAH, oldPNT := activeHandles, readProcNetTcp
	defer func() { activeHandles, readProcNetTcp = oldAH, oldPNT }()
	SetLeakThreshold(1 * time.Millisecond)
	// no active handles → early return
	activeHandles = func() []SocketHandle { return nil }
	readProcNetTcp = func() (string, error) { return "", nil }
	lg := &leakLogger{}
	SocketLeakedMonitor{}.ScanSockets(lg)
	if len(lg.msgs) != 0 {
		t.Fatalf("msgs should be empty, got %v", lg.msgs)
	}

	// read error
	h := SocketHandle{LocalAddress: "127.0.0.1", LocalPort: 9, RemoteAddress: "10.0.0.1", RemotePort: 443,
		Debug: &SocketDebug{Request: &SocketPhase{TS: time.Now().Add(-100 * time.Millisecond)}}}
	activeHandles = func() []SocketHandle { return []SocketHandle{h} }
	readProcNetTcp = func() (string, error) { return "", errTest }
	lg2 := &leakLogger{}
	SocketLeakedMonitor{}.ScanSockets(lg2)
	if len(lg2.msgs) != 1 || lg2.msgs[0] != "error getting open sockets" {
		t.Fatalf("msgs = %v", lg2.msgs)
	}
}

var errTest = errors.New("boom")

// TestLeakedMonitor covers the monitor (destructor + helpers).
func TestLeakedMonitor(t *testing.T) {
	oldRD := RegisterDestructor
	called := false
	RegisterDestructor = func(func()) { called = true }
	defer func() {
		RegisterDestructor = oldRD
		Close()
	}()
	SocketLeakedMonitor{}.Monitor(struct{}{})
	if !called {
		t.Fatal("registerDestructor not called")
	}
	// helpers
	SocketLeakedMonitor{}.SetActiveHandles(func() []SocketHandle { return nil })
	SocketLeakedMonitor{}.SetProcNetTcp(func() (string, error) { return "", nil })
	if got := keyFromSocket(SocketHandle{LocalAddress: "127.0.0.1", LocalPort: 1, RemoteAddress: "10.0.0.1", RemotePort: 2}); got != "127.0.0.1:1 -> 10.0.0.1:2" {
		t.Fatalf("keyFromSocket = %q", got)
	}
}

// ============================================================================
// metrics.go / registry.go / index.go / open_sockets.go / uv
// ============================================================================

func TestDefaultRecorderAndSet(t *testing.T) {
	DefaultRegistry.Clear()
	oldRec := recorder
	recorder = DefaultRecorder{}
	defer func() { recorder = oldRec }()
	DefaultRecorder{}.Timing("k", 5, nil)
	DefaultRecorder{}.Summary("k2", 7, map[string]any{"a": 1})
	if got := getSummarySum("timer_k"); got != 5 {
		t.Fatalf("summary timer_k = %v", got)
	}
	if got := getSummarySum("k2"); got != 7 {
		t.Fatalf("summary k2 = %v", got)
	}
	Set("unsupported", 1) // counts not supported → no-op
	if got := SanitizeValue("12.5"); got != 12.5 {
		t.Fatalf("SanitizeValue = %v", got)
	}
	if got := SanitizeValue(3); got != 3 {
		t.Fatalf("SanitizeValue(3) = %v", got)
	}
	if got := SanitizeValue(7.25); got != 7.25 {
		t.Fatalf("SanitizeValue(7.25) = %v", got)
	}
}

func TestRegistryMaintenance(t *testing.T) {
	r := NewRegistry()
	m := r.Metric(KindCounter, "m1", nil, nil)
	m.Inc(nil)
	if r.GetSingleMetric("m1") == nil {
		t.Fatal("m1 missing")
	}
	if r.Metric(KindCounter, "m1", nil, nil) != r.metrics["m1"] {
		t.Fatal("Metric should be cached")
	}
	r.SetTTL(1)
	if r.TTL() != 1 {
		t.Fatal("TTL mismatch")
	}
	m2 := r.Metric(KindGauge, "m2", nil, nil)
	if m2.Name() != "m2" || m2.Kind() != KindGauge {
		t.Fatalf("accessors = %q/%v", m2.Name(), m2.Kind())
	}
	m2.Sweep() // ttl set, lastAccess now → not stale → no removal
	if r.GetSingleMetric("m2") == nil {
		t.Fatal("m2 should still exist")
	}
	// force stale
	m2.lastAccess = time.Now().Add(-2 * time.Minute)
	m2.Sweep()
	if r.GetSingleMetric("m2") != nil {
		t.Fatal("m2 should be swept")
	}
	r.RemoveSingleMetric("m1")
	if r.GetSingleMetric("m1") != nil {
		t.Fatal("m1 should be removed")
	}
	// sweep with ttl 0 → no-op
	r.SetTTL(0)
	m3 := r.Metric(KindGauge, "m3", nil, nil)
	m3.lastAccess = time.Now().Add(-time.Hour)
	m3.Sweep()
	if r.GetSingleMetric("m3") == nil {
		t.Fatal("m3 should survive ttl=0 sweep")
	}
}

func TestUvAndOpenSocketsMonitor(t *testing.T) {
	if got := UvThreadpoolSize(); got != "4" {
		t.Fatalf("UvThreadpoolSize = %q, want 4", got)
	}
	if got := (NoopSockets{}).Connections("http", false); len(got) != 0 {
		t.Fatalf("NoopSockets = %v", got)
	}
	oldRD := RegisterDestructor
	called := false
	RegisterDestructor = func(func()) { called = true }
	defer func() {
		RegisterDestructor = oldRD
		Close()
	}()
	OpenSocketsMonitor{}.Monitor(true)
	if !called {
		t.Fatal("open_sockets monitor destructor not called")
	}
}

func TestMetricsHandler(t *testing.T) {
	DefaultRegistry.Clear()
	Inc("handler_probe")
	_ = MetricsHandler()
}

func TestQuantileEdges(t *testing.T) {
	if got := quantile(nil, 0.5); got != 0 {
		t.Fatalf("quantile(nil) = %v", got)
	}
	// rank clamps
	if got := quantile([]float64{5, 10, 15}, 0.999); got != 15 {
		t.Fatalf("quantile high = %v", got)
	}
	if got := quantile([]float64{5, 10, 15}, 0.001); got != 5 {
		t.Fatalf("quantile low = %v", got)
	}
}

func TestStatusAnyBranches(t *testing.T) {
	if statusAny(nil) != nil {
		t.Fatal("statusAny(nil) should be nil")
	}
	code := 200
	if statusAny(&Responder{Status: &code}) != 200 {
		t.Fatal("statusAny should return 200")
	}
}

func TestGetRoutePathSwaggerAndNull(t *testing.T) {
	req := &Request{Swagger: &SwaggerPath{APIPath: "/api/v1/x"}}
	if p, ok := getRoutePath(req); !ok || p != "/api/v1/x" {
		t.Fatalf("swagger route = %q ok=%v", p, ok)
	}
	empty := &Request{}
	if p, ok := getRoutePath(empty); ok || p != "" {
		t.Fatalf("null route = %q ok=%v", p, ok)
	}
	// route with leading slash stripped
	r2 := &Request{Route: &RoutePath{Path: "/a/b"}}
	if p, _ := getRoutePath(r2); p != "a_b" {
		t.Fatalf("route path = %q", p)
	}
}

func TestParseFloatStrBranches(t *testing.T) {
	if got := parseFloatStr(""); got != 0 {
		t.Fatalf("parseFloatStr(\"\")=%v", got)
	}
	if got := parseFloatStr("  42 "); got != 42 {
		t.Fatalf("parseFloatStr(42)=%v", got)
	}
	_ = reflect.DeepEqual
}

// ============================================================================
// mongodb command handlers + findReadMethod
// ============================================================================

type cmdClient struct {
	topology *TopologyInner
	handlers map[string]func(*CommandEvent)
}

func (c *cmdClient) On(event string, h func(*CommandEvent)) {
	if c.handlers == nil {
		c.handlers = map[string]func(*CommandEvent){}
	}
	c.handlers[event] = h
}

func (c *cmdClient) Topology() *TopologyInner { return c.topology }

func (c *cmdClient) fire(event string, ev *CommandEvent) {
	if h, ok := c.handlers[event]; ok {
		h(ev)
	}
}

func counterTotal(key string) float64 {
	m := getMetricSingle(key)
	if m == nil {
		return 0
	}
	var total float64
	for _, v := range m.Get() {
		if v.MetricName == key {
			total += v.Value
		}
	}
	return total
}

func summaryTotalCount(key string) float64 {
	m := getMetricSingle(key)
	if m == nil {
		return 0
	}
	var total float64
	for _, v := range m.Get() {
		if v.MetricName == key+"_count" {
			total += v.Value
		}
	}
	return total
}

func TestMongoCommandHandlers(t *testing.T) {
	if findReadMethod("find") != "read" || findReadMethod("insert") != "write" {
		t.Fatal("findReadMethod incorrect")
	}
	DefaultRegistry.Clear()
	MongoReset()
	client := &cmdClient{topology: &TopologyInner{Servers: map[string]*MongoServer{}}}
	MongoMonitor(client, "")
	client.fire("commandStarted", &CommandEvent{CommandName: "find", Command: map[string]any{"find": "mycol"}})
	client.fire("commandStarted", &CommandEvent{CommandName: "create", Command: map[string]any{"create": "tmp"}})
	client.fire("commandStarted", &CommandEvent{CommandName: "ping", Command: map[string]any{"ping": map[string]any{}}})
	client.fire("commandSucceeded", &CommandEvent{CommandName: "find", Duration: 5, Reply: &CommandReply{Cursor: &CommandCursor{Ns: "db.coll"}}})
	client.fire("commandFailed", &CommandEvent{CommandName: "insert", Duration: 9})

	if got := counterTotal("mongo_command_started"); got != 1 {
		t.Fatalf("mongo_command_started = %v, want 1", got)
	}
	if got := summaryTotalCount("mongo_command_time"); got != 2 {
		t.Fatalf("mongo_command_time count = %v, want 2", got)
	}
	m := getMetricSingle("mongo_command_started")
	if m == nil {
		t.Fatal("mongo_command_started missing")
	}
	if !reflect.DeepEqual(m.LabelNames(), []string{"collection", "method"}) {
		t.Fatalf("mongo_command_started labelNames = %v", m.LabelNames())
	}
}

// ============================================================================
// event_loop tick branch
// ============================================================================

type recordingWarn struct{ n int }

func (r *recordingWarn) Warn(map[string]any, string, ...any) { r.n++ }

func TestEventLoopTick(t *testing.T) {
	DefaultRegistry.Clear()
	oldRD := RegisterDestructor
	oldRec := recorder
	var stopFn []func()
	RegisterDestructor = func(f func()) { stopFn = append(stopFn, f) }
	recorder = DefaultRecorder{}

	oldNow := NowMS
	nowVal := int64(0)
	NowMS = func() int64 { nowVal += 300; return nowVal }
	// Cleanup order matters: stop the tick goroutine and drain its
	// in-flight iteration BEFORE restoring the globals it reads
	// dynamically (a restore racing an in-flight tick is a data race).
	defer func() {
		t.Logf("DBG stopFn=%d regdtr=%p", len(stopFn), RegisterDestructor)
		for _, s := range stopFn {
			s()
		}
		t.Logf("DBG after stop Fn")
		WaitLoopMonitors()
		t.Logf("DBG drained") // deterministic drain — no in-flight tick may touch
		// the globals we restore next.
		RegisterDestructor = oldRD
		recorder = oldRec
		NowMS = oldNow
	}()

	logger := &recordingWarn{}
	EventLoopMonitor(logger, 1, 100)

	// Poll for the timer to record (a fixed sleep is flaky under
	// full-suite load: the 1ms tick timer can fire late under -race).
	metricName := BuildPromKey("timer_" + "event-loop-millsec")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if summaryTotalCount(metricName) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := summaryTotalCount(metricName); got < 1 {
		t.Fatalf("%s count = %v, want >= 1", metricName, got)
	}
}
