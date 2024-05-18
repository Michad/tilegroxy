# tilegroxy

A map tile proxy and cache service. Designed to live between your map and mapping services. Inspired by [tilestache](https://github.com/tilestache/tilestache) and designed to be mostly compatible with tilestache configurations. Built in Go for performance.

Features a flexible plugin system for custom providers written in TODO.  Supports workflows that require pre-request authentication.

DO NOT USE YET! NOT READY


## How to run

### Standalone

### Docker


### Kubernetes



## Configuration

This application is heavily configuration driven. It is designed to be supplied with a configuration block that defines your various map layers as well as static configuration such as incoming authentication, cache connection, HTTP client configuration, and logging.  The configuration currently must be supplied as a single file upfront.  Loading configuration from external services or hot-loading configuration is planned but not yet supported.

### Layer

#### Provider

##### URL Template

### Cache

### Authentication

### 

 

## Custom Providers




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

