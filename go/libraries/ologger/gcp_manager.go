package ologger

import (
	"strconv"

	"ollitex/go/libraries/oerror"
)

// entryFieldsToOmit mirrors ENTRY_FIELDS_TO_OMIT in gcp-manager.js: the bunyan
// core fields omitted when converting a log entry for GCP (they are either not
// interesting, have a special GCP meaning, or are processed separately).
var entryFieldsToOmit = []string{
	"level", "name", "hostname", "v", "pid", "msg", "err", "error", "req", "res",
}

// ConvertLogEntry mirrors GCPManager.convertLogEntry: converts a bunyan log
// entry to the shape the GKE/GCP log sink expects (severity, message/stack,
// httpRequest, labels). `entry` is the serializer-applied entry; its `req`/
// `res` (if any) are the serializer's plain-object shapes and `err`/`error`
// may be a map or an error.
func ConvertLogEntry(entry Entry) Entry {
	gcp := omit(entry, entryFieldsToOmit)

	// Error information (in GCP the stack trace goes in `message`).
	if err, ok := firstPresent(entry, "err", "error"); ok {
		var m map[string]any
		switch e := err.(type) {
		case map[string]any:
			m = e
		case error:
			m = map[string]any{
				"message": e.Error(),
				"stack":   oerror.GetFullStack(e),
			}
		}
		if m != nil {
			if info, ok := m["info"].(map[string]any); ok {
				for k, v := range info {
					gcp[k] = v
				}
			}
			if c, ok := m["code"]; ok {
				gcp["code"] = c
			}
			if s, ok := m["signal"]; ok {
				gcp["signal"] = s
			}
			stack, _ := m["stack"].(string)
			if stack != "" && stack != "(no stack)" {
				gcp["message"] = stack
			} else if msg, ok := m["message"].(string); ok {
				gcp["message"] = msg
			}
			if name, ok := entry["name"].(string); ok && name != "" {
				gcp["serviceContext"] = map[string]any{"service": name}
			}
		}
	}

	// Log message.
	if msg, ok := entry["msg"]; ok && msg != "" {
		if existing, ok := gcp["message"]; ok && existing != "" {
			gcp["msg"] = entry["msg"]
		} else {
			gcp["message"] = entry["msg"]
		}
	}

	// Severity.
	if level, ok := entry["level"].(int); ok {
		if name := nameFromLevel(level); name != "" {
			gcp["severity"] = name
		}
	}

	// HTTP request information.
	if req, res, rt := entry["req"], entry["res"], entry["responseTimeMs"]; req != nil || res != nil || rt != nil {
		httpRequest := map[string]any{}
		if rm, ok := req.(map[string]any); ok {
			httpRequest["requestMethod"] = rm["method"]
			httpRequest["requestUrl"] = rm["url"]
			httpRequest["remoteIp"] = rm["remoteAddress"]
			if h, ok := rm["headers"].(map[string]any); ok {
				if cl, ok := h["content-length"]; ok && cl != "" {
					httpRequest["requestSize"] = toInt(cl)
				}
				if ua, ok := h["user-agent"]; ok {
					httpRequest["userAgent"] = ua
				}
				if ref, ok := h["referer"]; ok {
					httpRequest["referer"] = ref
				}
			}
		}
		if sm, ok := res.(map[string]any); ok {
			if sc, ok := sm["statusCode"]; ok {
				httpRequest["status"] = sc
			}
			if h, ok := sm["headers"].(map[string]any); ok {
				if cl, ok := h["content-length"]; ok && cl != "" {
					httpRequest["responseSize"] = toInt(cl)
				}
			}
		}
		if rt != nil {
			if ms, ok := toFloat(rt); ok {
				sec := ms / 1000
				// `${responseTimeSec}s` - mirror the JS string interpolation.
				httpRequest["latency"] = floatString(sec) + "s"
			}
		}
		gcp["httpRequest"] = httpRequest
	}

	// Labels (indexed in GCP for fast filtering).
	proj := labelId(gcp, "projectId", "project_id", "req", "projectId", entry)
	user := labelId(gcp, "userId", "user_id", "req", "userId", entry)
	doc := labelId(gcp, "docId", "doc_id", "req", "docId", entry)
	if proj != nil || user != nil || doc != nil {
		labels := map[string]any{}
		if proj != nil {
			labels["projectId"] = proj
		}
		if user != nil {
			labels["userId"] = user
		}
		if doc != nil {
			labels["docId"] = doc
		}
		gcp["logging.googleapis.com/labels"] = labels
	}

	return gcp
}

// labelId mirrors `a || b || (entry.req && entry.req.key)` - first truthy.
func labelId(gcp Entry, primary, alt string, reqKey, reqField string, entry Entry) any {
	if v, ok := gcp[primary]; ok && v != "" {
		return v
	}
	if v, ok := gcp[alt]; ok && v != "" {
		return v
	}
	if rm, ok := entry[reqKey].(map[string]any); ok {
		if v, ok := rm[reqField]; ok && v != "" {
			return v
		}
	}
	return nil
}

func omit(src Entry, excluded []string) Entry {
	out := Entry{}
	kill := map[string]bool{}
	for _, e := range excluded {
		kill[e] = true
	}
	for k, v := range src {
		if !kill[k] {
			out[k] = v
		}
	}
	return out
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case string:
		return parseInt10(n)
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// floatString mirrors JS number-to-string for the latency (e.g. 1.5 -> "1.5",
// 2 -> "2").
func floatString(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
