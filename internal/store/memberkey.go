package store

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
)

func adhocMemberKey(ids []model.AccountID) string {
	sorted := make([]string, 0, len(ids))
	seen := map[model.AccountID]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			sorted = append(sorted, string(id))
		}
	}
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return "a:" + hex.EncodeToString(sum[:])
}
