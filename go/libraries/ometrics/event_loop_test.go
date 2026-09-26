package ometrics

import "testing"

type warnLogger struct{ n int }

func (w *warnLogger) Warn(map[string]any, string, ...any) { w.n++ }

func TestEventLoopMonitor(t *testing.T) {
	t.Run("with a logger provided", func(t *testing.T) {
		oldRD := RegisterDestructor
		var called bool
		var stopFn []func()
		RegisterDestructor = func(f func()) { called = true; stopFn = append(stopFn, f) }
		defer func() {
			// Stop the tick goroutine (and drain its in-flight tick) BEFORE
			// restoring the globals it reads — otherwise it leaks and races
			// every later test that swaps `recorder`/`NowMS`.
			for _, f := range stopFn {
				f()
			}
			WaitLoopMonitors() // deterministic drain before global restore
			RegisterDestructor = oldRD
		}()

		EventLoopMonitor(&warnLogger{}, 0, 0)
		if !called {
			t.Fatal("registerDestructor was not called")
		}
	})

	t.Run("without a logger provided", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic 'logger is undefined'")
			}
			if s, ok := r.(string); !ok || s != "logger is undefined" {
				t.Fatalf("panic value = %v, want 'logger is undefined'", r)
			}
		}()
		EventLoopMonitor(nil, 0, 0)
	})
}
