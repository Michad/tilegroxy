# tilegroxy

A map tile proxy and cache service.

🎨 Designed to live between your map and mapping services.  
💡 Inspired by [tilestache](https://github.com/tilestache/tilestache) and mostly compatible with tilestache configurations.   
🚀 Built in Go for speed.  
🔌 Features a flexible plugin system for custom providers written in TODO.  
🛠️ BUT DO NOT USE YET! STILL A WORK IN PROGRESS!

## Features

The following features are currently available:

* Provide a uniform ZXY mapping interface for incoming requests.
* Proxy map tiles to ZXY, WMS, WMTS backed map layers
* Cache map tiles in disk, memory, ...
* Generic support for any content type 
* Incoming authentication using a static key or JWT
* Configurable timeout, logging, and error handling rules

The following are on the roadmap:

* Proxy map layers to TMS, QuadKey map layers
* Specific support for vector tile formats such as [MVT](https://github.com/mapbox/vector-tile-spec) or tiled GeoJSON
* Proxy map layers directly to providers such as Mapnik, Mapserver 
* Support for raster image reprocessing/combination on the fly
* Custom providers
* OpenTelemetry support


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
docker run -it --rm -v ./test_config.yml:/tilegroxy/tilegroxy.yml:Z localhost/tilegroxy seed -l osm -z 0 -v
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
Pre-populates the cache for a given layer for a given area (bounding box) for a range of zoom levels. 

Be mindful that the higher the zoom level (the more you "zoom in"), exponentially more tiles will need to be seeded for a given area. For instance, while zoom level 1 only requires 4 tiles to cover the planet, zoom level 10 requires over a million tiles.

Example:

  tilegroxy seed -c test_config.yml -l osm -z 2 -v -t 7 -z 0 -z 1 -z 3 -z 4

Usage:
  tilegroxy seed [flags]

Flags:
      --force                   Perform the seeding even if it'll produce an excessive number of tiles. Normally seeds over 10k tiles will error out. 
                                Warning: Overriding this protection absolutely can cause an Out-of-Memory error
  -h, --help                    help for seed
  -l, --layer string            The ID of the layer to seed
  -n, --max-latitude float32    The maximum latitude to seed. The north side of the bounding box (default 90)
  -e, --max-longitude float32   The maximum longitude to seed. The east side of the bounding box (default 180)
  -s, --min-latitude float32    The minimum latitude to seed. The south side of the bounding box (default -90)
  -w, --min-longitude float32   The minimum longitude to seed. The west side of the bounding box (default -180)
  -t, --threads uint16          How many concurrent requests to use to perform seeding. Be mindful of spamming upstream providers (default 1)
  -v, --verbose                 Output verbose information including every tile being requested and success or error status
  -z, --zoom uints              The zoom level(s) to seed (default [0,1,2,3,4,5])

Global Flags:
  -c, --config string   A file path to the configuration file to use. The file should have an extension of either json or yml/yaml and be readable. (default "./tilegroxy.yml")
```

### Config

The `tilegroxy config` command does nothing but contains two subcommands.

#### Check

Validates your supplied configuration.  

Full, up-to-date usage information can be found with `tilegroxy config check -h`.

```
Checks the validity of the configuration you supplied and then exits. If everything is valid the program displays "Valid" and exits with a code of 0. If the configuration is invalid then a descriptive error is outputted and it exits with a non-zero status code.

Usage:
  tilegroxy config check [flags]

Flags:
  -e, --echo   Echos back the full parsed configuration including default values if the configuration is valid
  -h, --help   help for check

Global Flags:
  -c, --config string   A file path to the configuration file to use. The file should have an extension of either json or yml/yaml and be readable. (default "./tilegroxy.yml")
```

#### Create

Helps create an initial configuration file. Still a work in progress.

Full, up-to-date usage information can be found with `tilegroxy config create -h`.

```
Creates either a JSON or YAML configuration with a skeleton you can use as a starting point for creating your configuration. 

Defaults to outputting to standard out, specify --output/-o to write to a file. Does not utilize --config/-c to avoid accidentally overwriting a configuration. If a file is specified this defaults to auto-detecting the format to use based on the file extension and ultimately defaults to YAML.

Example:
        tilegroxy config create --default --json -o tilegroxy.json

Usage:
  tilegroxy config create [flags]

Flags:
  -d, --default         Include all default configuration. TODO: make this non-mandatory (default true)
  -h, --help            help for create
      --json            Output the configuration in JSON
      --no-pretty       Disable pretty printing JSON
  -o, --output string   Write the configuration to a file. This will overwrite anything already in the file
      --yaml            Output the configuration in YAML

Global Flags:
  -c, --config string   A file path to the configuration file to use. The file should have an extension of either json or yml/yaml and be readable. (default "./tilegroxy.yml")
```


## Custom Providers

TODO. Not yet implemented.

## Migrating from tilestache

The configuration in tilegroxy is meant to be highly compatible with the configuration of tilestache, however there are significant differences.  The tilegroxy configuration supports a variety of options that are not available in tilestache and while we try to keep most parameters optional and have sane and safe defaults, it is highly advised you familiarize yourself with the various options documented above.

The following are the known steps to transition a configuration from tilestache to tilegroxy:

* Unsupported providers:
* Unsupported params url template
* moved params client params
* Names are always in all lowercase
* Disk cache umode to filemode changes
* 




## Troubleshooting

## Contributing

