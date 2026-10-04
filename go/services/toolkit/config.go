package toolkit

import (
	"fmt"
	"strconv"

	"ollitex/go/libraries/configschema"
)

// Settings is the single-source-of-truth view over the config store:
// every key the /hub admin shows (configschema registry) plus the stack
// boot plane (the default.rc settings /hub historically could not handle —
// mongo/redis/postgres/seaweed/nginx/languagetool toggles and ports) is
// read and written through the configstore Store. There is deliberately
// NO docker-env fallback in this plane.
type Settings struct {
	tk *Toolkit
}

func newSettings(t *Toolkit) *Settings { return &Settings{tk: t} }

// Group is one rendered section (registry group + its keys in order).
type Group struct {
	Name  string
	Keys  []Entry
	Sort  int
}

// Entry is one setting row.
type Entry struct {
	Key         string
	Kind        configschema.Kind
	Secret      bool
	Group       string
	Default     string
	Description string
	Value       string // "" = unset (registry default applies)
	Present     bool
}

// Groups renders every registry group with its entries (operator order:
// the registry is already sorted by group then key).
func (s *Settings) Groups() ([]Group, error) {
	names := configschema.Groups()
	var out []Group
	for _, g := range names {
		var gs Group
		gs.Name = g
		for _, p := range configschema.ByGroup(g) {
			e := Entry{
				Key:         p.Key,
				Kind:        p.Kind,
				Secret:      p.Secret,
				Group:       p.Group,
				Default:     p.Default,
				Description: p.Description,
			}
			if s.tk.Store != nil {
				v, err := s.tk.Store.Get(p.Key)
				if err == nil {
					e.Value = v
					e.Present = true
				} else if e.Secret {
					e.Value = "" // masked unless present-and-revealed
				}
			}
			gs.Keys = append(gs.Keys, e)
		}
		out = append(out, gs)
	}
	return out, nil
}

// Get returns a single key's value ("" + false when absent).
func (s *Settings) Get(key string) (string, bool, error) {
	if s.tk.Store == nil {
		return "", false, nil
	}
	v, err := s.tk.Store.Get(key)
	if err != nil {
		if err.Error() == "missing key" {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

// Set validates the value against the registry kind and upserts it.
func (s *Settings) Set(key, value, source string) error {
	p, ok := configschema.Find(key)
	if !ok {
		return fmt.Errorf("%q is not a registered setting key (unknown key — the registry is the key space)", key)
	}
	switch p.Kind {
	case configschema.KBool:
		if value == "true" || value == "false" {
			// ok
		} else {
			var ok bool
			if _, err := strconv.ParseBool(value); err != nil || !ok {
				return fmt.Errorf("%s is bool — value %q is not bool", key, value)
			}
		}
	case configschema.KInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%s is int — value %q is not an integer: %v", key, value, err)
		}
	}
	return s.tk.Store.Set(key, value, source)
}

// Delete removes a key (idempotent store call).
func (s *Settings) Delete(key, source string) error {
	return s.tk.Store.Delete(key)
}

// Reveal returns the stored value of a secret key for the editor (only
// called after the operator explicitly reveals).
func (s *Settings) Reveal(key string) (string, bool, error) { return s.Get(key) }
