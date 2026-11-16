package secret

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateAccessKeys = flag.Bool("update", false, "rewrite testdata/accesskey/vectors.json")

// The shared vectors of the access key grammar (testdata/accesskey), which
// the web pages and the app run as well. This test writes them with
// -update and otherwise checks that they still say what this code does.

type akKey struct {
	Name     string `json:"name"`
	Random   string `json:"random"`
	Key      string `json:"key"`
	Auth     string `json:"auth"`
	AuthHash string `json:"auth_hash"`
	Wrap     string `json:"wrap"`
}

type akInput struct {
	Name  string `json:"name"`
	Input string `json:"input"`
	// Random is the bytes the input reads as, or "" when it is refused.
	Random  string `json:"random,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

func fixedRandom(label string) []byte {
	s := sha256.Sum256([]byte(label))
	return s[:]
}

// rawKey writes any 35 bytes as 48 characters, for the vectors that need a
// key this code would never make (another version).
func rawKey(raw []byte) string {
	n := new(big.Int).SetBytes(raw)
	digits := make([]byte, AccessKeyChars)
	mod := new(big.Int)
	for i := AccessKeyChars - 1; i >= 0; i-- {
		n.DivMod(n, accessKeyBase, mod)
		digits[i] = AccessKeyAlphabet[mod.Int64()]
	}
	return string(digits)
}

func swapCase(c byte) byte {
	switch {
	case c >= 'a' && c <= 'z':
		return c - 32
	case c >= 'A' && c <= 'Z':
		return c + 32
	}
	return c
}

func accessKeyVectors(t *testing.T) map[string]any {
	randoms := []struct {
		name string
		b    []byte
	}{
		{"all zero", make([]byte, 32)},
		{"all ff", bytes.Repeat([]byte{0xff}, 32)},
		{"fixed one", fixedRandom("churr access key vector one")},
		{"fixed two", fixedRandom("churr access key vector two")},
		{"fixed three", fixedRandom("churr access key vector three")},
	}
	var keys []akKey
	for _, r := range randoms {
		auth := AccessKeyAuth(r.b)
		h, _ := HashAccessKeyAuth(auth)
		keys = append(keys, akKey{Name: r.name, Random: hex.EncodeToString(r.b), Key: FormatAccessKey(r.b),
			Auth: hex.EncodeToString(auth), AuthHash: hex.EncodeToString(h), Wrap: hex.EncodeToString(AccessKeyWrap(r.b))})
	}
	one := fixedRandom("churr access key vector one")
	key := FormatAccessKey(one)
	bare := strings.ReplaceAll(key, " ", "")
	groups := strings.Split(key, " ")
	at := func(i int, c byte) string { b := []byte(bare); b[i] = c; return string(b) }
	// A character of the key that is a letter, for the case vectors.
	letter := -1
	for i := 1; i < len(bare); i++ {
		if c := bare[i]; (c >= 'a' && c <= 'z' && c != 'o' && c != 'l' && c != 'i') || (c >= 'A' && c <= 'Z') {
			letter = i
			break
		}
	}
	if letter < 0 {
		t.Fatal("the fixed key has no letter to change the case of")
	}
	other := func(i int) byte {
		c := bare[i]
		j := (strings.IndexByte(AccessKeyAlphabet, c) + 1) % 58
		return AccessKeyAlphabet[j]
	}
	swapAt := 10
	for bare[swapAt] == bare[swapAt+1] {
		swapAt++
	}
	swapped := []byte(bare)
	swapped[swapAt], swapped[swapAt+1] = swapped[swapAt+1], swapped[swapAt]

	// A key of version 2 with a correct checksum.
	v2 := append([]byte{2}, one...)
	sum := sha256.Sum256(v2)
	v2 = append(v2, sum[:2]...)

	inputs := []akInput{
		{Name: "as shown", Input: key},
		{Name: "no spaces", Input: bare},
		{Name: "hyphens between groups", Input: strings.Join(groups, "-")},
		{Name: "spaces anywhere", Input: " " + bare[:5] + "  " + bare[5:17] + " " + bare[17:] + " "},
		{Name: "lines, tabs and a no-break space", Input: strings.Join(groups[:6], "\t") + "\n" + strings.Join(groups[6:9], " ") + "\r\n" + strings.Join(groups[9:], " - ")},
		{Name: "a zero", Input: at(7, '0'), Error: "character"},
		{Name: "a one", Input: at(7, '1'), Error: "character"},
		{Name: "a capital O", Input: at(7, 'O'), Error: "character"},
		{Name: "a capital I", Input: at(7, 'I'), Error: "character"},
		{Name: "punctuation", Input: at(7, '.'), Error: "character"},
		{Name: "a letter outside ASCII", Input: strings.Replace(bare, bare[7:8], "é", 1), Error: "character"},
		{Name: "empty", Input: "", Error: "length"},
		{Name: "one short", Input: bare[:47], Error: "length"},
		{Name: "one long", Input: bare + "2", Error: "length"},
		{Name: "a group missing", Input: strings.Join(groups[:11], " "), Error: "length"},
		{Name: "the wrong case", Input: at(letter, swapCase(bare[letter])), Error: "checksum"},
		{Name: "all in small letters", Input: strings.ToLower(bare), Error: "checksum"},
		{Name: "one character changed", Input: at(30, other(30)), Error: "checksum"},
		{Name: "two characters swapped", Input: string(swapped), Error: "checksum"},
		{Name: "the last character changed", Input: at(47, other(47)), Error: "checksum"},
		{Name: "too large to be a key", Input: strings.Repeat("z", 48), Error: "checksum"},
		{Name: "another version", Input: rawKey(v2), Error: "version"},
	}
	for i, in := range inputs {
		got, err := ParseAccessKey(in.Input)
		if in.Error == "" {
			if err != nil {
				t.Fatalf("%s: %v", in.Name, err)
			}
			inputs[i].Random = hex.EncodeToString(got)
			continue
		}
		var ae *AccessKeyError
		if !errors.As(err, &ae) || ae.Kind != in.Error {
			t.Fatalf("%s: got %v, want a %s error", in.Name, err, in.Error)
		}
		inputs[i].Message = ae.Message
	}
	return map[string]any{
		"about": "The access key grammar of docs/AUTH.md section 5.6, shared by the server (internal/secret), the web pages " +
			"(static/shared/accesskey.js) and the app (lib/core/access_key.dart). Written by go test ./internal/secret -update.",
		"alphabet":  AccessKeyAlphabet,
		"version":   AccessKeyVersion,
		"salt":      AccessKeySalt,
		"auth_info": AccessKeyAuthInfo,
		"wrap_info": AccessKeyWrapInfo,
		"keys":      keys,
		"inputs":    inputs,
	}
}

func TestAccessKeyVectors(t *testing.T) {
	b, err := json.MarshalIndent(accessKeyVectors(t), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	path := filepath.Join("..", "..", "testdata", "accesskey", "vectors.json")
	if *updateAccessKeys {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, b) {
		t.Errorf("%s is out of date: run go test ./internal/secret -update and read the diff", path)
	}
}

func TestAccessKeyRoundTrip(t *testing.T) {
	for i := 0; i < 500; i++ {
		r := NewAccessKey()
		k := FormatAccessKey(r)
		if len(k) != 59 || k[0] != '2' || strings.Count(k, " ") != 11 {
			t.Fatalf("%q is not 12 groups of 4 starting with 2", k)
		}
		for _, c := range []byte(strings.ReplaceAll(k, " ", "")) {
			if strings.IndexByte(AccessKeyAlphabet, c) < 0 {
				t.Fatalf("%q has %q", k, c)
			}
		}
		got, err := ParseAccessKey(k)
		if err != nil || !bytes.Equal(got, r) {
			t.Fatalf("%q read back as %x, %v", k, got, err)
		}
	}
	if _, err := HashAccessKeyAuth(make([]byte, 31)); err == nil {
		t.Fatal("a 31-byte sign-in value was hashed")
	}
}
