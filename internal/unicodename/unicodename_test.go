package unicodename

import "testing"

func TestLookup(t *testing.T) {
	for _, c := range []struct {
		cp        rune
		name, cat string
	}{
		{0x41, "LATIN CAPITAL LETTER A", "Lu"},
		{0x10F2, "GEORGIAN LETTER HIE", "Ll"},
		{0x1F600, "GRINNING FACE", "So"},
		{0x0A, "LINE FEED (LF)", "Cc"},
		{0x200D, "ZERO WIDTH JOINER", "Cf"},
		{0x0301, "COMBINING ACUTE ACCENT", "Mn"},
		{0x4E00, "CJK UNIFIED IDEOGRAPH-4E00", "Lo"},
		{0x20000, "CJK UNIFIED IDEOGRAPH-20000", "Lo"},
		{0x17000, "TANGUT IDEOGRAPH-17000", "Lo"},
		{0xAC00, "HANGUL SYLLABLE GA", "Lo"},
		{0xD7A3, "HANGUL SYLLABLE HIH", "Lo"},
		{0xE000, "", "Co"},
		{0x10FFFF, "", "Cn"},
		{0x0378, "", "Cn"},
	} {
		name, cat := Lookup(c.cp)
		if name != c.name || cat != c.cat {
			t.Errorf("U+%04X: got %q %q, want %q %q", c.cp, name, cat, c.name, c.cat)
		}
	}
	if CategoryName("So") != "Other Symbol" {
		t.Error("category name")
	}
}
