package group

import (
	"github.com/mailgun/groupcache/v2"

	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	// NilBackfillError is returned by the Getter if there there is no backfill func, in lieu of panicing
	NilBackfillError = Error("item not in cache and backfill func is nil")
	// ItemNotFoundError is a generic error returned by a BackFillFunc if the item is not found or findable
	ItemNotFoundError = Error("item not found")
	// CacheNotFoundError is an error returned if the cache requested is not found
	CacheNotFoundError = Error("cache not found")
	// NameRequiredError is returned when creating or adding a cache, and the Config.Name field is empty
	NameRequiredError = Error("name is required")
)

// BackFillFunc is a function that can retrieve an uncached item to go into the cache
type BackFillFunc func(key string) ([]byte, error)

// Cache (group) is a distributed LRU cache where consistent hashing on keynames is used to cut out
// "who's on first" nonsense, and backfills are linearly distributed to mitigate multiple-member requests.
type Cache struct {
	addr     string
	caches   map[string]*groupcache.Group
	pool     *groupcache.HTTPPool
	configs  map[string]*Config
	close    func() error
	debugOut *log.Logger
	regLock  sync.Mutex
}

// NewCache creates a ache from the Config. Only call this once. If you need
// more caches use the .Add() function. fillfunc may be nil if caches will be added later
// using .Add().
func NewCache(config Config, fillfunc BackFillFunc) (*Cache, error) {

	srv := http.Server{}
	mux := http.NewServeMux()

	pool := groupcache.NewHTTPPoolOpts(config.PeerList[0], nil)
	pool.Set(config.PeerList...)
	mux.Handle("/", pool)

	srv.Handler = mux
	srv.Addr = config.ListenAddress

	gc := Cache{
		addr:     config.ListenAddress,
		debugOut: log.New(io.Discard, "[DEBUG] ", 0),
		pool:     pool,
		configs:  make(map[string]*Config),
		caches:   make(map[string]*groupcache.Group),
		close:    srv.Close,
	}

	if fillfunc != nil {
		if err := gc.Add(config, fillfunc); err != nil {
			return nil, err
		}
	}

	mux.HandleFunc("/stats", gc.Stats)

	go func(server *http.Server) {
		server.ListenAndServe()
	}(&srv)

	return &gc, nil
}

// Add creates new caches in the cluster. Config.ListenAddress and Config.PeerList are ignored.
func (gc *Cache) Add(config Config, fillfunc BackFillFunc) error {

	var gf groupcache.GetterFunc = func(ctx context.Context, key string, dest groupcache.Sink) error {

		if fillfunc == nil {
			return NilBackfillError
		}

		value, err := fillfunc(key)
		if err != nil {
			return err
		}
		if config.ItemExpiration == 0 {
			dest.SetBytes(value, time.Time{})
		} else {
			dest.SetBytes(value, time.Now().Add(config.ItemExpiration))
		}
		return nil
	}

	gc.regLock.Lock()
	defer gc.regLock.Unlock()

	gc.caches[config.Name] = groupcache.NewGroup(config.Name, config.CacheSize, gf)
	gc.configs[config.Name] = &config
	return nil
}

// Names returns the names of the current caches
func (gc *Cache) Names() []string {
	gc.regLock.Lock()
	defer gc.regLock.Unlock()

	list := make([]string, len(gc.caches))
	i := 0
	for k := range gc.caches {
		list[i] = k
		i++
	}
	return list
}

// Close calls the listener close function
func (gc *Cache) Close() error {
	return gc.close()
}

// Get will return the value of the cacheName'd key, asking other cache members or
// backfilling as necessary.
func (gc *Cache) Get(cacheName, key string) (value interface{}, ok bool) {
	return gc.GetContext(context.Background(), cacheName, key)
}

// GetContext will return the value of the cacheName'd key, asking other cache members or
// backfilling as necessary, honoring the provided context.
func (gc *Cache) GetContext(ctx context.Context, cacheName, key string) (value interface{}, ok bool) {
	gc.debugOut.Printf("Getting %s %s\n", cacheName, key)
	return gc.get(ctx, cacheName, key)
}

func (gc *Cache) get(ctx context.Context, cacheName, key string) (value interface{}, ok bool) {
	if cache, ok := gc.caches[cacheName]; ok {
		var b []byte
		err := cache.Get(ctx, key, groupcache.AllocatingByteSliceSink(&b))
		if err != nil {
			// crap
			return err, false
		}
		return b, true
	}
	return CacheNotFoundError, false
}

// Set forces an item into the cache, following the configured expiration policy
func (gc *Cache) Set(cacheName, key string, value []byte) error {
	return gc.SetContext(context.Background(), cacheName, key, value, time.Time{})
}

// SetContext forces an item into the cache, following the specified expiration (unless a zero Time is provided
// then falling back to the configured expiration policy) honoring the provided context.
func (gc *Cache) SetContext(ctx context.Context, cacheName, key string, value []byte, expiration time.Time) error {
	gc.debugOut.Printf("Setting %s %s @ %s\n", cacheName, key, expiration.String())
	return gc.set(ctx, cacheName, key, value, expiration)
}

// set is an internal function for all of the Set* funcs. expirationOption is either “true“ (follow policy),
// “false“ (no expiration), or a time.Time specifying when to expire the item
func (gc *Cache) set(ctx context.Context, cacheName, key string, value []byte, expiration time.Time) error {
	if cache, ok := gc.caches[cacheName]; ok {
		if expiration.IsZero() && gc.configs[cacheName].ItemExpiration != 0 {
			// Local expiration is zero, but the cache has an expiration
			return cache.Set(ctx, key, value, time.Now().Add(gc.configs[cacheName].ItemExpiration), true)
		}
		return cache.Set(ctx, key, value, expiration, true)
	}
	return CacheNotFoundError
}

// SetToExpireAt forces an item into the cache, to expire at a specific time regardless of the cache configuration. Use
// SetContext if you need to set the expiration and a context.
func (gc *Cache) SetToExpireAt(cacheName, key string, expireAt time.Time, value []byte) error {
	gc.debugOut.Printf("Setting %s %s @ %s\n", cacheName, key, expireAt.String())
	return gc.set(context.Background(), cacheName, key, value, expireAt)
}

// Remove makes a best effort to remove an item from the cache
func (gc *Cache) Remove(cacheName, key string) error {
	return gc.RemoveContext(context.Background(), cacheName, key)
}

// RemoveContext makes a best effort to remove an item from the cache, honoring the provided context.
func (gc *Cache) RemoveContext(ctx context.Context, cacheName, key string) error {
	if cache, ok := gc.caches[cacheName]; ok {
		gc.debugOut.Printf("Removing %s %s\n", cacheName, key)
		return cache.Remove(ctx, key)
	}
	return CacheNotFoundError
}

// SetDebugOut wires in the debug logger to the specified logger
func (gc *Cache) SetDebugOut(logger *log.Logger) {
	gc.debugOut = logger
}

// SetPeers allows the dynamic [re]setting of the peerlist
func (gc *Cache) SetPeers(peers ...string) {
	gc.pool.Set(peers...)
}

// Stats is a request finisher that outputs the Cache stats as JSON
func (gc *Cache) Stats(w http.ResponseWriter, req *http.Request) {

	stb, err := gc.stats()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Write(stb)
	w.Write([]byte{10})
}

func (gc *Cache) stats() ([]byte, error) {
	type cachesStats struct {
		Main groupcache.CacheStats
		Hot  groupcache.CacheStats
	}
	type stats struct {
		Cache  string
		Group  groupcache.Stats
		Caches cachesStats
	}

	gc.regLock.Lock()
	defer gc.regLock.Unlock()

	statList := make([]stats, 0)

	for name, gp := range gc.caches {
		statList = append(statList,
			stats{
				Cache: name,
				Group: gp.Stats,
				Caches: cachesStats{
					Main: gp.CacheStats(groupcache.MainCache),
					Hot:  gp.CacheStats(groupcache.HotCache),
				},
			})
	}

	data, err := json.MarshalIndent(statList, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil

}
