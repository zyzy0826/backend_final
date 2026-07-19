package middleware

import (
	"sync"
	"time"
)

// TokenDenylist tracks JWTs that have been explicitly revoked (e.g. on logout).
// Because JWTs are stateless, this is the only way to invalidate a token before
// its natural expiry. Entries are keyed by the token's jti claim and drop out
// once the token would have expired anyway, so the map stays bounded.
//
// This is an in-memory store: it is per-process (not shared across replicas)
// and cleared on restart. For a single-instance deployment that is sufficient;
// a multi-instance setup would need a shared backend (DB/Redis) instead.
type TokenDenylist struct {
	mu      sync.Mutex
	revoked map[string]time.Time // jti -> original expiry
}

func NewTokenDenylist() *TokenDenylist {
	return &TokenDenylist{revoked: make(map[string]time.Time)}
}

// Revoke marks a token id as invalid until its expiry. Expired entries are swept
// on each call, so a revoked token never lingers past when it would have died.
func (d *TokenDenylist) Revoke(jti string, exp time.Time) {
	if jti == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for id, e := range d.revoked {
		if now.After(e) {
			delete(d.revoked, id)
		}
	}
	d.revoked[jti] = exp
}

// IsRevoked reports whether the given token id has been revoked and is still
// within its validity window.
func (d *TokenDenylist) IsRevoked(jti string) bool {
	if jti == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	exp, ok := d.revoked[jti]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(d.revoked, jti)
		return false
	}
	return true
}
