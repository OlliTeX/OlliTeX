package ometrics

// prometheus.go — D22 Phase-A exposition layer for the ometrics registry:
// Prometheus TEXT FORMAT (spec 0.0.4) over the same internal registry the
// Node-@overleaf/metrics port implements (counter/gauge/summary/histogram,
// default labels, quantiles, le buckets). This is the "ometrics bridge" of
// the D22 plan: services calling Inc/Count/Summary/Timing/Histogram/Gauge
// end up scrapeable at /metrics with ZERO new dependencies (the registry is
// dependency-free by design; Prometheus scrapes this text natively).

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// PrometheusContentType is the required Content-Type for text exposition.
const PrometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

// encodeLabelValue applies the Prometheus text-spec escaping (backslash,
// double quote, newline). Rune literals keep the quote character out of any
// string-literal nesting.
func encodeLabelValue(v string) string {
	q := string(rune(34))  // the double-quote character, spelled numerically (see header note)
	bs := string(rune(92)) // the backslash character
	v = strings.ReplaceAll(v, bs, bs+bs)
	v = strings.ReplaceAll(v, q, bs+q)
	v = strings.ReplaceAll(v, "\n", bs+"n")
	return v
}

func encodeValue(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// kindString maps MetricKind to its Prometheus TYPE token (the names already
// are the prometheus tokens: counter|gauge|summary|histogram).
func kindString(k MetricKind) string {
	return string(k)
}

// writeSeriesLines emits `name{label="v",...} value` sample lines (labels
// sorted for determinism).
func writeSeriesLines(w io.Writer, entries []ValueEntry) error {
	for _, e := range entries {
		var b strings.Builder
		b.WriteString(e.MetricName)
		keys := make([]string, 0, len(e.Labels))
		for k := range e.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(k)
				b.WriteString(`="`)
				b.WriteString(encodeLabelValue(fmt.Sprintf("%v", e.Labels[k])))
				b.WriteString(`"`)
			}
			b.WriteByte('}')
		}
		b.WriteByte(' ')
		b.WriteString(encodeValue(e.Value))
		b.WriteByte('\n')
		if _, err := io.WriteString(w, b.String()); err != nil {
			return err
		}
	}
	return nil
}

// PrometheusEncode writes the whole registry in Prometheus text format.
// Metric order = registration order; HELP/TYPE are emitted once per base
// metric (the _sum/_count/bucket suffix lines share the base TYPE, per spec).
//
// Counter naming note: the Node-faithful registry stores names WITHOUT the
// _total suffix (prom-client appended it only inside its own formatter).
// Prometheus >= 2.x accepts counter samples without _total; we keep the
// stored names so the JSON (getMetricsAsJSON) and the text view agree.
func PrometheusEncode(w io.Writer, r *Registry) error {
	if r == nil {
		r = DefaultRegistry
	}
	for _, m := range r.allMetrics() {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", m.Name(), m.Name()); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", m.Name(), kindString(m.Kind())); err != nil {
			return err
		}
		if err := writeSeriesLines(w, m.Get()); err != nil {
			return err
		}
	}
	return nil
}

// PrometheusHandler serves the registry snapshot as Prometheus text format.
// Mount at /metrics on the service INTERNAL port only: the Go services bind
// docker-internal listeners and the HAProxy edge does not forward /metrics —
// any future public exposure is an explicit owner decision.
func PrometheusHandler(r *Registry) http.Handler {
	if r == nil {
		r = DefaultRegistry
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", PrometheusContentType)
		w.WriteHeader(http.StatusOK)
		_ = PrometheusEncode(w, r)
	})
}

// routeLabel turns a concrete path into a low-cardinality route label:
// 24-hex mongo ids, 32-hex ids and UUIDs collapse to placeholders — the same
// granularity Node's metrics recorded (req.route.path, the pattern, not the
// instance). Low cardinality is the contract: a metrics label that
// grows unboundedly is a Prometheus cardinality incident.
func routeLabel(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segments {
		l := len(s)
		if l == 24 || l == 32 {
			hexish := true
			for _, c := range s {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					hexish = false
					break
				}
			}
			if hexish {
				segments[i] = "{id}"
				continue
			}
		}
		if l == 36 && strings.Count(s, "-") == 4 && s[8] == '-' && s[13] == '-' {
			segments[i] = "{uuid}"
		}
	}
	out := "/" + strings.Join(segments, "/")
	if len(out) > 128 {
		out = out[:128]
	}
	return out
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

// HTTPMiddleware records one counter and one summary per HTTP request using
// the Node-faithful metric names (`requests` counter with
// {method,path,code}; `request_time` summary in ms) so Grafana panels and
// alert rules written against the old registry keep reading the same series.
// The route label is low-cardinality (ids collapsed) — a Prometheus
// cardinality contract.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := NowMS()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		code := rec.status
		if code == 0 {
			code = http.StatusOK
		}
		labels := map[string]any{
			"method": r.Method,
			"path":   routeLabel(r.URL.Path),
			"code":   code,
		}
		ms := NowMS() - start
		Inc("requests", labels)
		Summary("request_time", float64(ms), labels)
	})
}
