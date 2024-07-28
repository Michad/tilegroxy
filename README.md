# tilegroxy    
[![Docker Image CI](https://github.com/Michad/tilegroxy/actions/workflows/docker-image.yml/badge.svg)](https://github.com/Michad/tilegroxy/actions/workflows/docker-image.yml) [![Go Report Card](https://goreportcard.com/badge/michad/tilegroxy)](https://goreportcard.com/report/michad/tilegroxy) [![OpenSSF Scorecard](https://img.shields.io/ossf-scorecard/github.com/Michad/tilegroxy?label=openssf%20scorecard&style=flat)](https://scorecard.dev/viewer/?uri=github.com%2FMichad%2Ftilegroxy) ![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/michad/d1b9e082f6608635494188d0f52bae69/raw/coverage.json) [![Libyears](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/michad/d1b9e082f6608635494188d0f52bae69/raw/libyears.json)](https://libyear.com/)    
![Go Version](https://img.shields.io/github/go-mod/go-version/michad/tilegroxy) [![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0) [![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md) 

Tilegroxy lives between your map and your mapping providers to deliver a consistent, cached API for all your layers. 

🚀 Built in Go.  
🔌 Features a flexible plugin system powered by [Yaegi](https://github.com/traefik/yaegi).  
💡 Inspired by [tilestache](https://github.com/tilestache/tilestache)   
🛠️ This project is still a work in progress. Changes may occur prior to the 1.0 release.

## Why tilegroxy?

Tilegroxy shines when you consume maps from multiple sources.  It isn't tied to any one mapping backend and can pull data from any protocol, whether the standard alphabet soup or a proprietary, authenticated API. Rather than make your frontend aware of every single vendor and exposing your keys, utilize tilegroxy and provide a uniform API with a configuration-driven backend that can be augmented by code when necessary.  

### Features:

* Provide a uniform interface for serving map layers
* Proxy to ZXY, WMS, TMS, WMTS, or other protocol map layers
* Cache tiles in disk, memory, s3, redis, and/or memcached
* Require authentication using static key, JWT, or custom logic
* Restrict access to a given layer and/or geographic region based on auth token
* Create your own custom provider to pull in non-standard and proprietary imagery sources
* Tweak your map layer with 18 standard effects or by providing your own pixel-level logic
* Combine multiple map layers with adjustable rules and blending methods
* Act as an HTTP server for [MapServer](https://www.mapserver.org) and any other CGI application that generates tiles
* Commands for seeding and testing your layers
* Support for both raster and vector format tiles
* Run as HTTPS including Let's Encrypt support
* Configurable timeout, logging, and error handling rules
* Override configuration via environment variables
* Externalize passwords/keys using AWS Secrets Manager
* Container deployment

The following are on the roadmap and expected before a 1.0 release:

* OpenTelemetry support
* Example k8s deployment file

## Configuration

Configuration is required to define your layers, cache, authentication, and service operation.  The configuration should be supplied as a JSON or YAML file either directly or through an external service such as etcd or consul. Configuration can also be partially supplied via Environment Variables. 

Details can be found in [Configuration documentation](./docs/configuration.md) or through [examples](./examples/configurations/). For help converting from tilestache see the documentation on [Migrating From Tilestache](./docs/migrate-tilestache.md).

You can also use `tilegroxy config create` to help get started.

## How to get it

Tilegroxy is designed to be run in a container. But it can also be run directly for that old-school approach; we don't judge.   

### Building

Tilegroxy builds as a statically linked executable. Prebuilt binaries are available from [Github](https://github.com/Michad/tilegroxy/releases).

Building tilegroxy yourself requires `go`, `git`, `make`, and `date`.  It uses a standard [Makefile](./Makefile) workflow:

Build with

```
make
```

then install with

```
sudo make install
```

Once installed, tilegroxy can be run directly as an HTTP server via the `tilegroxy serve` command documented below. It's recommended to create a systemd unit file to allow it to run as a daemon as an appropriate user.

#### Tests

The build includes integration tests that use [testcontainers](https://golang.testcontainers.org/).  This requires you have either docker or podman installed and running. If you encounter difficulties running these tests it's recommended you use a prebuilt binary.  That said, you can also build with just unit tests using:

```
make clean build unit
```

### Docker

Tilegroxy is available as a container image on the Github container repository.  

You can pull the most recent versioned release with the `latest` tag and the very latest (and maybe buggy) build with the `edge` tag. Tags are also available for version numbers.  [See here for a full list](https://github.com/Michad/tilegroxy/pkgs/container/tilegroxy).

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

The following global flags are available for supplying your configuration:

```
  -c, --config string            A file path to the configuration file to use. 
                                 The file should have an extension of either 
                                 json or yml/yaml and be readable. 
                                 (default "./tilegroxy.yml")
      --remote-endpoint string   The endpoint to use to connect to the remote 
                                 provider (default "http://127.0.0.1:2379")
      --remote-path string       The path to use to select the configuration 
                                 on the remote provider 
                                 (default "/config/tilegroxy.yml")
      --remote-provider string   The provider to pull configuration from. 
                                 One of: etcd, etcd3, consul, firestore, nats
      --remote-type string       The file format to use to parse the configuration 
                                 from the remote provider (default "yaml")
```

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

```

### Config

The `tilegroxy config` command contains two subcommands.

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

```

## Extending tilegroxy

One of the top design goals of tilegroxy is to be highly flexible. If there's functionality you need, there's a couple different ways you can add it in.  See the [extensibility documentation](./docs/extensibility.md) for instructions.

## Troubleshooting

Please submit an [Issue](https://github.com/Michad/tilegroxy/issues/new) for any trouble you run into so we can build out this section.

**I have trouble running tests due to an error referencing docker or permissions**

This is most likely an issue due to your Docker installation.  There can be a number of issues at play depending on your OS and setup.  Some suggestions:

Make sure you have docker installed, the daemon is running, and your user has permission to use docker (is in the docker group).  If using Podman, ensure `podman.socket` is enabled both globally and for your `--user`.  If using Docker on Linux try temporarily setting `/var/run/docker.sock` world-writeable. If using Docker on a Mac, make sure colima is running. On Windows, ensure Docker Desktop is running.

If using a system with SELinux try temporarily disabling SELinux with `sudo setenforce 0` or running with "Ryuk" disabled by setting the env var `TESTCONTAINERS_RYUK_DISABLED=true`.


## Contributing

As this is a young project any contribution via an Issue or Pull Request is very welcome.

A few please and thank yous: 

* Follow [go conventions](https://go.dev/doc/effective_go) and the patterns you see elsewhere in the codebase.  Linters are configured in Github Actions, they can be run locally with `make lint`
* Use [semantic](https://gist.github.com/joshbuchea/6f47e86d2510bce28f8e7f42ae84c716)/[conventional](https://www.conventionalcommits.org/en/v1.0.0/) commit messages. 
* Open an issue for discussion before making large, fundamental change/refactors
* Ensure you add tests. You can use `make coverage` to ensure you're not dropping coverage.

Very niche providers might be declined. Those are best suited as custom providers outside the core platform.
