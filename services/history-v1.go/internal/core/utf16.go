package core

// UTF-16 helpers. Node string length and Range/TextOperation lengths count
// UTF-16 code units; Go strings are UTF-8. The engine tracks lengths in
// UTF-16 units throughout. A supplementary code point (U+10000 and up, e.g.
// emoji) is 2 UTF-16 units (a surrogate pair); everything else is 1.

// utf16Length — number of UTF-16 code units in s.
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2 // surrogate pair
		} else {
			n++
		}
	}
	return n
}

// UTF16Length — exported wrapper for service packages that cannot reach the
// unexported utf16Length (mirrors the engine's TextOperation size guards).
func UTF16Length(s string) int { return utf16Length(s) }

// utf16Decode — s → []uint16 of UTF-16 code units (supplementary as pairs).
func utf16Decode(s string) []uint16 {
	units := make([]uint16, 0, utf16Length(s))
	for _, r := range s {
		if r > 0xFFFF {
			cr := uint32(r) - 0x10000
			units = append(units,
				uint16(0xD800+(cr>>10)),
				uint16(0xDC00+(cr&0x3FF)),
			)
		} else {
			units = append(units, uint16(r))
		}
	}
	return units
}

// utf16Encode — []uint16 (surrogate-aware) → Go string (UTF-8).
func utf16Encode(units []uint16) string {
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) {
			// lead surrogate + its low part → one supplementary rune.
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
