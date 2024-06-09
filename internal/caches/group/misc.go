package group

import (
	"time"
)

// Error is an error type
type Error string

// Error returns the stringified version of Error
func (e Error) Error() string {
	return string(e)
}

// Config is used to store configuration information to pass to a GroupCache.
type Config struct {
	Name           string        // For New and Add. Pass as ``cacheName`` to differentiate caches
	ListenAddress  string        // Only for New to set the listener
	PeerList       []string      // Only for New to establish the initial PeerList. May be reset with GroupCache.SetPeers()
	CacheSize      int64         // For New and Add to set the size in bytes of the cache
	ItemExpiration time.Duration // For New and Add to set the default expiration duration. Leave as empty for infinite.
}
