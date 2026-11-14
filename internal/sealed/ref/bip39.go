package ref

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"strings"
)

// The recovery phrase (docs/SEALED.md section 13.3): 12 words from the
// BIP-39 English list, standing for 128 random bits and BIP-39's 4-bit
// checksum. BIP-39's own key derivation is not used.

//go:embed bip39_english.txt
var bip39English string

// Words is the BIP-39 English list: 2,048 words, in order.
var Words = func() []string {
	w := strings.Fields(bip39English)
	if len(w) != 2048 {
		panic("ref: the BIP-39 word list must have 2,048 words")
	}
	return w
}()

// ErrPhrase is the refusal of a typed phrase: not twelve words, a word
// that is not on the list, or a checksum that does not match.
var ErrPhrase = errors.New("ref: that is not a recovery phrase")

// Phrase writes 16 bytes as twelve words: the 128 bits followed by the
// first four bits of their SHA-256, cut into twelve groups of eleven bits,
// each the index of a word.
func Phrase(entropy []byte) string {
	if len(entropy) != 16 {
		panic("ref: a phrase stands for 16 bytes")
	}
	sum := sha256.Sum256(entropy)
	bits := append(append([]byte{}, entropy...), sum[0]) // 136 bits; the first 132 are used
	words := make([]string, 12)
	for i := range words {
		words[i] = Words[bits11(bits, i)]
	}
	return strings.Join(words, " ")
}

// bits11 reads the i-th group of eleven bits, most significant first.
func bits11(b []byte, i int) int {
	v := 0
	for bit := i * 11; bit < (i+1)*11; bit++ {
		v = v<<1 | int(b[bit/8]>>(7-bit%8)&1)
	}
	return v
}

// ReadPhrase turns a typed phrase back into its 16 bytes: it is lower-cased
// and split on spaces, each word is matched to the list (the first four
// letters identify a word, so "aban" is "abandon"), and the checksum is
// checked.
func ReadPhrase(typed string) ([]byte, error) {
	words := strings.Fields(strings.ToLower(typed))
	if len(words) != 12 {
		return nil, ErrPhrase
	}
	bits := make([]byte, 17)
	for i, w := range words {
		idx := wordIndex(w)
		if idx < 0 {
			return nil, ErrPhrase
		}
		for k := 0; k < 11; k++ {
			if idx>>(10-k)&1 == 1 {
				bit := i*11 + k
				bits[bit/8] |= 1 << (7 - bit%8)
			}
		}
	}
	entropy := bits[:16]
	sum := sha256.Sum256(entropy)
	if bits[16]>>4 != sum[0]>>4 {
		return nil, ErrPhrase
	}
	return entropy, nil
}

// wordIndex finds a typed word on the list: the word itself, or its first
// four letters. It returns -1 for a word that is not there.
func wordIndex(w string) int {
	for i, word := range Words {
		if word == w {
			return i
		}
	}
	if len(w) < 4 {
		return -1
	}
	// No two words of the list share their first four letters, so those
	// identify a word whether it was cut short or misspelled further on.
	for i, word := range Words {
		if len(word) >= 4 && word[:4] == w[:4] {
			return i
		}
	}
	return -1
}
