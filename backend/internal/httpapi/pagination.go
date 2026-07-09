package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// C2 list conventions: ?cursor=<opaque>&limit=<n≤100> →
// {"items":[…],"next_cursor":"…|null"}. Cursors are opaque base64 wrappers
// around keyset values (never offsets), so pages stay stable under
// concurrent inserts.

const (
	DefaultLimit = 25
	MaxLimit     = 100
)

// PageRequest is the decoded pagination input of a list endpoint.
type PageRequest struct {
	// Cursor holds the decoded keyset values of the last row of the previous
	// page, or nil for the first page.
	Cursor []string
	Limit  int
}

// ErrBadCursor is returned (wrapped) when a cursor cannot be decoded.
var ErrBadCursor = errors.New("invalid cursor")

// ParsePage extracts cursor and limit query parameters per C2.
func ParsePage(r *http.Request) (PageRequest, error) {
	p := PageRequest{Limit: DefaultLimit}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return p, fmt.Errorf("limit must be a positive integer")
		}
		if n > MaxLimit {
			n = MaxLimit
		}
		p.Limit = n
	}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		vals, err := DecodeCursor(raw)
		if err != nil {
			return p, err
		}
		p.Cursor = vals
	}
	return p, nil
}

// EncodeCursor packs keyset values into an opaque cursor string.
func EncodeCursor(parts ...string) string {
	b, _ := json.Marshal(parts)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor unpacks a cursor produced by EncodeCursor.
func DecodeCursor(s string) ([]string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadCursor, err)
	}
	var parts []string
	if err := json.Unmarshal(b, &parts); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadCursor, err)
	}
	return parts, nil
}

// ListResponse is the C2 list envelope.
type ListResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// NewListResponse builds a list envelope; pass next == "" when there is no
// further page (serialized as null).
func NewListResponse[T any](items []T, next string) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	resp := ListResponse[T]{Items: items}
	if next != "" {
		resp.NextCursor = &next
	}
	return resp
}
