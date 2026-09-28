package opmodel

// UTF-16 unit handling.
//
// The engine (JS) counts lengths in UTF-16 code units: a supplementary code
// point (U+10000 and up, e.g. an emoji) is 2 units (a surrogate pair);
// everything else is 1. Go strings are UTF-8, so every length in this package
// must use these helpers instead of len(string).
//
// Well-formed content (what JSON-decoded document content is) is processed
// exactly: each rune is re-encoded to its UTF-16 units and back. A lone
// surrogate byte sequence survives the trip: encoding/json maps an unpaired
// \uD800 escape to U+FFFD (same 1 unit as the vendor's lone surrogate), and
// raw lone surrogate bytes (ED A0 80 …) decode to the surrogate rune itself,
// which is 1 unit.

// Units — number of UTF-16 code units in s (JS: s.length).
func Units(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// utf16Decode — s → []uint16 of UTF-16 code units (supplementary as pairs).
// A surrogate rune (possible in Go strings decoded from lone surrogate
// bytes) maps to itself.
func utf16Decode(s string) []uint16 {
	units := make([]uint16, 0, Units(s))
	for _, r := range s {
		switch {
		case r > 0xFFFF:
			cr := uint32(r) - 0x10000
			units = append(units,
				uint16(0xD800+(cr>>10)),
				uint16(0xDC00+(cr&0x3FF)),
			)
		default:
			units = append(units, uint16(r))
		}
	}
	return units
}

// utf16Encode — []uint16 → Go string (UTF-8). Lead + low surrogate pair →
// one supplementary rune; a lone unit maps to its rune value.
func utf16Encode(units []uint16) string {
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) {
			high := uint32(u) - 0xD800
			low := uint32(units[i+1]) - 0xDC00
			runes = append(runes, rune(0x10000+high<<10+low))
			i++
		} else {
			runes = append(runes, rune(u))
		}
	}
	return string(runes)
}
