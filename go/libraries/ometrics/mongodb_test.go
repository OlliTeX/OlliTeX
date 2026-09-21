package ometrics

import (
	"fmt"
	"reflect"
	"testing"
)

// fakeMongoClient implements MongoClient (on is a no-op, topology is live).
type fakeMongoClient struct {
	topology *TopologyInner
}

func (fakeMongoClient) On(string, func(*CommandEvent)) {}
func (f fakeMongoClient) Topology() *TopologyInner     { return f.topology }

func mongoGetMetrics() map[string]float64 {
	result := map[string]float64{}
	for _, metric := range DefaultRegistry.GetMetricsAsJSON() {
		for _, value := range metric.Values {
			server := "undefined"
			if s, ok := value.Labels["mongo_server"]; ok {
				server = toStringKey(s)
			}
			key := metric.Name + ":" + server
			if c, ok := value.Labels["client"]; ok && fmt.Sprint(c) != "" {
				key += ":" + toStringKey(c)
			}
			result[key] = value.Value
		}
	}
	return result
}

func TestMongoMonitor(t *testing.T) {
	pool := &MongoPool{
		TotalConnectionCount:     8,
		AvailableConnectionCount: 2,
		WaitQueueSize:            4,
		Options:                  MongoPoolOptions{MaxPoolSize: 10},
	}
	server := &MongoServer{S: &MongoServerInner{Pool: pool}}

	t.Run("handles an unconnected client", func(t *testing.T) {
		DefaultRegistry.Clear()
		MongoReset()
		MongoMonitor(fakeMongoClient{topology: nil}, "")
		if got := mongoGetMetrics(); !reflect.DeepEqual(got, map[string]float64{}) {
			t.Fatalf("metrics = %v, want {}", got)
		}
	})

	expect4 := map[string]float64{
		"mongo_connection_pool_max:server1":       10,
		"mongo_connection_pool_size:server1":      8,
		"mongo_connection_pool_available:server1": 2,
		"mongo_connection_pool_waiting:server1":   4,
	}

	t.Run("collects Mongo metrics", func(t *testing.T) {
		DefaultRegistry.Clear()
		MongoReset()
		servers := map[string]*MongoServer{"server1": server}
		MongoMonitor(fakeMongoClient{topology: &TopologyInner{Servers: servers}}, "")
		if got := mongoGetMetrics(); !reflect.DeepEqual(got, expect4) {
			t.Fatalf("metrics = %v, want %v", got, expect4)
		}
	})

	expect4Client := map[string]float64{
		"mongo_connection_pool_max:server1:native":       10,
		"mongo_connection_pool_size:server1:native":      8,
		"mongo_connection_pool_available:server1:native": 2,
		"mongo_connection_pool_waiting:server1:native":   4,
	}
	t.Run("collects Mongo metrics with client", func(t *testing.T) {
		DefaultRegistry.Clear()
		MongoReset()
		servers := map[string]*MongoServer{"server1": server}
		MongoMonitor(fakeMongoClient{topology: &TopologyInner{Servers: servers}}, "native")
		if got := mongoGetMetrics(); !reflect.DeepEqual(got, expect4Client) {
			t.Fatalf("metrics = %v, want %v", got, expect4Client)
		}
	})

	t.Run("handles topology changes", func(t *testing.T) {
		DefaultRegistry.Clear()
		MongoReset()
		servers := map[string]*MongoServer{"server1": server}
		MongoMonitor(fakeMongoClient{topology: &TopologyInner{Servers: servers}}, "")

		if got := mongoGetMetrics(); !reflect.DeepEqual(got, expect4) {
			t.Fatalf("initial metrics = %v, want %v", got, expect4)
		}

		// Add a server
		servers["server2"] = server
		expectBoth := map[string]float64{
			"mongo_connection_pool_max:server1":       10,
			"mongo_connection_pool_size:server1":      8,
			"mongo_connection_pool_available:server1": 2,
			"mongo_connection_pool_waiting:server1":   4,
			"mongo_connection_pool_max:server2":       10,
			"mongo_connection_pool_size:server2":      8,
			"mongo_connection_pool_available:server2": 2,
			"mongo_connection_pool_waiting:server2":   4,
		}
		if got := mongoGetMetrics(); !reflect.DeepEqual(got, expectBoth) {
			t.Fatalf("both metrics = %v, want %v", got, expectBoth)
		}

		// Delete a server
		delete(servers, "server1")
		expect2 := map[string]float64{
			"mongo_connection_pool_max:server2":       10,
			"mongo_connection_pool_size:server2":      8,
			"mongo_connection_pool_available:server2": 2,
			"mongo_connection_pool_waiting:server2":   4,
		}
		if got := mongoGetMetrics(); !reflect.DeepEqual(got, expect2) {
			t.Fatalf("server2 metrics = %v, want %v", got, expect2)
		}

		// Delete the last server
		delete(servers, "server2")
		if got := mongoGetMetrics(); !reflect.DeepEqual(got, map[string]float64{}) {
			t.Fatalf("empty metrics = %v, want {}", got)
		}
	})
}
