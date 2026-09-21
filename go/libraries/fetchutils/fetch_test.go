package fetchutils

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"ollitex/go/libraries/oerror"
)

// fetch_test.go — port of test/unit/FetchUtils.test.js (56 Node cases) to the
// Go API surface; the TestServer endpoints are identical to TestServer.js.

// expectContextCanceled asserts the request failed due to ctx cancellation.
// Go divergence note: oerror deliberately does not Unwrap non-OError causes
// into the stack (Node OError.stack parity pinned in oerror tests), so we
// assert the transport error text rather than errors.Is(..., context.Canceled).
func expectContextCanceled(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error from the canceled request")
	}
	var oe *oerror.OError
	if !errors.As(err, &oe) {
		t.Fatalf("want *oerror.OError, got %v", err)
	}
	if !strings.Contains(oe.Message, "context canceled") {
		t.Fatalf("message %q does not indicate context cancellation", oe.Message)
	}
}

func asMap(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(err)
	}
	return m
}

func TestFetchJson(t *testing.T) {
	ts := newTestServer(t)

	t.Run("parses a JSON response", func(t *testing.T) {
		v, err := FetchJson(context.Background(), ts.url("/json/hello"))
		if err != nil {
			t.Fatal(err)
		}
		if asMap(v)["msg"] != "hello" {
			t.Fatalf("got %v", v)
		}
	})

	t.Run("parses JSON in the request", func(t *testing.T) {
		v, err := FetchJson(context.Background(), ts.url("/json/add"), &Options{
			Method: http.MethodPost,
			JSON:   map[string]any{"a": 2, "b": 3},
		})
		if err != nil {
			t.Fatal(err)
		}
		if asMap(v)["sum"] != float64(5) {
			t.Fatalf("got %v", v)
		}
	})

	t.Run("accepts stringified JSON as body", func(t *testing.T) {
		v, err := FetchJson(context.Background(), ts.url("/json/add"), &Options{
			Method:  http.MethodPost,
			Body:    strings.NewReader(`{"a":2,"b":3}`),
			Headers: http.Header{"Content-Type": {"application/json"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if asMap(v)["sum"] != float64(5) {
			t.Fatalf("got %v", v)
		}
	})

	t.Run("returns FetchError when the payload is not JSON", func(t *testing.T) {
		_, err := FetchJson(context.Background(), ts.url("/badjson"))
		var fe *FetchError
		if !errors.As(err, &fe) {
			t.Fatalf("got %v, want *FetchError", err)
		}
	})

	t.Run("handles errors when the payload is JSON", func(t *testing.T) {
		_, err := FetchJson(context.Background(), ts.url("/json/500"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
		if rfe.Status != 500 {
			t.Fatalf("status = %d", rfe.Status)
		}
	})

	t.Run("handles errors when the payload is not JSON", func(t *testing.T) {
		_, err := FetchJson(context.Background(), ts.url("/500"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
	})

	t.Run("supports abort signals (context)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := FetchJson(ctx, ts.url("/hang"))
			done <- err
		}()
		ts.waitUntilRequest(t, "/hang")
		cancel()
		expectContextCanceled(t, <-done)
		ts.expectHangCancelled(t)
	})

	t.Run("supports basic auth", func(t *testing.T) {
		v, err := FetchJson(context.Background(), ts.url("/json/basic-auth"), &Options{
			BasicAuth: &BasicAuth{User: "user", Password: "pass"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if asMap(v)["key"] != "verysecret" {
			t.Fatalf("got %v", v)
		}
	})

	t.Run("sets an Authorization header for basic auth", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/basic-auth"), &Options{
			BasicAuth: &BasicAuth{User: "user", Password: "pass"},
		})
		req := ts.lastRequest()
		want := "Basic dXNlcjpwYXNz"
		if got := req.Header.Get("Authorization"); got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
	})

	t.Run("destroys the request body if it doesn't get consumed", func(t *testing.T) {
		body := newInfiniteBody()
		url := rawEarlyResponder(t)
		_, _ = FetchJson(context.Background(), url, &Options{
			Method: http.MethodPost,
			Body:   body,
		})
		deadline := time.Now().Add(2 * time.Second)
		for !body.Destroyed() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !body.Destroyed() {
			t.Fatalf("request body not destroyed (Node: stream.destroyed=true)")
		}
	})
}

func TestFetchJsonHeaders(t *testing.T) {
	ts := newTestServer(t)

	t.Run("sets an Accept header of application/json by default", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"))
		if got := ts.lastRequest().Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
	})

	t.Run("passes provided headers", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
			Headers: http.Header{"X-Some-Value": {"value"}},
		})
		if got := ts.lastRequest().Header.Get("X-Some-Value"); got != "value" {
			t.Fatalf("x-some-value = %q", got)
		}
	})

	t.Run("respects an explicitly provided Accept header", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
			Headers: http.Header{"Accept": {"application/vnd.api+json"}},
		})
		if got := ts.lastRequest().Header.Get("Accept"); got != "application/vnd.api+json" {
			t.Fatalf("Accept = %q", got)
		}
	})

	t.Run("maps header values provided as map[string]any", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
			Headers: map[string]any{"X-Foo": "bar"},
		})
		if got := ts.lastRequest().Header.Get("X-Foo"); got != "bar" {
			t.Fatalf("x-foo = %q", got)
		}
		if got := ts.lastRequest().Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
	})

	t.Run("treats an unset Accept header as absent and applies the default", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
			Headers: map[string]any{"Accept": nil},
		})
		if got := ts.lastRequest().Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
	})

	t.Run("omits headers with unset values rather than sending undefined", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
			Headers: map[string]any{"X-Some-Value": nil, "X-Other-Value": nil},
		})
		req := ts.lastRequest()
		if req.Header.Get("X-Some-Value") != "" || req.Header.Values("X-Some-Value") != nil {
			t.Fatalf("x-some-value should be omitted: %v", req.Header)
		}
		if req.Header.Get("X-Other-Value") != "" || req.Header.Values("X-Other-Value") != nil {
			t.Fatalf("x-other-value should be omitted: %v", req.Header)
		}
	})

	t.Run("omits unset pair headers provided as tuples", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
			Headers: []any{[2]any{"X-Foo", "bar"}, [2]any{"X-Unset", nil}},
		})
		req := ts.lastRequest()
		if got := req.Header.Get("X-Foo"); got != "bar" {
			t.Fatalf("x-foo = %q", got)
		}
		if req.Header.Values("X-Unset") != nil {
			t.Fatalf("x-unset should be omitted: %v", req.Header)
		}
	})

	t.Run("sets a Content-Type header of application/json when sending a JSON body", func(t *testing.T) {
		_, _ = FetchJson(context.Background(), ts.url("/json/add"), &Options{
			Method: http.MethodPost,
			JSON:   map[string]any{"a": 2, "b": 3},
		})
		if got := ts.lastRequest().Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
	})
}

func TestFetchStream(t *testing.T) {
	ts := newTestServer(t)

	t.Run("returns a stream", func(t *testing.T) {
		stream, err := FetchStream(context.Background(), ts.url("/large"))
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		raw, err := io.ReadAll(stream)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != ts.largePayload {
			t.Fatalf("len = %d, want %d", len(raw), len(ts.largePayload))
		}
	})

	t.Run("closes when the stream is closed", func(t *testing.T) {
		stream, err := FetchStream(context.Background(), ts.url("/large"))
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if _, err := io.ReadAll(stream); err == nil {
			t.Fatalf("reading after close should fail")
		}
	})

	t.Run("aborts before transfer when the request body is destroyed", func(t *testing.T) {
		body := newInfiniteBody()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		errch := make(chan error, 1)
		go func() {
			_, err := FetchStream(ctx, ts.url("/hang"), &Options{Method: http.MethodPost, Body: body})
			errch <- err
		}()
		body.Close() // destroy before transfer
		err := <-errch
		if err == nil {
			t.Fatalf("expected an error after destroying the request body")
		}
	})

	t.Run("handles errors", func(t *testing.T) {
		_, err := FetchStream(context.Background(), ts.url("/500"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
	})

	t.Run("supports abort signals (context)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := FetchStream(ctx, ts.url("/hang"))
			done <- err
		}()
		ts.waitUntilRequest(t, "/hang")
		cancel()
		expectContextCanceled(t, <-done)
		ts.expectHangCancelled(t)
	})

	t.Run("destroys the request body when an error occurs", func(t *testing.T) {
		body := newInfiniteBody()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := FetchStream(ctx, ts.url("/hang"), &Options{Method: http.MethodPost, Body: body})
			done <- err
		}()
		ts.waitUntilRequest(t, "/hang")
		cancel()
		<-done
		deadline := time.Now().Add(2 * time.Second)
		for !body.Destroyed() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !body.Destroyed() {
			t.Fatalf("request body not destroyed on error")
		}
	})
}

func TestFetchNothing(t *testing.T) {
	ts := newTestServer(t)

	t.Run("drains and closes the response", func(t *testing.T) {
		resp, err := FetchNothing(context.Background(), ts.url("/large"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("handles errors", func(t *testing.T) {
		_, err := FetchNothing(context.Background(), ts.url("/500"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
	})

	t.Run("doesn't abort when the request body ends normally", func(t *testing.T) {
		_, err := FetchNothing(context.Background(), ts.url("/sink"), &Options{
			Method: http.MethodPost,
			Body:   strings.NewReader("hello there"),
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("supports abort signals (context)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := FetchNothing(ctx, ts.url("/hang"))
			done <- err
		}()
		ts.waitUntilRequest(t, "/hang")
		cancel()
		expectContextCanceled(t, <-done)
		ts.expectHangCancelled(t)
	})

	t.Run("destroys the request body when an error occurs", func(t *testing.T) {
		body := newInfiniteBody()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := FetchNothing(ctx, ts.url("/hang"), &Options{Method: http.MethodPost, Body: body})
			done <- err
		}()
		ts.waitUntilRequest(t, "/hang")
		cancel()
		<-done
		deadline := time.Now().Add(2 * time.Second)
		for !body.Destroyed() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !body.Destroyed() {
			t.Fatalf("request body not destroyed on error")
		}
	})
}

func TestRequestFailedErrorInfo(t *testing.T) {
	ts := newTestServer(t)
	getRejection := func(t *testing.T, url string) *RequestFailedError {
		t.Helper()
		_, err := FetchNothing(context.Background(), url)
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
		return rfe
	}

	cases := []struct {
		path string
		code int
		body string
	}{
		{"/400", 400, "boom-400"}, {"/409", 409, "boom-409"},
		{"/413", 413, "boom-413"}, {"/422", 422, "boom-422"},
	}
	for _, c := range cases {
		t.Run("includes the response body in OError.info for a "+c.path[1:], func(t *testing.T) {
			rfe := getRejection(t, ts.url(c.path))
			if rfe.OError.Info["body"] != c.body {
				t.Fatalf("info[body] = %v, want %q (status %d)", rfe.OError.Info["body"], c.body, c.code)
			}
			if rfe.Info["status"] != c.code {
				t.Fatalf("info[status] = %v, want %d", rfe.Info["status"], c.code)
			}
			if rfe.Info["method"] != "GET" {
				t.Fatalf("info[method] = %v, want GET", rfe.Info["method"])
			}
		})
	}

	t.Run("omits the response body from OError.info for a 500", func(t *testing.T) {
		rfe := getRejection(t, ts.url("/500"))
		if rfe.OError.Info["body"] != nil {
			t.Fatalf("info[body] should be absent: %v", rfe.OError.Info)
		}
		if rfe.Body != "Internal Server Error" {
			t.Fatalf("Body = %q, want Internal Server Error", rfe.Body)
		}
	})
}

func TestFetchString(t *testing.T) {
	ts := newTestServer(t)

	t.Run("returns a string", func(t *testing.T) {
		body, err := FetchString(context.Background(), ts.url("/hello"))
		if err != nil {
			t.Fatal(err)
		}
		if body != "hello" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("handles errors", func(t *testing.T) {
		_, err := FetchString(context.Background(), ts.url("/500"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
	})
}

func TestFetchRedirect(t *testing.T) {
	ts := newTestServer(t)

	t.Run("returns the immediate redirect", func(t *testing.T) {
		loc, err := FetchRedirect(context.Background(), ts.url("/redirect/1"))
		if err != nil {
			t.Fatal(err)
		}
		want := ts.url("/redirect/2")
		if loc != want {
			t.Fatalf("location = %q, want %q", loc, want)
		}
	})

	t.Run("rejects status 200", func(t *testing.T) {
		_, err := FetchRedirect(context.Background(), ts.url("/hello"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
	})

	t.Run("rejects empty redirect with the missing-Location cause", func(t *testing.T) {
		_, err := FetchRedirect(context.Background(), ts.url("/redirect/empty-location"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
		cause, ok := rfe.OError.Cause.(*oerror.OError)
		if !ok {
			t.Fatalf("no OError cause: %v", rfe.OError.Cause)
		}
		if cause.Message != "missing Location response header on 3xx response" {
			t.Fatalf("cause message = %q", cause.Message)
		}
	})

	t.Run("handles errors", func(t *testing.T) {
		_, err := FetchRedirect(context.Background(), ts.url("/500"))
		var rfe *RequestFailedError
		if !errors.As(err, &rfe) {
			t.Fatalf("got %v, want *RequestFailedError", err)
		}
	})
}

func TestResponseAccessOnError(t *testing.T) {
	ts := newTestServer(t)
	// The http.Response is exposed for diagnostics on rejection
	// (Node: fetchJsonWithResponse / fetchStringWithResponse).
	_, err := FetchJson(context.Background(), ts.url("/json/500"))
	var rfe *RequestFailedError
	if !errors.As(err, &rfe) {
		t.Fatalf("got %v, want *RequestFailedError", err)
	}
	if rfe.Response == nil || rfe.Response.StatusCode != 500 {
		t.Fatalf("Response = %+v", rfe.Response)
	}
}
