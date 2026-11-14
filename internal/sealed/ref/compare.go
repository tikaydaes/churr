package ref

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// The comparison code (docs/SEALED.md section 15) and call keys (sections
// 5.2 and 10).

// compareRounds is how many times each half is hashed again.
const compareRounds = 5200

// CompareHalf is one account's half of the comparison code: 30 digits made
// from the account key a device has pinned for that account.
func CompareHalf(accountKey []byte, account UUID, server string) string {
	h := sha256.Sum256(labelled(LabelCompare, accountKey, account[:], []byte(server)))
	for i := 0; i < compareRounds; i++ {
		h = sha256.Sum256(append(h[:], accountKey...))
	}
	var sb strings.Builder
	for g := 0; g < 6; g++ {
		// Five bytes as a big-endian number, modulo 100,000.
		var v uint64
		for _, b := range h[g*5 : g*5+5] {
			v = v<<8 | uint64(b)
		}
		fmt.Fprintf(&sb, "%05d", v%100000)
	}
	return sb.String()
}

// CompareCode is the code two people compare: the two halves, the smaller
// first when compared as text, as twelve groups of five digits separated
// by spaces. Both devices show the same 60 digits if and only if each has
// pinned the other's true account key.
func CompareCode(halfA, halfB string) string {
	if halfB < halfA {
		halfA, halfB = halfB, halfA
	}
	digits := halfA + halfB
	groups := make([]string, 0, 12)
	for i := 0; i < len(digits); i += 5 {
		groups = append(groups, digits[i:i+5])
	}
	return strings.Join(groups, " ")
}

// CallSecret derives a call's secret from the epoch secret. It comes from
// the epoch secret and not the history secret, so the backup cannot yield
// call keys.
func CallSecret(epochSecret []byte, conv UUID, keyEpoch uint64, call UUID) []byte {
	return HKDF(epochSecret, epochSalt(conv, keyEpoch), labelled(LabelCall, call[:]), 32)
}

// CallSenderKey derives the key one device encrypts its call frames under.
func CallSenderKey(callSecret []byte, conv UUID, keyEpoch uint64, device UUID) []byte {
	return HKDF(callSecret, epochSalt(conv, keyEpoch), labelled(LabelCallSender, device[:]), 32)
}

// CallKeyIndex is the key index carried in each call frame.
func CallKeyIndex(keyEpoch uint64) byte { return byte(keyEpoch % 16) }
