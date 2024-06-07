# tilegroxy


[![Docker Image CI](https://github.com/Michad/tilegroxy/actions/workflows/docker-image.yml/badge.svg)](https://github.com/Michad/tilegroxy/actions/workflows/docker-image.yml)
[![Go Report Card](https://goreportcard.com/badge/michad/tilegroxy)](https://goreportcard.com/report/michad/tilegroxy)
[![CodeQL](https://github.com/Michad/tilegroxy/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/Michad/tilegroxy/actions/workflows/github-code-scanning/codeql)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0) [![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md) 

A map tile proxy and cache service. Lives between your webmap and your mapping engines to provide a simple, consistent interface and improved performance.

💡 Inspired by [tilestache](https://github.com/tilestache/tilestache) and mostly compatible with tilestache configurations.   
🚀 Built in Go for speed.  
🔌 Features a flexible plugin system for custom providers written in TODO.  
🛠️ BUT DO NOT USE YET! STILL A WORK IN PROGRESS!




## Features

The following features are currently available:

* Provide a uniform ZXY mapping interface for incoming requests.
* Proxy map tiles to ZXY, WMS, TMS, or WMTS backed map layers
* Cache map tiles in disk, memory, s3, redis ...
* Generic support for any content type 
* Incoming authentication using a static key or JWT
* Configurable timeout, logging, and error handling rules

The following are on the roadmap:

* Support for raster image reprocessing/combination on the fly
* Custom providers
* Proxy map layers directly to providers such as Mapnik, Mapserver 
* Specific support for vector tile formats such as [MVT](https://github.com/mapbox/vector-tile-spec) or tiled GeoJSON
* OpenTelemetry support
* Support for external secret stores such as AWS Secrets Manager to avoid secrets in the configuration
* Support for external configuration sources 
* Support for HTTPS server w/ Let's Encrypt or static certs


## Configuration

Tilegroxy is designed to be supplied with a declarative configuration that defines your various map layers as well as static parameters such as incoming authentication, cache connections, HTTP client configuration, and logging.  

The configuration currently must be supplied as a single file upfront.  Loading configuration from external services or hot-loading configuration is planned but not yet supported.

Documentation of the various configuration options can be found [here](./docs/configuration.md).

Example configurations are located under [examples](./examples/configurations/). You can also use `tilegroxy config create` to help get started.

## How to get

Tilegroxy is recommended to be installed and run through a container with the only requirement being a mapped configuration file. It can also be run directly for the old-school approach.  It is primarily meant for use in \*nix environments. Building and running on Windows should work but is currently untested.

### Standalone

Tilegroxy builds as a standalone executable that can be placed inside `/usr/local/bin` to install. Prebuilt binaries are available at TODO.

Building it yourself requires go 1.22+ and is quite simple:

```
go test ./... 
go build
./tilegroxy version
```

Once built, tilegroxy can be run directly as an HTTP server via the `tilegroxy serve` command documented below. It's recommended to create a systemd unit file to allow it to run as a daemon as an appropriate user.

### Docker

Tilegroxy is available as a container image on TODO

You can build the docker image yourself with

```
docker build -f build/dockerfile . -t tilegroxy
```

To run tilegroxy from within a container:

```
docker run -it --rm -v ./test_config.yml:/tilegroxy/tilegroxy.yml:Z \
tilegroxy seed -l osm -z 0 -v
```

To run it through docker compose:

TODO


### Kubernetes

TODO. Not yet implemented.


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

TODO. Not yet implemented.

## Migrating from tilestache

An important difference between tilegroxy and tilestache is that tilegroxy can only be run as a standalone executable rather than running as a module in another webserver.  

The configuration in tilegroxy is meant to be highly compatible with the configuration of tilestache, however there are significant differences.  The tilegroxy configuration supports a variety of options that are not available in tilestache and while we try to keep most parameters optional and have sane and safe defaults, it is highly advised you familiarize yourself with the various options documented above.

The following are the known incompatibilities with tilestache configurations:

* Unsupported providers:
    * Mapnik
    * Vector
    * MBTiles
    * Mapnik Grid
    * Sandwich
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
* URL Template provider:
    * No `referer` parameter - instead specify the referer header via the `Client` configuration
    * No `timeout` parameter - instead specify the timeout via the `Client` configuration
    * No `source projection` parameter - Might be added in the future
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
* Redis cache supports a wider variety of configuration options. It's recommended but not required that you consider utilizing a Cluster or Ring deployment if you previously used a single server.
* S3 cache:
    * No `use_locks` parameter - Caches are currently lockless
    * No `reduced_redundancy` parameter - Instead use the more flexible `storageclass` parameter with the "REDUCED_REDUNDANCY" option
    * While supported, it's recommended you don't use the `access` and `secret` parameters. All standard methods of supplying AWS credentials are supported.




## Troubleshooting

Please submit an [Issue](https://github.com/Michad/tilegroxy/issues/new) for any trouble you run into so we can build out this section.

## Contributing

As this is a young project any contribution via an Issue or Pull Request is very welcome without too much process.

Please try to follow go conventions and the patterns you see elsewhere in the codebase.  Also, please use [semantic](https://gist.github.com/joshbuchea/6f47e86d2510bce28f8e7f42ae84c716) or [conventional](https://www.conventionalcommits.org/en/v1.0.0/) commit messages. If you want to make a fundamental change/refactor please open an Issue for discussion first.  

Very specific providers might be declined if it seems highly unlikely they can/will be reused. Those are best suited as custom providers outside the core platform.