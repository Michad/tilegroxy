# Extension Example

This folder is a standalone Go module showing how to build your own tilegroxy executable with native entities, as described in the extensibility documentation. It implements a sample version of every entity type and a wrapper executable, `my_tg_wrapper`, that always uses the `tilegroxy.yml` beside it, runs the `test` command against every layer, and then starts serving.

`make e2e` from the repository root builds and runs this.

## Structure

`main.go` is the wrapper executable. It imports `sample` for its registrations and then calls `cmd.ExecuteArgs` once for `test` and once for `serve`

`sample` is a package with one file per entity type. Each registers itself from `init()`:

* `provider.go` renders a solid color tile
* `cache.go` stores tiles in the sample datastore
* `datastore.go` shares an in-memory map with anything referencing its ID
* `secret.go` resolves `secret.` references from values held in its own configuration
* `authentication.go` accepts requests carrying a fixed key in a header and records a user ID
* `analytics.go` appends each event to a file as a line of JSON
* `check.go` is a health check that renders a tile through a layer, bypassing the cache

`tilegroxy.yml` is the configuration file, using only the sample entities

There's no `go.mod` or `go.sum` checked in to avoid needing to maintain the files as dependencies change. Running `make extension` will generate these files. In your own project, run `go mod init` with your module path, update the `sample` import in `main.go` to match, and `go get github.com/Michad/tilegroxy` to build against a released version

## Running

```
make extension
./examples/extension/my_tg_wrapper
curl -H "X-Api-Key: hunter2" -o tile.png http://localhost:8089/tiles/color/3/2/1
```

Build with `-tags viper_bind_struct`, as the Makefile does, so environment variables can override any configuration key the same way they do for the official binary. `SERVER_PORT` and `HEALTH_PORT` change the ports, for example.

The analytics file is written relative to the working directory.
