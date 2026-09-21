package otc

import (
	"regexp"
	"time"
)

// projectVersionRx is Change.PROJECT_VERSION_RX (Node: `^[0-9]+\.[0-9]+$`).
var projectVersionRx = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// Change mirrors lib/change.js: a list of Operations applied atomically by
// given Author(s) at a given time.
type Change struct {
	Operations     []Operation
	Timestamp      time.Time
	Authors        []any // v1 author ids (string | null)
	Origin         OriginIface
	V2Authors      []string
	ProjectVersion *string
	V2DocVersions  *V2DocVersions
}

// NewChange mirrors the Change constructor.
func NewChange(operations []Operation, ts time.Time, authors []any, origin OriginIface, v2Authors []string, projectVersion *string, v2 *V2DocVersions) *Change {
	if authors == nil {
		authors = []any{}
	}
	if v2Authors == nil {
		v2Authors = []string{}
	}
	return &Change{
		Operations:     operations,
		Timestamp:      ts,
		Authors:        authors,
		Origin:         origin,
		V2Authors:      v2Authors,
		ProjectVersion: projectVersion,
		V2DocVersions:  v2,
	}
}

// ChangeFromRaw mirrors Change.fromRaw (nil → nil).
func ChangeFromRaw(raw map[string]any) (*Change, error) {
	if raw == nil {
		return nil, nil
	}
	return changeMustFromRaw(raw)
}

func changeMustFromRaw(raw map[string]any) (*Change, error) {
	opsRaw, ok := raw["operations"].([]any)
	if !ok {
		return nil, gop("bad raw.operations")
	}
	timestamp, ok := raw["timestamp"].(string)
	if !ok || timestamp == "" {
		return nil, gop("bad raw.timestamp")
	}
	ts, err := parseRawTime(timestamp)
	if err != nil {
		return nil, err
	}

	timestampParsed := ts

	// authors with the 0→null cleanup (Node hasOwnProperty / bad-data hack)
	var authors []any
	if v, ok := raw["authors"]; ok {
		authorArr, _ := v.([]any)
		for _, a := range authorArr {
			if n, isNum := a.(int); isNum && n == 0 {
				authors = append(authors, nil)
			} else if f, isFlt := a.(float64); isFlt && f == 0 {
				authors = append(authors, nil)
			} else {
				authors = append(authors, a)
			}
		}
	}

	var ops []Operation
	for _, o := range opsRaw {
		om, okM := o.(map[string]any)
		if !okM {
			return nil, gop("bad raw.operations element")
		}
		op, err := OperationFromRaw(om)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}

	var origin OriginIface
	if v, ok := raw["origin"].(map[string]any); ok {
		o, err := OriginFromRaw(v)
		if err != nil {
			return nil, err
		}
		origin = o
	}

	var v2Authors []string
	if v, ok := raw["v2Authors"].([]any); ok {
		for _, x := range v {
			if s, okS := x.(string); okS {
				v2Authors = append(v2Authors, s)
			}
		}
	}

	var pv *string
	if s, ok := raw["projectVersion"].(string); ok && s != "" {
		pv = &s
	}

	var v2 *V2DocVersions
	if m, ok := raw["v2DocVersions"].(map[string]any); ok {
		v2 = V2DocVersionsFromRaw(m)
	}

	_ = timestampParsed
	return NewChange(ops, ts, authors, origin, v2Authors, pv, v2), nil
}

// ToRaw serialises a Change (Node: `toRaw`).
func (c *Change) ToRaw() map[string]any {
	opsRaw := make([]any, 0, len(c.Operations))
	for _, op := range c.Operations {
		opsRaw = append(opsRaw, op.ToRaw())
	}
	raw := map[string]any{
		"operations": opsRaw,
		"timestamp":  toISOString(c.Timestamp),
		"authors":    c.Authors,
	}
	if c.V2Authors != nil {
		raw["v2Authors"] = c.V2Authors
	}
	if c.Origin != nil {
		raw["origin"] = c.Origin.ToRaw()
	}
	if c.ProjectVersion != nil {
		raw["projectVersion"] = *c.ProjectVersion
	}
	if c.V2DocVersions != nil {
		raw["v2DocVersions"] = c.V2DocVersions.ToRaw()
	}
	return raw
}

func (c *Change) GetOperations() []Operation { return c.Operations }
func (c *Change) SetOperations(ops []Operation) {
	if ops == nil {
		panic(newTypeError("Change: bad operations"))
	}
	c.Operations = ops
}
func (c *Change) GetTimestamp() time.Time          { return c.Timestamp }
func (c *Change) GetAuthors() []any                { return c.Authors }
func (c *Change) GetV2Authors() []string           { return c.V2Authors }
func (c *Change) GetOrigin() OriginIface           { return c.Origin }
func (c *Change) GetProjectVersion() *string       { return c.ProjectVersion }
func (c *Change) GetV2DocVersions() *V2DocVersions { return c.V2DocVersions }

// FindBlobHashes (Node: `findBlobHashes`).
func (c *Change) FindBlobHashes(hashes map[string]bool) {
	for _, op := range c.Operations {
		op.FindBlobHashes(hashes)
	}
}

// PushOperation appends an operation (Node: `pushOperation`).
func (c *Change) PushOperation(op Operation) *Change {
	c.Operations = append(c.Operations, op)
	return c
}

// ApplyTo applies the change to a snapshot and bumps its version (Node:
// `iterativelyApplyTo`/`applyTo`). Recoverable errors are ignored unless
// strict.
func (c *Change) ApplyTo(s *Snapshot, strict bool) error {
	for _, op := range c.Operations {
		if err := op.ApplyTo(s); err != nil {
			if !isRecoverable(err) || strict {
				return err
			}
		}
	}
	if c.ProjectVersion != nil {
		s.SetProjectVersion(c.ProjectVersion)
	}
	if c.V2DocVersions != nil {
		s.UpdateV2DocVersions(c.V2DocVersions)
	}
	ts := c.Timestamp
	s.SetTimestamp(&ts)
	return nil
}

func isRecoverable(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*EditMissingFileError); ok {
		return true
	}
	if _, ok := err.(*FileNotFoundError); ok {
		return true
	}
	return false
}

// TransformAfter rewrites this change's operations against a concurrently
// applied change (Node: `transformAfter`).
func (c *Change) TransformAfter(other *Change) {
	thisOps := c.Operations
	otherOps := other.Operations
	for i := range otherOps {
		for j := range thisOps {
			primes := OperationTransform(thisOps[j], otherOps[i])
			thisOps[j] = primes[0]
		}
	}
}

// Clone returns a deep clone (Node: `clone`).
func (c *Change) Clone() (*Change, error) {
	return ChangeFromRaw(c.ToRaw())
}

// Store stores each operation and returns the raw change (Node: `store`).
func (c *Change) Store(bs BlobStore) (map[string]any, error) {
	raw := c.ToRaw()
	// authors dedupe
	raw["authors"] = dedupeAuthors(c.Authors)
	opsRaw := make([]any, 0, len(c.Operations))
	for _, op := range c.Operations {
		oraw, err := op.Store(bs)
		if err != nil {
			return nil, err
		}
		opsRaw = append(opsRaw, oraw)
	}
	raw["operations"] = opsRaw
	return raw, nil
}

func dedupeAuthors(authors []any) []any {
	seen := map[any]bool{}
	out := make([]any, 0, len(authors))
	for _, a := range authors {
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// CanBeComposedWith (Node: `canBeComposedWith`).
func (c *Change) CanBeComposedWith(other *Change) bool {
	if len(c.Operations) > 1 || len(other.Operations) > 1 {
		return false
	}
	return c.Operations[0].CanBeComposedWith(other.Operations[0])
}
