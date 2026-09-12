package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Slugify lowercases, strips accents, keeps [a-z0-9], and joins with hyphens.
// Non-Latin scripts are dropped; the admin can edit the slug by hand.
func Slugify(s string) string {
	s = norm.NFD.String(strings.ToLower(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case unicode.Is(unicode.Mn, r):
			// combining mark from NFD: drop it
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

// UniqueSlug returns base, or base-2, base-3, ... until no row in table other
// than excludeID has it. table must be "products" or "categories".
func (s *Store) UniqueSlug(ctx context.Context, table, base string, excludeID int64) (string, error) {
	if table != "products" && table != "categories" {
		return "", fmt.Errorf("UniqueSlug: bad table %q", table)
	}
	q := `select exists(select 1 from ` + table + ` where slug = $1 and id <> $2)`
	for i := 1; ; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", base, i)
		}
		var taken bool
		if err := s.db.QueryRow(ctx, q, cand, excludeID).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
}
