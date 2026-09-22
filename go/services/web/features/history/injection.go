package history

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"ollitex/go/services/web/core"
)

// injectUserDetails mirrors HistoryManager.injectUserDetails:
// for entry in data.updates with meta.users: each entry that is a string
// (overleaf id) or number (v1 id) is replaced by the _userView object
// {first_name, last_name, email, id}; unresolvable -> undefined (-> null
// in the JSON array). Objects pass through untouched.
//
// Node JSON.stringify drops nothing but serializes undefined array slots as
// null — pinned by the live oracle (users with/without a matching user doc).
func injectUserDetails(a *core.App, cxt *core.Cxt, body []byte) ([]byte, error) {
	var top opair
	if err := top.UnmarshalJSON(body); err != nil {
		return body, nil // not an object (pass through; Node would still res.json)
	}
	updRaw := top.get("updates")
	if updRaw == nil {
		return body, nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(updRaw, &entries) != nil {
		return body, nil
	}

	// --- collect ids ---
	stringIDs := map[string]bool{}
	numericIDs := []int64{}
	type metaSlot struct {
		metaRaw json.RawMessage
		users   []any
	}
	var slots []metaSlot
	for _, e := range entries {
		en := &opair{}
		if en.UnmarshalJSON(e) != nil {
			continue
		}
		metaRaw := en.get("meta")
		if metaRaw == nil {
			continue
		}
		meta := &opair{}
		if meta.UnmarshalJSON(metaRaw) != nil {
			continue
		}
		usersRaw := meta.get("users")
		if usersRaw == nil {
			continue
		}
		var arr []any
		if json.Unmarshal(usersRaw, &arr) != nil {
			continue
		}
		for _, u := range arr {
			switch t := u.(type) {
			case string:
				stringIDs[t] = true
			case float64:
				numericIDs = append(numericIDs, int64(t))
			}
		}
		slots = append(slots, metaSlot{metaRaw: metaRaw, users: arr})
	}
	users := loadUsersByIDs(a, cxt, keysOf(stringIDs))
	v1users := loadUsersByV1IDs(a, cxt, numericIDs)

	// --- rebuild ---
	outEntries := make([]json.RawMessage, 0, len(entries))
	slotIdx := 0
	for _, e := range entries {
		en := &opair{}
		if en.UnmarshalJSON(e) != nil {
			outEntries = append(outEntries, e)
			continue
		}
		metaRaw := en.get("meta")
		if metaRaw == nil {
			ob, _ := json.Marshal(en)
			outEntries = append(outEntries, ob)
			continue
		}
		meta := &opair{}
		if meta.UnmarshalJSON(metaRaw) != nil {
			ob, _ := json.Marshal(en)
			outEntries = append(outEntries, ob)
			continue
		}
		usersRaw := meta.get("users")
		if usersRaw == nil {
			ob, _ := json.Marshal(en)
			outEntries = append(outEntries, ob)
			continue
		}
		var arr []any
		if json.Unmarshal(usersRaw, &arr) != nil {
			ob, _ := json.Marshal(en)
			outEntries = append(outEntries, ob)
			continue
		}
		if slotIdx < len(slots) {
			s := slots[slotIdx]
			_ = s
		}
		slotIdx++
		newUsers := make([]any, 0, len(arr))
		for _, u := range arr {
			switch t := u.(type) {
			case string:
				if uv, ok := users[t]; ok {
					newUsers = append(newUsers, json.RawMessage(userViewJSON(uv)))
				} else {
					newUsers = append(newUsers, nil) // undefined -> null
				}
			case float64:
				key := int64(t)
				if uv, ok := v1users[key]; ok {
					newUsers = append(newUsers, json.RawMessage(userViewJSON(uv)))
				} else {
					newUsers = append(newUsers, nil)
				}
			default:
				newUsers = append(newUsers, u)
			}
		}
		ub, _ := json.Marshal(newUsers)
		meta.set("users", ub)
		mb, _ := json.Marshal(meta)
		en.set("meta", mb)
		ob, _ := json.Marshal(en)
		outEntries = append(outEntries, ob)
	}
	mb, _ := json.Marshal(outEntries)
	top.set("updates", mb)
	out, _ := json.Marshal(top)
	return out, nil
}

// userViewJSON: _userView key order { first_name, last_name, email, id }.
func userViewJSON(u userView) string {
	var b bytes.Buffer
	b.WriteString(`{"first_name":`)
	writeJSONString(&b, u.FirstName)
	b.WriteString(`,"last_name":`)
	writeJSONString(&b, u.LastName)
	b.WriteString(`,"email":`)
	writeJSONString(&b, u.Email)
	b.WriteString(`,"id":`)
	writeJSONString(&b, u.ID)
	b.WriteByte('}')
	return b.String()
}

func writeJSONString(b *bytes.Buffer, s string) {
	q, _ := json.Marshal(s)
	b.Write(q)
}

// enrichLabels mirrors the controller's _enrichLabels: append
// user_display_name per label ("Anonymous" without a resolvable user).
func enrichLabels(a *core.App, cxt *core.Cxt, labelsRaw []byte) ([]byte, error) {
	var ids []json.RawMessage
	if json.Unmarshal(labelsRaw, &ids) != nil {
		return nil, errNotArray
	}
	if len(ids) == 0 {
		return []byte("[]"), nil
	}
	type labelT struct {
		pair *opair
		id   string
	}
	var labels []labelT
	idSet := map[string]bool{}
	allAnon := true
	for _, e := range ids {
		o := &opair{}
		if o.UnmarshalJSON(e) != nil {
			return nil, errNotArray
		}
		lt := labelT{pair: o}
		if raw := o.get("user_id"); raw != nil {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				lt.id = s
				allAnon = false
			}
		}
		labels = append(labels, lt)
	}
	if !allAnon {
		ids := make([]string, 0, len(labels))
		for _, l := range labels {
			if l.id != "" {
				idSet[l.id] = true
			}
		}
		for id := range idSet {
			ids = append(ids, id)
		}
		uvs := loadUsersByIDs(a, cxt, ids)
		for i := range labels {
			l := &labels[i]
			display := "Anonymous"
			if l.id != "" {
				if uv, ok := uvs[l.id]; ok {
					display = displayNameFrom(uv)
				}
			}
			dv, _ := json.Marshal(display)
			l.pair.set("user_display_name", dv)
		}
	} else {
		// Node: !uniqueUsers.size -> return labels UNCHANGED (no key added).
		return labelsRaw, nil
	}
	out := make([]json.RawMessage, 0, len(labels))
	for _, l := range labels {
		b, _ := json.Marshal(l.pair)
		out = append(out, b)
	}
	res, _ := json.Marshal(out)
	return res, nil
}

func displayNameFrom(u userView) string {
	n := strings.TrimSpace(u.FirstName + maybeMid(u.LastName))
	if n == "" {
		if u.Email != "" {
			if i := strings.Index(u.Email, "@"); i > 0 {
				n = u.Email[:i]
			}
		}
	}
	if n == "" {
		return "?"
	}
	return n
}

func maybeMid(last string) string {
	if last == "" {
		return ""
	}
	return " " + last
}

var _ = strconv.Itoa
