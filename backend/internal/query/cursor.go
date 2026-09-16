package query

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// cursor is what a nextCursor string decodes to. Opaque to the client;
// bound to the project and to a hash of the filter/sort so a cursor from
// one list cannot page another (frontend/API_CONTRACT.md: "cursor must be
// bound to project/filter/sort by the server"). Tampering only ever yields
// a page of the same project — the tenant condition is in every query.
type cursor struct {
	Kind    string `json:"k"`
	Project string `json:"p"`
	Filter  string `json:"f"`
	// Keyset position. Which fields matter depends on Kind.
	Time string  `json:"t,omitempty"`
	Str  string  `json:"s,omitempty"`
	Num  float64 `json:"n,omitempty"`
	Rank int     `json:"r,omitempty"`
	ID   string  `json:"i"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s, kind, project, filter string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return cursor{}, ErrBadCursor
	}
	if err := json.Unmarshal(b, &c); err != nil || c.Kind != kind || c.Project != project || c.Filter != filter || c.ID == "" {
		return cursor{}, ErrBadCursor
	}
	return c, nil
}

// filterHash pins a cursor to the exact list it came from.
func filterHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8])
}

func clamp(limit, def int) int {
	if limit <= 0 {
		return def
	}
	if limit > 100 {
		return 100
	}
	return limit
}
