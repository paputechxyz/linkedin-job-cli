package store

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// ContentHash fingerprints a job for LLM-free dedup. It is stable across
// cosmetic differences (case, whitespace, HTML tags) so a re-fetched or
// cross-source duplicate of the same posting matches. The full description is
// included per R3; listedAt anchors the posting in time.
func ContentHash(company, title, description string, listedAt int64) string {
	s := normalize(company) + "\x1f" + normalize(title) + "\x1f" + normalize(description) + "\x1f" + strconv.FormatInt(listedAt, 10)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// IsDuplicateEnriched reports whether any stored job with this content hash has
// already been enriched. Used by ingest to skip a redundant LLM call on a
// re-fetch or cross-source duplicate (R3, R7).
func (s *Store) IsDuplicateEnriched(hash string) bool {
	if hash == "" {
		return false
	}
	j, err := s.FindByContentHash(hash)
	if err != nil || j == nil {
		return false
	}
	return j.IsEnriched()
}

func normalize(s string) string {
	s = strings.ToLower(s)
	s = htmlTagRE.ReplaceAllString(s, " ")
	return collapseWS(s)
}

func collapseWS(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range strings.TrimSpace(s) {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			if !inSpace {
				b.WriteRune(' ')
				inSpace = true
			}
		default:
			b.WriteRune(r)
			inSpace = false
		}
	}
	return b.String()
}
