package gsync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// ---------- small JSON helpers ----------

func gsStr(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func gsFloat(m map[string]any, k string) float64 {
	v, _ := m[k].(float64)
	return v
}

// gsBool — truthy decode.
func gsBool(m map[string]any, k string) bool {
	v, _ := m[k].(bool)
	return v
}

// gsBody — read the request JSON (Node express.json limit 50mb).
func gsBody(r *http.Request, maxBytes int64) (map[string]any, error) {
	if maxBytes <= 0 {
		maxBytes = 50 << 20
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// gsResErr — write a gsync error in the Node controller shape:
//   - 401 InvalidTokenError -> res.status(401).json({detail})
//   - 404 not linked        -> res.status(404).json({detail})
//   - 500 bridge            -> res.status(500).json({detail?, error})
//   - 400 validation        -> res.status(400).json({message})
func gsResErr(res *core.Res, err error) {
	var ge *gsError
	if ok := asGSerr(err, &ge); ok {
		switch ge.Status {
		case 400:
			res.JSON(400, []byte(`{"message":`+gsJSONStr(ge.Message)+`}`))
			return
		case 401:
			res.JSON(401, []byte(`{"detail":`+gsJSONStr(ge.Message)+`}`))
			return
		case 404:
			res.JSON(404, []byte(`{"detail":`+gsJSONStr(ge.Message)+`}`))
			return
		default:
			d := ge.Detail
			if d == "" {
				d = ge.Message
			}
			res.JSON(500, []byte(`{"detail":`+gsJSONStr(d)+`,"error":`+gsJSONStr(ge.Message)+`}`))
			return
		}
	}
	// generic
	res.JSON(500, []byte(`{"error":"`+strings.ReplaceAll(fmt.Sprint(err), `"`, `\"`)+`"}`))
}

func asGSerr(err error, target **gsError) bool {
	if ge, ok := err.(*gsError); ok {
		*target = ge
		return true
	}
	return false
}

func gsJSONStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var (
	_ = strconv.Itoa
	_ = http.MethodGet
	_ = time.Now
	_ = core.App{}
)
