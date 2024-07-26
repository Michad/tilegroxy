# Migrating from tilestache

An important difference between tilegroxy and tilestache is that tilegroxy can only be run as a standalone executable rather than running as a module in another webserver.  However tilegroxy, like tilestache, can also [be used as a library](./extensibility.md#using-tilegroxy-as-a-library) if you wish to build applications extending it.

The configuration in tilegroxy is meant to be highly compatible with the configuration of tilestache, however there are significant differences. The tilegroxy configuration supports a variety of options that are not available in tilestache and while we try to keep most parameters optional and have sane and safe defaults, it is highly advised you familiarize yourself with the various options documented above.

The following are the known incompatibilities with tilestache configurations:

* Unsupported providers:
    * Mapnik
    * Vector
    * MBTiles
    * Mapnik Grid
    * Goodies providers
* Unsupported caches:
    * LimitedDisk
* "Names" are always in all lowercase 
* Configuration keys are case insensitive and have no spaces
* Configuring projections is currently unsupported
* Cache contents are not guaranteed to be transferrable
* Layers:
    * Layers are supplied as a flat array of layer objects with an `id` parameter for the URL-safe layer name instead of them being supplied as an Object with the id being a key. 
    * Most parameters unavailable. Some can be configured via the `Client` configuration and others will be added in future versions.
    * The `write cache` parameter is replaced with `skipcache` with inverted value
    * No `bounds` parameter - instead use the `fallback` provider but note it applies on a per-tile level only (not per-pixel)
    * No `pixel effects` parameter - instead use the `effect` provider
* URL Template provider:
    * For the most part you should use the Proxy provider instead but URL Template is available for compatibility
    * No `referer` parameter - instead specify the referer header via the `Client` configuration
    * No `timeout` parameter - instead specify the timeout via the `Client` configuration
    * No `source projection` parameter - Might be added in the future
* Sandwich provider:
    * No direct equivalent to the sandwich provider is available but most if not all functionality is available by combining Blend and Static providers
* Proxy provider:
    * No `provider` parameter 
    * No `timeout` parameter - instead specify the timeout via the `Client` configuration
* Test cache:
    * It's recommended but not required to change the `name` to "none" instead of "test"
    * No 'verbose' parameter - Instead use the `Logging` configuration to turn on debug logging if needed
* Disk cache:
    * No `umode` parameter - Instead use `filemode` with Go numerics instead of unix numerics. Might be added in the future
    * No `dirs` parameter - Files are currently stored in a flat structure rather than creating separate directories
    * No `gzip` parameter - Might be added in the future
    * The `path` parameter must be supplied as a file path, not a URI
* Memcache cache:
    * No `revision` parameter - Put the revision inside the key prefix
    * The `key prefix` parameter is replaced with `keyprefix`
    * The `servers` array is now an array of objects containing `host` and `port` instead of an array of strings with those combined
* Redis cache:
    * Supports a wider variety of configuration options. It's recommended but not required that you consider utilizing a Cluster or Ring deployment if you previously used a single server.
    * The `key prefix` parameter is replaced with `keyprefix`
* S3 cache:
    * No `use_locks` parameter - Caches are currently lockless
    * No `reduced_redundancy` parameter - Instead use the more flexible `storageclass` parameter with the "REDUCED_REDUNDANCY" option
    * While supported, it's recommended you don't use the `access` and `secret` parameters. All standard methods of supplying AWS credentials are supported.
