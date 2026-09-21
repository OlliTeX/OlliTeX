package ometrics

import "testing"

type warnLogger struct{ n int }

func (w *warnLogger) Warn(map[string]any, string, ...any) { w.n++ }

func TestEventLoopMonitor(t *testing.T) {
	t.Run("with a logger provided", func(t *testing.T) {
		oldRD := RegisterDestructor
		var called bool
		RegisterDestructor = func(func()) { called = true }
		defer func() {
			RegisterDestructor = oldRD
			Close() // stop the interval goroutine (Node: clearInterval)
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
