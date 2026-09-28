package historyot

import (
	"errors"
	"time"
)

// Change — vendor `Change` (lib/change.js), scoped to what the B-phase
// (UpdateTranslator + OperationsCompressor) and C-phase need: raw<->wire,
// the operation list, authors + v2Authors, origin, projectVersion,
// v2DocVersions.
//
// Vendor `new Change(operations, timestamp, authors, origin, v2Authors,
// projectVersion, v2DocVersions)`.
type Change struct {
	Operations     []Operation
	Ts             time.Time
	Authors        []any
	Origin         any
	V2Authors      []any
	ProjectVersion any
	V2DocVersions  *V2DocVersions
}

// V2DocVersions — vendor `V2DocVersions` (lib/v2_doc_versions.js): a map
// from doc id to {pathname, v} (+ metadata on the wire).
type V2DocVersions struct {
	Data map[string]any
}

// NewChange — vendor `new Change(...)`.
func NewChange(operations []Operation, ts time.Time, authors []any,
	origin any, v2Authors []any, projectVersion any, v2DocVersions *V2DocVersions) *Change {
	return &Change{
		Operations:     operations,
		Ts:             ts,
		Authors:        authors,
		Origin:         origin,
		V2Authors:      v2Authors,
		ProjectVersion: projectVersion,
		V2DocVersions:  v2DocVersions,
	}
}

// ChangeFromRaw — vendor `Change.fromRaw(raw)` (via mustFromRaw):
//
//	operations: required array of raw operations
//	timestamp:  required non-empty wire ISO string
//	authors:    (raw value) array; numeric 0 maps to `null` (historical
//	           bad-data clean-up, vendor keeps this)
//	origin / v2Authors / projectVersion / v2DocVersions: passthrough
func ChangeFromRaw(raw map[string]any) (*Change, error) {
	opsRawAny, ok := raw["operations"].([]any)
	if !ok {
		return nil, errors.New("bad raw.operations")
	}
	tsStr, ok := raw["timestamp"].(string)
	if !ok || tsStr == "" {
		return nil, errors.New("bad raw.timestamp")
	}
	ts, err := decodeWireTime(raw["timestamp"])
	if err != nil {
		return nil, err
	}

	var authors []any
	if a, has := raw["authors"]; has {
		if s, ok := a.([]any); ok {
			authors = make([]any, len(s))
			for i, av := range s {
				// Historical hack: numeric 0 author id -> null (see vendor).
				if n, isNum := asFloat64any(av); isNum && n == 0 {
					authors[i] = nil
				} else {
					authors[i] = av
				}
			}
		}
	}

	operations := make([]Operation, 0, len(opsRawAny))
	for _, oRaw := range opsRawAny {
		m, ok := oRaw.(map[string]any)
		if !ok {
			return nil, errors.New("bad raw change operation")
		}
		op, err := OperationFromRaw(m)
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}

	var origin any
	if o, has := raw["origin"]; has {
		if m, ok := o.(map[string]any); ok {
			origin, err = OriginFromRaw(m)
			if err != nil {
				return nil, err
			}
		}
	}

	var v2DocVersions *V2DocVersions
	if v, has := raw["v2DocVersions"]; has {
		if m, ok := v.(map[string]any); ok {
			v2DocVersions = &V2DocVersions{Data: shallowCopyMap(m)}
		}
	}

	var v2Authors []any
	if a, has := raw["v2Authors"]; has {
		if s, ok := a.([]any); ok {
			v2Authors = s
		}
	}

	var projectVersion any
	if pv, has := raw["projectVersion"]; has {
		projectVersion = pv
	}

	return NewChange(operations, ts, authors, origin, v2Authors, projectVersion, v2DocVersions), nil
}

// ToRaw — vendor `toRaw()`:
//
//	{
//	  operations: [...] (op.toRaw()),
//	  timestamp:  toISOString(),
//	  authors:    [...],
//	  v2Authors?, origin?, projectVersion?, v2DocVersions?  // optional
//	}
//
// Vendor conditionals: `if (this.v2Authors)`, `if (this.origin)`,
// `if (this.projectVersion)`, `if (this.v2DocVersions)` — JS truthiness.
func (c *Change) ToRaw() map[string]any {
	ops := make([]any, 0, len(c.Operations))
	for _, op := range c.Operations {
		ops = append(ops, op.ToRaw())
	}
	raw := map[string]any{
		"operations": ops,
		"timestamp":  wireTime(c.Ts),
	}
	if c.Authors != nil {
		raw["authors"] = c.Authors
	} else {
		raw["authors"] = []any{}
	}
	if c.V2Authors != nil {
		raw["v2Authors"] = c.V2Authors
	}
	// Vendor: `if (this.origin) raw.origin = this.origin.toRaw()` — the
	// wire map, not the typed struct (fix found in B9).
	if o := c.OriginRaw(); o != nil {
		raw["origin"] = o
	}
	// Vendor: `if (this.projectVersion)` — JS truthiness (0/""/null/undefined
	// falsy). The wire value is a number, so 0 is dropped.
	if pv := jsTruthyAny(c.ProjectVersion); pv {
		raw["projectVersion"] = c.ProjectVersion
	}
	if c.V2DocVersions != nil {
		raw["v2DocVersions"] = c.V2DocVersions.Data
	}
	return raw
}

// OriginRaw — the wire origin map (for tests/ports emitting raw).
func (c *Change) OriginRaw() map[string]any {
	if c.Origin == nil {
		return nil
	}
	switch o := c.Origin.(type) {
	case *Origin:
		return o.ToWire()
	case *RestoreOrigin:
		return o.ToWire()
	case *RestoreFileOrigin:
		return o.ToWire()
	case *RestoreProjectOrigin:
		return o.ToWire()
	}
	return nil
}

// PushOperation — vendor `pushOperation(operation)`.
func (c *Change) PushOperation(op Operation) *Change {
	c.Operations = append(c.Operations, op)
	return c
}

// FindBlobHashes — vendor `findBlobHashes(blobHashes)`: aggregate across
// operations (nil-safe: no-op ops contribute nothing).
func (c *Change) FindBlobHashes(blobHashes map[string]struct{}) {
	for _, op := range c.Operations {
		op.FindBlobHashes(blobHashes)
	}
}

// jsTruthyAny — vendor JS truthiness for the wire values used by Change.toRaw
// (numeric/string). Go `0` and `""` are falsy, like JS.
func jsTruthyAny(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	case bool:
		return x
	default:
		return true
	}
}

func asFloat64any(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
