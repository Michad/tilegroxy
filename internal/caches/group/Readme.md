

# group
`import "github.com/Michad/tilegroxy/internal/caches/group"`

* [Overview](#pkg-overview)
* [Index](#pkg-index)

## <a name="pkg-overview">Overview</a>



## <a name="pkg-index">Index</a>
* [Constants](#pkg-constants)
* [type BackFillFunc](#BackFillFunc)
* [type Cache](#Cache)
  * [func NewCache(config Config, fillfunc BackFillFunc) (*Cache, error)](#NewCache)
  * [func (gc *Cache) Add(config Config, fillfunc BackFillFunc) error](#Cache.Add)
  * [func (gc *Cache) Close() error](#Cache.Close)
  * [func (gc *Cache) Exists(name string) bool](#Cache.Exists)
  * [func (gc *Cache) Get(cacheName, key string) (value interface{}, ok bool)](#Cache.Get)
  * [func (gc *Cache) GetContext(ctx context.Context, cacheName, key string) (value interface{}, ok bool)](#Cache.GetContext)
  * [func (gc *Cache) Names() []string](#Cache.Names)
  * [func (gc *Cache) Remove(cacheName, key string) error](#Cache.Remove)
  * [func (gc *Cache) RemoveContext(ctx context.Context, cacheName, key string) error](#Cache.RemoveContext)
  * [func (gc *Cache) Set(cacheName, key string, value []byte) error](#Cache.Set)
  * [func (gc *Cache) SetContext(ctx context.Context, cacheName, key string, value []byte, expiration time.Time) error](#Cache.SetContext)
  * [func (gc *Cache) SetDebugOut(logger *log.Logger)](#Cache.SetDebugOut)
  * [func (gc *Cache) SetPeers(peers ...string)](#Cache.SetPeers)
  * [func (gc *Cache) SetToExpireAt(cacheName, key string, expireAt time.Time, value []byte) error](#Cache.SetToExpireAt)
  * [func (gc *Cache) Stats(w http.ResponseWriter, req *http.Request)](#Cache.Stats)
* [type Config](#Config)
* [type Error](#Error)
  * [func (e Error) Error() string](#Error.Error)


#### <a name="pkg-files">Package files</a>
[group.go](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go) [misc.go](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/misc.go)


## <a name="pkg-constants">Constants</a>
``` go
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
```




## <a name="BackFillFunc">type</a> [BackFillFunc](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=831:881#L27)
``` go
type BackFillFunc func(key string) ([]byte, error)
```
BackFillFunc is a function that can retrieve an uncached item to go into the cache










## <a name="Cache">type</a> [Cache](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=1090:1293#L31)
``` go
type Cache struct {
    // contains filtered or unexported fields
}

```
Cache (group) is a distributed LRU cache where consistent hashing on keynames is used to cut out
"who's on first" nonsense, and backfills are linearly distributed to mitigate multiple-member requests.







### <a name="NewCache">func</a> [NewCache](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=1479:1546#L44)
``` go
func NewCache(config Config, fillfunc BackFillFunc) (*Cache, error)
```
NewCache creates a ache from the Config. Only call this once. If you need
more caches use the .Add() function. fillfunc may be nil if caches will be added later
using .Add().





### <a name="Cache.Add">func</a> (\*Cache) [Add](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=2316:2380#L81)
``` go
func (gc *Cache) Add(config Config, fillfunc BackFillFunc) error
```
Add creates new caches in the cluster. Config.ListenAddress and Config.PeerList are ignored.




### <a name="Cache.Close">func</a> (\*Cache) [Close](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=3450:3480#L135)
``` go
func (gc *Cache) Close() error
```
Close calls the listener close function




### <a name="Cache.Exists">func</a> (\*Cache) [Exists](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=3247:3288#L124)
``` go
func (gc *Cache) Exists(name string) bool
```
Exists returns true if the named cache exists.




### <a name="Cache.Get">func</a> (\*Cache) [Get](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=3617:3689#L141)
``` go
func (gc *Cache) Get(cacheName, key string) (value interface{}, ok bool)
```
Get will return the value of the cacheName'd key, asking other cache members or
backfilling as necessary.




### <a name="Cache.GetContext">func</a> (\*Cache) [GetContext](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=3905:4005#L147)
``` go
func (gc *Cache) GetContext(ctx context.Context, cacheName, key string) (value interface{}, ok bool)
```
GetContext will return the value of the cacheName'd key, asking other cache members or
backfilling as necessary, honoring the provided context.




### <a name="Cache.Names">func</a> (\*Cache) [Names](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=2999:3032#L110)
``` go
func (gc *Cache) Names() []string
```
Names returns the names of the current caches




### <a name="Cache.Remove">func</a> (\*Cache) [Remove](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=6289:6341#L198)
``` go
func (gc *Cache) Remove(cacheName, key string) error
```
Remove makes a best effort to remove an item from the cache




### <a name="Cache.RemoveContext">func</a> (\*Cache) [RemoveContext](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=6512:6592#L203)
``` go
func (gc *Cache) RemoveContext(ctx context.Context, cacheName, key string) error
```
RemoveContext makes a best effort to remove an item from the cache, honoring the provided context.




### <a name="Cache.Set">func</a> (\*Cache) [Set](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=4518:4581#L166)
``` go
func (gc *Cache) Set(cacheName, key string, value []byte) error
```
Set forces an item into the cache, following the configured expiration policy




### <a name="Cache.SetContext">func</a> (\*Cache) [SetContext](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=4868:4981#L172)
``` go
func (gc *Cache) SetContext(ctx context.Context, cacheName, key string, value []byte, expiration time.Time) error
```
SetContext forces an item into the cache, following the specified expiration (unless a zero Time is provided
then falling back to the configured expiration policy) honoring the provided context.




### <a name="Cache.SetDebugOut">func</a> (\*Cache) [SetDebugOut](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=6826:6874#L212)
``` go
func (gc *Cache) SetDebugOut(logger *log.Logger)
```
SetDebugOut wires in the debug logger to the specified logger




### <a name="Cache.SetPeers">func</a> (\*Cache) [SetPeers](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=6961:7003#L217)
``` go
func (gc *Cache) SetPeers(peers ...string)
```
SetPeers allows the dynamic [re]setting of the peerlist




### <a name="Cache.SetToExpireAt">func</a> (\*Cache) [SetToExpireAt](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=5978:6071#L192)
``` go
func (gc *Cache) SetToExpireAt(cacheName, key string, expireAt time.Time, value []byte) error
```
SetToExpireAt forces an item into the cache, to expire at a specific time regardless of the cache configuration. Use
SetContext if you need to set the expiration and a context.




### <a name="Cache.Stats">func</a> (\*Cache) [Stats](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/group.go?s=7100:7164#L222)
``` go
func (gc *Cache) Stats(w http.ResponseWriter, req *http.Request)
```
Stats is a request finisher that outputs the Cache stats as JSON




## <a name="Config">type</a> [Config](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/misc.go?s=261:771#L16)
``` go
type Config struct {
    Name           string        // For New and Add. Pass as ``cacheName`` to differentiate caches
    ListenAddress  string        // Only for New to set the listener
    PeerList       []string      // Only for New to establish the initial PeerList. May be reset with GroupCache.SetPeers()
    CacheSize      int64         // For New and Add to set the size in bytes of the cache
    ItemExpiration time.Duration // For New and Add to set the default expiration duration. Leave as empty for infinite.
}

```
Config is used to store configuration information to pass to a GroupCache.










## <a name="Error">type</a> [Error](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/misc.go?s=61:78#L8)
``` go
type Error string
```
Error is an error type










### <a name="Error.Error">func</a> (Error) [Error](https://github.com/Michad/tilegroxy/tree/master/internal/caches/group/misc.go?s=130:159#L11)
``` go
func (e Error) Error() string
```
Error returns the stringified version of Error








- - -
Generated by [godoc2md](http://github.com/cognusion/godoc2md)
