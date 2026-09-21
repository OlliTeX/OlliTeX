package ologger

import (
	"errors"
	"testing"
)

func TestReqSerializer_FalsyUntouched(t *testing.T) {
	if got := ReqSerializer(nil); got != nil {
		t.Fatalf("expected falsy req returned untouched, got %#v", got)
	}
}

func TestReqSerializer_PassthroughNonReq(t *testing.T) {
	v := "not-a-req"
	if got := ReqSerializer(v); got != v {
		t.Fatalf("expected non-Req value untouched, got %#v", got)
	}
}

func TestReqSerializer_ExtractsMethodUrlHeadersRemote(t *testing.T) {
	req := Req{
		Method:      "GET",
		OriginalURL: "/project/123",
		IP:          "127.0.0.1",
		Headers: map[string]string{
			"referer":        "https://example.com",
			"user-agent":     "test-agent",
			"content-length": "42",
		},
		Params: map[string]any{},
	}
	entry, ok := ReqSerializer(req).(map[string]any)
	if !ok {
		t.Fatalf("expected a map entry, got %#v", ReqSerializer(req))
	}
	if entry["method"] != "GET" {
		t.Fatalf("expected method GET, got %v", entry["method"])
	}
	if entry["url"] != "/project/123" {
		t.Fatalf("expected url originalUrl, got %v", entry["url"])
	}
	if entry["remoteAddress"] != "127.0.0.1" {
		t.Fatalf("expected remoteAddress ip, got %v", entry["remoteAddress"])
	}
	h, _ := entry["headers"].(map[string]any)
	if h["referer"] != "https://example.com" || h["user-agent"] != "test-agent" || h["content-length"] != "42" {
		t.Fatalf("expected headers copied, got %#v", h)
	}
	// No recognised params -> no id keys.
	for _, k := range []string{"projectId", "userId", "docId"} {
		if _, has := entry[k]; has {
			t.Fatalf("expected no %q for empty params", k)
		}
	}
}

func TestReqSerializer_ExtractsSnakeCaseIds(t *testing.T) {
	req := Req{
		Method:  "GET",
		URL:     "/",
		Headers: map[string]string{},
		Params: map[string]any{
			"project_id": "project-1",
			"user_id":    "user-1",
			"doc_id":     "doc-1",
		},
	}
	entry := ReqSerializer(req).(map[string]any)
	if entry["projectId"] != "project-1" {
		t.Fatalf("expected projectId project-1, got %v", entry["projectId"])
	}
	if entry["userId"] != "user-1" {
		t.Fatalf("expected userId user-1, got %v", entry["userId"])
	}
	if entry["docId"] != "doc-1" {
		t.Fatalf("expected docId doc-1, got %v", entry["docId"])
	}
}

func TestReqSerializer_PrefersCamelCase(t *testing.T) {
	req := Req{
		Method:  "GET",
		URL:     "/",
		Headers: map[string]string{},
		Params: map[string]any{
			"projectId":  "camel-project",
			"project_id": "snake-project",
			"userId":     "camel-user",
			"user_id":    "snake-user",
			"docId":      "camel-doc",
			"doc_id":     "snake-doc",
		},
	}
	entry := ReqSerializer(req).(map[string]any)
	if entry["projectId"] != "camel-project" || entry["userId"] != "camel-user" || entry["docId"] != "camel-doc" {
		t.Fatalf("expected camelCase ids to win, got %#v", entry)
	}
}

func TestReqSerializer_OmitsUnrecognisedParams(t *testing.T) {
	req := Req{Method: "GET", URL: "/", Headers: map[string]string{}, Params: map[string]any{"other": 1}}
	entry := ReqSerializer(req).(map[string]any)
	for _, k := range []string{"projectId", "userId", "docId"} {
		if _, has := entry[k]; has {
			t.Fatalf("expected %q omitted, got %v", k, entry[k])
		}
	}
}

func TestReqSerializer_LockdownUsesRawParamsOnly(t *testing.T) {
	// Node: under REQ_LOCKDOWN_MODE the public `params` getter throws; the raw
	// symbol accessor is the only safe source. Go: LockdownInstalled means
	// Params is poison and MUST NOT be read - ids come from RawParams.
	req := Req{
		Method:            "GET",
		URL:               "/project/123",
		Headers:           map[string]string{},
		Params:            map[string]any{"projectId": "WRONG-POISON"}, // must not be read
		RawParams:         map[string]any{"project_id": "project-1", "user_id": "user-1", "doc_id": "doc-1"},
		LockdownInstalled: true,
	}
	entry := ReqSerializer(req).(map[string]any)
	if entry["projectId"] != "project-1" {
		t.Fatalf("expected projectId from raw params, got %v", entry["projectId"])
	}
	if entry["userId"] != "user-1" || entry["docId"] != "doc-1" {
		t.Fatalf("expected user/doc ids from raw params, got %#v", entry)
	}
}

func TestReqSerializer_RemoteAddressXForwarded(t *testing.T) {
	req := Req{
		Method:  "GET",
		URL:     "/",
		IP:      "10.0.0.1",
		Headers: map[string]string{"x-forwarded-from": "203.0.113.5, 10.0.0.1"},
		Params:  map[string]any{},
	}
	entry := ReqSerializer(req).(map[string]any)
	if entry["remoteAddress"] != "203.0.113.5" {
		t.Fatalf("expected first x-forwarded-from segment, got %v", entry["remoteAddress"])
	}
}

func TestErrSerializer_Nil(t *testing.T) {
	got := ErrSerializer(nil).(map[string]any)
	if len(got) != 0 {
		t.Fatalf("expected empty for nil error, got %#v", got)
	}
}

func TestErrSerializer_Error(t *testing.T) {
	e := errors.New("kaboom")
	got := ErrSerializer(e).(map[string]any)
	if got["message"] != "kaboom" {
		t.Fatalf("expected message kaboom, got %v", got["message"])
	}
	if got["stack"] != "kaboom" {
		t.Fatalf("expected stack for a plain error, got %v", got["stack"])
	}
}

func TestErrSerializer_Map(t *testing.T) {
	got := ErrSerializer(map[string]any{"message": "m", "stack": "s"}).(map[string]any)
	if got["message"] != "m" || got["stack"] != "s" {
		t.Fatalf("expected message/stack copied, got %#v", got)
	}
}

func TestResSerializer_Empty(t *testing.T) {
	got := ResSerializer(map[string]any{"anything": true}).(map[string]any)
	if len(got) != 0 {
		t.Fatalf("expected res serializer to return an empty object, got %#v", got)
	}
}
