package ologger

import "testing"

func getMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected a map, got %#v", v)
	}
	return m
}

func TestConvertLogEntry_BasicMessageAndSeverity(t *testing.T) {
	entry := Entry{
		"level": 30,
		"msg":   "hello",
		"foo":   "bar",
		"name":  "ignored",
		"pid":   123,
	}
	gcp := ConvertLogEntry(entry)
	if gcp["message"] != "hello" {
		t.Fatalf("expected message from msg, got %v", gcp["message"])
	}
	if gcp["severity"] != "info" {
		t.Fatalf("expected severity info, got %v", gcp["severity"])
	}
	if gcp["foo"] != "bar" {
		t.Fatalf("expected foo retained, got %v", gcp)
	}
	for _, k := range []string{"level", "name", "pid", "msg", "hostname", "v"} {
		if _, has := gcp[k]; has {
			t.Fatalf("expected %q omitted from the GCP entry", k)
		}
	}
}

func TestConvertLogEntry_ErrStackBecomesMessage(t *testing.T) {
	entry := Entry{
		"level": 50,
		"err": map[string]any{
			"message": "boom",
			"stack":   "boom\n    at x",
		},
	}
	gcp := ConvertLogEntry(entry)
	if gcp["message"] != "boom\n    at x" {
		t.Fatalf("expected the stack trace in message, got %v", gcp["message"])
	}
	if gcp["severity"] != "error" {
		t.Fatalf("expected severity error, got %v", gcp["severity"])
	}
	if _, has := gcp["err"]; has {
		t.Fatalf("expected err omitted")
	}
}

func TestConvertLogEntry_ErrInfoCodeSignalName(t *testing.T) {
	entry := Entry{
		"level": 50,
		"name":  "myapp",
		"err": map[string]any{
			"message": "m",
			"signal":  "SIGKILL",
			"code":    42,
			"info":    map[string]any{"a": 1},
		},
	}
	gcp := ConvertLogEntry(entry)
	if gcp["a"] != 1 {
		t.Fatalf("expected err.info merged, got %v", gcp["a"])
	}
	if gcp["code"] != 42 {
		t.Fatalf("expected code copied, got %v", gcp["code"])
	}
	if gcp["signal"] != "SIGKILL" {
		t.Fatalf("expected signal copied, got %v", gcp["signal"])
	}
	if gcp["message"] != "m" {
		t.Fatalf("expected err.message in message, got %v", gcp["message"])
	}
	sc := getMap(t, gcp["serviceContext"])
	if sc["service"] != "myapp" {
		t.Fatalf("expected serviceContext.service myapp, got %v", sc)
	}
}

func TestConvertLogEntry_HTTPRequest(t *testing.T) {
	entry := Entry{
		"level": 20,
		"req": map[string]any{
			"method":        "GET",
			"url":           "/x",
			"remoteAddress": "1.2.3.4",
			"projectId":     "p1",
			"headers": map[string]any{
				"content-length": "100",
				"user-agent":     "ua",
				"referer":        "ref",
			},
		},
		"responseTimeMs": 1500,
	}
	gcp := ConvertLogEntry(entry)
	hp := getMap(t, gcp["httpRequest"])
	if hp["requestMethod"] != "GET" || hp["requestUrl"] != "/x" || hp["remoteIp"] != "1.2.3.4" {
		t.Fatalf("expected req fields in httpRequest, got %#v", hp)
	}
	if hp["requestSize"] != int64(100) {
		t.Fatalf("expected requestSize 100, got %v", hp["requestSize"])
	}
	if hp["userAgent"] != "ua" || hp["referer"] != "ref" {
		t.Fatalf("expected userAgent/referer, got %#v", hp)
	}
	if hp["latency"] != "1.5s" {
		t.Fatalf("expected latency 1.5s, got %v", hp["latency"])
	}
	labels := getMap(t, gcp["logging.googleapis.com/labels"])
	if labels["projectId"] != "p1" {
		t.Fatalf("expected projectId label from req, got %#v", labels)
	}
	if gcp["severity"] != "debug" {
		t.Fatalf("expected severity debug, got %v", gcp["severity"])
	}
}

func TestConvertLogEntry_LabelsFromEntryKeys(t *testing.T) {
	entry := Entry{
		"level":      20,
		"project_id": "pp",
		"user_id":    "uu",
		"doc_id":     "dd",
	}
	gcp := ConvertLogEntry(entry)
	labels := getMap(t, gcp["logging.googleapis.com/labels"])
	if labels["projectId"] != "pp" || labels["userId"] != "uu" || labels["docId"] != "dd" {
		t.Fatalf("expected camelCase labels from snake_case keys, got %#v", labels)
	}
}

func TestConvertLogEntry_NoErrorNoReqStillKeepsCustom(t *testing.T) {
	entry := Entry{"custom": "kept"}
	gcp := ConvertLogEntry(entry)
	if gcp["custom"] != "kept" {
		t.Fatalf("expected custom key retained, got %#v", gcp)
	}
	if _, has := gcp["httpRequest"]; has {
		t.Fatalf("expected no httpRequest without req/res/responseTimeMs")
	}
	if _, has := gcp["logging.googleapis.com/labels"]; has {
		t.Fatalf("expected no labels without ids")
	}
}
