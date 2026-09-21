package ometrics

import "sync"

// --- the mongo client surface mongodb.js reads from ---------------------------

// MongoClient is the narrow mongo-driver surface the module monitors (Node:
// `mongoClient.on(...)`, `mongoClient.topology?.s?.servers`).
type MongoClient interface {
	// On registers a command-event handler (Node: `client.on(event, handler)`).
	On(event string, handler func(*CommandEvent))
	// Topology returns the (possibly absent) server topology (Node:
	// `client.topology?.s`).
	Topology() *TopologyInner
}

// CommandEvent is a mongo command event (commandStarted/Succeeded/Failed).
type CommandEvent struct {
	CommandName string
	Command     map[string]any
	Reply       *CommandReply
	Duration    float64
}

// CommandReply / CommandCursor model `event.reply?.cursor?.ns`.
type CommandReply struct {
	Cursor *CommandCursor
}

type CommandCursor struct {
	Ns string
}

// MongoPoolOptions is `pool.options`.
type MongoPoolOptions struct {
	MaxPoolSize int
}

// MongoPool is the connection-pool surface mongodb.js reads.
type MongoPool struct {
	TotalConnectionCount     int
	AvailableConnectionCount int
	WaitQueueSize            int
	Options                  MongoPoolOptions
}

// MongoServerInner is `server.s` (driver v4 shape `{ pool }`).
type MongoServerInner struct {
	Pool *MongoPool
}

// MongoServer is one topology server (supports both `server.s.pool` and
// `server.pool` — the v4/v5 shapes the Node source handles).
type MongoServer struct {
	S    *MongoServerInner
	Pool *MongoPool
}

// TopologyInner is `topology.s` `{ servers }`.
type TopologyInner struct {
	Servers map[string]*MongoServer
}

// --- module state (mongodb.js) -------------------------------------------------

// MongoMetrics is the created metric set (Node: the module-level `metrics`).
type MongoMetrics struct {
	poolSize, availableConnections, waitQueueSize, maxPoolSize *Metric
	mongoCommandStarted                                        *Metric
	mongoCommandTimer                                          *Metric
}

var (
	mongoMu         sync.Mutex
	mongoMetrics    *MongoMetrics
	mongoCollectFns []func()
)

// initMetricsOnce mirrors mongodb.initMetricsOnce.
func initMetricsOnce(clientLabel string) *MongoMetrics {
	mongoMu.Lock()
	defer mongoMu.Unlock()
	if mongoMetrics != nil {
		return mongoMetrics
	}
	poolLabelNames := []string{"mongo_server"}
	if clientLabel != "" {
		poolLabelNames = append(poolLabelNames, "client")
	}
	poolSize := DefaultRegistry.Metric(KindGauge, "mongo_connection_pool_size", poolLabelNames, nil)
	available := DefaultRegistry.Metric(KindGauge, "mongo_connection_pool_available", poolLabelNames, nil)
	waiting := DefaultRegistry.Metric(KindGauge, "mongo_connection_pool_waiting", poolLabelNames, nil)
	maxSize := DefaultRegistry.Metric(KindGauge, "mongo_connection_pool_max", poolLabelNames, nil)
	started := DefaultRegistry.Metric(KindCounter, "mongo_command_started", []string{"method", "collection"}, nil)
	timer := DefaultRegistry.Metric(KindSummary, "mongo_command_time", []string{"status", "method", "ns"}, nil)

	mm := &MongoMetrics{
		poolSize:             poolSize,
		availableConnections: available,
		waitQueueSize:        waiting,
		maxPoolSize:          maxSize,
		mongoCommandStarted:  started,
		mongoCommandTimer:    timer,
	}

	// Node: only poolSize carries the collect() hook that resets all four pool
	// gauges and re-runs every registered collect fn.
	poolSize.SetCollect(func() {
		poolSize.Reset()
		available.Reset()
		waiting.Reset()
		maxSize.Reset()
		mongoMu.Lock()
		fns := append([]func(){}, mongoCollectFns...)
		mongoMu.Unlock()
		for _, fn := range fns {
			fn()
		}
	})

	mongoMetrics = mm
	return mongoMetrics
}

func findReadMethod(commandName string) string {
	if commandName == "find" {
		return "read"
	}
	return "write"
}

// Monitor mirrors mongodb.monitor.
func MongoMonitor(client MongoClient, clientLabel string) {
	mm := initMetricsOnce(clientLabel)

	client.On("commandStarted", func(ev *CommandEvent) {
		collection, ok := ev.Command[ev.CommandName]
		if !ok {
			return
		}
		collStr, isStr := collection.(string)
		if !isStr {
			return // Lifecycle commands
		}
		if ev.CommandName == "create" {
			return // Mongoose init
		}
		mm.mongoCommandStarted.Inc(map[string]any{
			"method":     findReadMethod(ev.CommandName),
			"collection": collStr,
		})
	})

	client.On("commandSucceeded", func(ev *CommandEvent) {
		ns := ""
		if ev.Reply != nil && ev.Reply.Cursor != nil {
			ns = ev.Reply.Cursor.Ns
		}
		mm.mongoCommandTimer.Observe(map[string]any{
			"status": "success",
			"method": findReadMethod(ev.CommandName),
			"ns":     ns,
		}, ev.Duration)
	})

	client.On("commandFailed", func(ev *CommandEvent) {
		mm.mongoCommandTimer.Observe(map[string]any{
			"status": "failed",
			"method": findReadMethod(ev.CommandName),
		}, ev.Duration)
	})

	collect := func() {
		top := client.Topology()
		if top == nil || top.Servers == nil {
			return
		}
		for address, server := range top.Servers {
			var pool *MongoPool
			if server != nil {
				if server.S != nil {
					pool = server.S.Pool
				}
				if pool == nil {
					pool = server.Pool
				}
			}
			if pool == nil {
				continue
			}
			labels := map[string]any{"mongo_server": address}
			if clientLabel != "" {
				labels["client"] = clientLabel
			}
			mm.poolSize.Set(labels, float64(pool.TotalConnectionCount))
			mm.availableConnections.Set(labels, float64(pool.AvailableConnectionCount))
			mm.waitQueueSize.Set(labels, float64(pool.WaitQueueSize))
			mm.maxPoolSize.Set(labels, float64(pool.Options.MaxPoolSize))
		}
	}

	mongoMu.Lock()
	mongoCollectFns = append(mongoCollectFns, collect)
	mongoMu.Unlock()
}

// MongoReset mirrors mongodb.reset.
func MongoReset() {
	mongoMu.Lock()
	mongoMetrics = nil
	mongoCollectFns = mongoCollectFns[:0]
	mongoMu.Unlock()
}
