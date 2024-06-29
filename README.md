# tilegroxy    
[![Docker Image CI](https://github.com/Michad/tilegroxy/actions/workflows/docker-image.yml/badge.svg)](https://github.com/Michad/tilegroxy/actions/workflows/docker-image.yml) [![Go Report Card](https://goreportcard.com/badge/michad/tilegroxy)](https://goreportcard.com/report/michad/tilegroxy) ![Go](https://img.shields.io/github/go-mod/go-version/michad/tilegroxy) 
![Coverage](https://img.shields.io/badge/Coverage-80.2%25-brightgreen)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0) [![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md) 

Tilegroxy lives between your map and your mapping providers to deliver a consistent, cached API for all your layers.

🚀 Built in Go.  
🔌 Features a flexible plugin system powered by [Yaegi](https://github.com/traefik/yaegi).  
💡 Inspired by [tilestache](https://github.com/tilestache/tilestache)   
🛠️ This project is still a work in progress. Changes may occur prior to the 1.0 release.

## Why tilegroxy?

Tilegroxy shines when you consume maps from multiple sources.  It isn't tied to any one mapping backend and can pull data from any protocol, whether the standard alphabet soup or a proprietary, authenticated API. Rather than make your frontend aware of every single vendor and exposing your keys, utilize tilegroxy and provide a uniform API with a configuration-driven backend that can be augmented by code when necessary.  

### Features:

* Provide a uniform mapping API for incoming requests
* Proxy to ZXY, WMS, TMS, WMTS, or other protocol map layers
* Cache tiles in disk, memory, s3, redis, and/or memcached
* Require authentication using static key, JWT, or custom logic
* Create your own custom provider to pull in non-standard and proprietary imagery sources
* Tweak your map layer with 18 standard effects or by providing your own pixel-level logic
* Combine multiple map layers with adjustable rules and blending methods
* Generic support for any content type (raster or vector)
* Configurable timeout, logging, and error handling rules
* Commands for seeding and testing your layers
* Container deployment

The following are on the roadmap and expected before a 1.0 release:

* Proxy map layers directly to local providers such as Mapnik, Mapserver 
* Providers that composite/modify vector layers formats such as [MVT](https://github.com/mapbox/vector-tile-spec) or tiled GeoJSON
* OpenTelemetry support
* Support for external secret stores such as AWS Secrets Manager to avoid secrets in the configuration
* Support for external configuration sources 
* Support for HTTPS server w/ Let's Encrypt or static certs



## Configuration

Configuration is required to define your layers and operational parameter.  Currently that must be supplied as a single file upfront.  Loading configuration from external services or hot-loading configuration is planned but not yet supported.

Details can be found in [documentation](./docs/configuration.md) or through [examples](./examples/configurations/). 

You can also use `tilegroxy config create` to help get started.

## How to get it

Tilegroxy is designed to be run in a container. But it can also be run directly for that old-school approach; we don't judge.   

It is also meant to be used in Linux (or at least *nix). Building and running on Windows should work but is currently untested. You will need a bash-compatible shell to run the Makefile.

### Building

Tilegroxy builds as a statically linked executable. Prebuilt binaries are available from [Github](https://github.com/Michad/tilegroxy/releases).

Building tilegroxy yourself requires `go`, `git`, `make`, and `date`.  It uses a standard Makefile workflow:

```
make
```

The build includes integration tests using [testcontainers](https://golang.testcontainers.org/) which requires you have either docker or podman installed. If you encounter difficulties running these tests it's recommended you use a prebuilt binary.  That said, you can build with only unit tests using:

```
make clean build unit
```

Installing it after it's built is of course:

```
sudo make install
```

Once installed, tilegroxy can be run directly as an HTTP server via the `tilegroxy serve` command documented below. It's recommended to create a systemd unit file to allow it to run as a daemon as an appropriate user.

### Docker

Tilegroxy is available as a container image on the Github container repository.  

You can pull the most recent versioned release with the `latest` tag and the very latest (and maybe buggy) build with the `edge` tag. Tags are also available for version numbers.  [See here for a list](https://github.com/Michad/tilegroxy/pkgs/container/tilegroxy).

For example: 

```
docker pull ghcr.io/michad/tilegroxy:latest
```

To then run tilegroxy:

```
docker run --rm -v ./test_config.yml:/tilegroxy/tilegroxy.yml:Z ghcr.io/michad/tilegroxy seed -l osm -z 0 -v
```

You can of course build the docker image yourself:

```
docker build . -t tilegroxy
```

An [example docker-compose.yml file](./docker-compose.yml) is included that can be used to start the tilegroxy server using a configuration file named "test_config.yml" in the current working directory.

### Kubernetes

Coming soon. 

## Commands

The `tilegroxy` executable is a standard [cobra](https://github.com/spf13/cobra) program with a handful of commands available. If you're deploying tilegroxy for use as a webserver you want to use the `serve` command. A couple other commands are available to aid in standing up and administering a tilegroxy deployment.

### Serve

The main operating mode of tilegroxy. Starts up an HTTP server and responds to incoming web requests.

```
tilegroxy serve -c /path/to/tilegroxy.yml
```

### Seed

A helper command to allow you to prepopulate your cache with prerendered tiles. This is especially useful when adding a new layer to tilegroxy that is slow to render the furthest out zoom levels and you want to avoid your first end-users running into this slowness. This command is roughly equivalent to standing up a server using the `serve` command and then hitting the layer endpoint with `cURL` requests for all the tiles you want.

Full, up-to-date usage information can be found with `tilegroxy seed -h`.

```
Pre-populates the cache for a given layer for a given area (bounding box) 
for a range of zoom levels. 

Be mindful that the higher the zoom level (the more you "zoom in"), 
exponentially more tiles will need to be seeded for a given area. For 
instance, while zoom level 1 only requires 4 tiles to cover the planet, 
zoom level 10 requires over a million tiles.

Example:

  tilegroxy seed -c test_config.yml -l osm -z 2 -v -t 7 -z 0 -z 1 -z 3 -z 4

Usage:
  tilegroxy seed [flags]

Flags:
      --force                   Perform the seeding even if it'll produce 
                                an excessive number of tiles. Normally 
                                seeds over 10k tiles will error out. 
                                Warning: Overriding this protection 
                                absolutely can cause an Out-of-Memory error
  -h, --help                    help for seed
  -l, --layer string            The ID of the layer to seed
  -n, --max-latitude float32    The maximum latitude to seed. The north 
                                side of the bounding box (default 90)
  -e, --max-longitude float32   The maximum longitude to seed. The east 
                                side of the bounding box (default 180)
  -s, --min-latitude float32    The minimum latitude to seed. The south 
                                side of the bounding box (default -90)
  -w, --min-longitude float32   The minimum longitude to seed. The west 
                                side of the bounding box (default -180)
  -t, --threads uint16          How many concurrent requests to use to 
                                perform seeding. Be mindful of spamming 
                                upstream providers (default 1)
  -v, --verbose                 Output verbose information including every 
                                tile being requested and success or error status
  -z, --zoom uints              The zoom level(s) to seed (default [0,1,2,
                                3,4,5])

Global Flags:
  -c, --config string           A file path to the configuration file to 
                                use. The file should have an extension of 
                                either json or yml/yaml and be readable. 
                                (default "./tilegroxy.yml")
```

### Config

The `tilegroxy config` command does nothing but contains two subcommands.

#### Check

Validates your supplied configuration.  

Full, up-to-date usage information can be found with `tilegroxy config check -h`.

```
Checks the validity of the configuration you supplied and then exits. If 
everything is valid the program displays "Valid" and exits with a code of 
0. If the configuration is invalid then a descriptive error is outputted 
and it exits with a non-zero status code.

Usage:
  tilegroxy config check [flags]

Flags:
  -e, --echo   Echos back the full parsed configuration including default 
               values if the configuration is valid
  -h, --help   help for check

Global Flags:
  -c, --config string   A file path to the configuration file to use. The 
                        file should have an extension of either json or 
                        yml/yaml and be readable. 
                        (default "./tilegroxy.yml")
```

#### Create

Helps create an initial configuration file. Still a work in progress.

Full, up-to-date usage information can be found with `tilegroxy config create -h`.

```
Creates either a JSON or YAML configuration with a skeleton you can use as 
a starting point for creating your configuration. 

Defaults to outputting to standard out, specify --output/-o to write to a 
file. Does not utilize --config/-c to avoid accidentally overwriting a 
configuration. If a file is specified this defaults to auto-detecting the 
format to use based on the file extension and ultimately defaults to YAML.

Example:
        tilegroxy config create --default --json -o tilegroxy.json

Usage:
  tilegroxy config create [flags]

Flags:
  -d, --default         Include all default configuration. TODO: make 
                        non-mandatory (default true)
  -h, --help            help for create
      --json            Output the configuration in JSON
      --no-pretty       Disable pretty printing JSON
  -o, --output string   Write the configuration to a file. This will 
                        overwrite anything already in the file
      --yaml            Output the configuration in YAML

Global Flags:
  -c, --config string   A file path to the configuration file to use. The 
                        file should have an extension of either json or 
                        yml/yaml and be readable. 
                        (default "./tilegroxy.yml")
```

### Test

Tests your layers and cache are correctly configured and working by performing end-to-end tests.

Full, up-to-date usage information can be found with `tilegroxy test -h`.

``` 
Tests that everything is working end-to-end for all or some layers 
including caching. This goes further than 'config check' and instead of 
just validating the configuration can be parsed it actually makes sample 
request(s) and populates the result in the cache. This is similar to 
running 'seed' for a single tile or standing up the server and making a 
cURL request for each layer. The output will list each layer and the 
status, with any error encountered if applicable.

This test uses an arbitrary tile coordinate to test with. The default 
coordinate might be outside the bounds of your map layer, there is 
currently no logic to consider the bounds configured for each layer; you 
will need to specify an applicable tile to use.  It is not recommended to 
use 0,0,0 due to potential performance issues when dealing with large 
data. If your cache is configured to prevent overwriting existing items 
you might need to pick a distinct tile each time you run the test or run 
with cache disabled (--no-cache).

Example:

        tilegroxy test -c test_config.yml -l osm -z 10 -x 123 -y 534

Usage:
  tilegroxy test [flags]

Flags:
  -h, --help                help for test
  -l, --layer strings       The ID(s) of the layer to test. Tests all 
                            layers by default
      --no-cache            Don't write to the cache. The Cache 
                            configuration must still be syntactically valid
  -t, --threads uint16      How many layers to test at once. Be mindful of 
                            spamming upstream providers (default 1)
  -x, --x-coordinate uint   The x coordinate to use to test (default 123)
  -y, --y-coordinate uint   The y coordinate to use to test (default 534)
  -z, --z-coordinate uint   The z coordinate to use to test (default 10)

Global Flags:
  -c, --config string   A file path to the configuration file to use. The 
                        file should have an extension of either json or 
                        yml/yaml and be readable. 
                        (default "./tilegroxy.yml")
```


## Custom Providers

For cases where the built-in providers don't suffice for your needs you can write your own custom providers with code that is loaded dynamically at runtime.  

Custom providers must be written in Go and are interpreted using [Yaegi](https://github.com/traefik/yaegi).  Yaegi offers a full featured implementation of the Go specification without the need to precompile.  

Example custom providers can be found within [examples/providers](./examples/providers/).  A custom provider must live within a single file and can call the entire standard library.  This includes potentially dangerous function calls such as `exec` and `unsafe`; be as cautious using custom providers from third party as you would be executing any other third party software.  

Custom providers must be within the `custom` package and must import the `tilegroxy/tilegroxy` package for mandatory datatypes. There are two mandatory functions:

```go
func preAuth(*internal.RequestContext, tilegroxy.ProviderContext, map[string]interface{}, tilegroxy.ClientConfig, tilegroxy.ErrorMessages) (tilegroxy.ProviderContext, error)

func generateTile(*internal.RequestContext, tilegroxy.ProviderContext, tilegroxy.TileRequest, map[string]interface{}, tilegroxy.ClientConfig,tilegroxy.ErrorMessages) (*tilegroxy.Image, error)
```

The `preAuth` function is responsible for authenticating outgoing requests and returning a token or whatever else is needed. It is called when needed by the application when either `expiration` is reached or an `AuthError` is returned by `generateTile`. A given instance of tilegroxy will only call this method once at a time and then shares the result among threads. However, ProviderContext is not shared between instances of tilegroxy. 

The `generateTile` function is the main function which returns an image for a given tile request. You should never trigger a call to `preAuth` yourself from `generateTile` (instead return an `AuthError`) to prevent excessive calls to the upstream provider from multiple tiles.

The following types are available for custom providers:

| Type | Description |
| --- | --- |
| [RequestContext](./internal/request_context.go) | Contains contextual information specific to the incoming request. Can retrieve headers via the Value method and authz information if configured properly. Do note there won't be a request when seed and test commands are run, this context will be a "Background Context" at those times |
| [ProviderContext](./internal/providers/provider.go) | A struct for on the fly, provider-specific information. It is primarily used to facilitate authentication. Includes an Expiration field to inform the application when to re-auth via the preAuth method (this should occur before auth actually expires). Also includes an auth token field, a auth Bypass field (for un-authed usecases), and a map |
| [TileRequest](./internal/tile_request.go) | The parameters from the user indicating the layer being requested as well as the specific tile coordinate |
| [ClientConfig](./internal/config/config.go) | A struct from the configuration which indicates settings such as static headers and timeouts. See `Client` in [Configuration documentation](./docs/configuration.md) for details |
| [ErrorMessages](./internal/config/config.go) | A struct from the configuration which indicates common error messages. See `Error Messages` in [Configuration documentation](./docs/configuration.md) for details |
| [Image](./internal/statics.go) | The imagery for a given tile. Currently type mapped to []byte |
| [AuthError](./internal/providers/provider.go) | An Error type to indicate an upstream provider returned an auth error that should trigger a new call to preAuth |
| [GetTile](./internal/providers/provider.go) | A utility method that performs an HTTP GET request to a given URL. Use this when possible to ensure all standard Client configurations are honored |

There is a performance cost of using a custom provider vs a built-in provider. The exact cost depends on the complexity of your provider, however it is typically below 10 milliseconds while tile generation as a whole is usually more than order of magnitude slower. Due to the custom providers being written in Go, it is easy to convert a custom provider to a built-in provider if your use-case is highly performance critical.

## Migrating from tilestache

An important difference between tilegroxy and tilestache is that tilegroxy can only be run as a standalone executable rather than running as a module in another webserver.  

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




## Troubleshooting

Please submit an [Issue](https://github.com/Michad/tilegroxy/issues/new) for any trouble you run into so we can build out this section.

**I have trouble running tests due to an error referencing docker or permissions**

This is most likely an issue due to your Docker installation.  There can be a number of issues at play depending on your OS and setup.  Some suggestions:

Make sure you have docker installed, the daemon is running, and your user has permission to use docker (is in the docker group).  If using Podman, ensure `podman.socket` is enabled both globally and for your `--user`.  If using Docker on Linux try temporarily setting `/var/run/docker.sock` world-writeable. If using Docker on a Mac, make sure colima is running. On Windows, ensure Docker Desktop is running.

If using a system with SELinux try temporarily disabling SELinux with `sudo setenforce 0` or running with "Ryuk" disabled by setting the env var `TESTCONTAINERS_RYUK_DISABLED=true`.


## Contributing

As this is a young project any contribution via an Issue or Pull Request is very welcome.

Please try to follow go conventions and the patterns you see elsewhere in the codebase.  Also, please use [semantic](https://gist.github.com/joshbuchea/6f47e86d2510bce28f8e7f42ae84c716) or [conventional](https://www.conventionalcommits.org/en/v1.0.0/) commit messages. If you want to make a fundamental change/refactor please open an Issue for discussion first.  

Very niche providers might be declined. Those are best suited as custom providers outside the core platform.