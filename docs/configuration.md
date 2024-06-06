# Configuration

Tilegroxy is heavily configuration driven. This document describes the various configuration options available for defining the map layers you wish to serve up and various aspects about how you want the application to function.

Every configuration option that supports different "types" (such as authentication, provider, and cache) has a "name" parameter for selecting the type. Parameters keys and names should generally be in all lowercase.

## Layer

A layer represents a distinct mapping layer as would be displayed in a typical web map application.  Each layer can be accessed independently from other map layers. The main thing that needs to be configured for a layer is the provider described below. 

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| id | string | Yes | None | A url-safe identifier of the layer. Primarily used as a path parameter for incoming tile web requests |
| provider | Provider | Yes | None | See below |
| overrideclient | Client | No | None | A Client configuration to use for this layer specifically that overrides the Client from the top-level of the configuration. See below for Client schema | 

## Provider

A provider represents the underlying functionality that "provides" the tiles that make up the mapping layer.  This is most commonly an external HTTP(s) endpoint using either the "proxy" or "URL template" providers. Custom providers can be created to extract tiles from other sources.  

### Proxy

Proxy providers are the simplest option that simply forward tile requests to another HTTP(s) endpoint. This provider is primarily used for map layers that already return imagery in tiles: ZXY, TMS, or WMTS.  TMS inverts the y coordinate compared to ZXY and WMTS formats, which is handled by the InvertY parameter

Name should be "proxy"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| url | string | Yes | None | A URL pointing to the tile server. Should contain placeholders `{z}` `{x}` and `{y}` for tile coordinates |
| inverty | bool | No | false | Changes tile numbering to be South-to-North instead of North-to-South. |


### URL Template

URL Template providers are similar to the Proxy provider but are meant for endpoints that return mapping imagery via other schemes, primarily WMS. Instead of merely supplying tile coordinates, the URL Template provider will supply the bounding box.

Currently only supports EPSG:4326

Name should be "url template"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| template | string | Yes | None | A URL pointing to the tile server. Should contain placeholders `$xmin` `$xmax` `$ymin` and `$ymax` for tile coordinates |

## Cache

The cache configuration defines the datastores where tiles should be stored/retrieved. It's recommended when possible to make use of a multi-tiered cache with a smaller, faster "near" cache first followed by a larger, slower "far" cache.  

There is no universal mechanism for expiring cache entries. Some cache options include built-in mechanisms for applying an TTL and maximum size however some require an external cleanup mechanism if desired. Be mindful of this as some options may incur their own costs if allowed to grow unchecked.

### None

Disables the cache.  

Name should be "none" or "test"

### Multi

Implements a multi-tiered cache. 

When looking up cache entries each cache is tried in order. When storing cache entries each cache is called simultaneously. This means that the fastest cache(s) should be first and slower cache(s) last. As each cache needs to be tried before tile generation starts, it is not recommended to have more than 2 or 3 caches configured.

Name should be "multi"


Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| tiers | Cache[] | Yes | None | An array of Cache configurations. Multi should not be nested inside a Multi |


### Disks

Stores the cache entries as files in a location on the filesystem. 

If the filesystem is purely local then you will experience inconsistent performance if tilegroxy is deployed in a high-availability environment. If utilizing a networked filesystem then be mindful that cache writes are currently synchronous so delays from the filesystem will cause slower performance.

Files are stored in a flat structure inside the specified directory. No cleanup process is included inside of `tilegroxy` itself. It is recommended you use an external cleanup process to avoid running out of disk space.

Name should be "disk"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| path | string | Yes | None | The absolute path to the directory to store cache entries within. Directory (and tree) will be created if it does not already exist |
| filemode | uint32 | No | 0777 | A [Go filemode](https://pkg.go.dev/io/fs#FileMode) as an integer to use for all created files/directories. This might change in the future to support a more conventional unix permission notation |

### Memcache

TODO. Not yet implemented.

### Memory

A local in-memory cache. This stores the tiles in the memory of the tilegroxy daemon itself. 

**This is not recommended for production use.** It is meant for development and testing use-cases only. Setting this cache too high can cause stability issues for the service and this cache is not distributed so can cause inconsistent performance when deploying in a high-availability production environment.

Name should be "memory"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| maxsize | uint16 | No | 100 | Maximum number of tiles to hold in the cache. Setting this too high can cause out-of-memory panics |
| ttl | uint32 | No | 3600 | Maximum time to live for cache entries in seconds |

### Redis

Cache tiles using redis or another redis-compatible key-value store.  

Name should be "redis"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| host | String | No | 127.0.0.1 | The host of the redis server. A convenience equivalent to supplying `servers` with a single entry. Do not supply both this and `servers` |
| port | int | No | 6379 | The port of the redis server. A convenience equivalent to supplying `servers` with a single entry. Do not supply both this and `servers` |
| db | int | No | 0 | Database number, defaults to 0. Unused in cluster mode |
| keyprefix | string | No | None | A prefix to use for keys stored in cache. Serves a similar purpose as `db` in avoiding collisions when multiple applications use the same redis |
| username | string | No | None | Username to use to authenticate with redis |
| password | string | No | None | Password to use to authenticate with redis |
| mode | string | No | standalone | Controls operating mode of redis. Can be `standalone`, `ring` or `cluster`. Standalone is a single redis server. Ring distributes entries to multiple servers without any replication [(more details)](https://redis.uptrace.dev/guide/ring.html). Cluster is a proper redis cluster. |
| ttl | uint32 | No | 1 day | How long cache entries should persist for in seconds. Cannot be disabled. |
| servers | Array of `host` and `port` | No | host and port | The list of servers to connect to supplied as an array of objects, each with a host and key parameter. This should only have a single entry when operating in standalone mode. If this is unspecified it uses the standalone `host` and `port` parameters as a default, therefore this shouldn't be specified at the same time as those |

Example:

```json
{
    "name": "redis"
    "mode": "ring",
    "servers": [
        {
            "host": "127.0.0.1",
            "port": 6379
        },
        {
            "host": "127.0.0.1",
            "port": 6380
        }
    ],
    "ttl": 3600
}
```

### S3

Cache tiles as objects in an AWS S3 bucket.  

Ensure the user you're using has proper permissions for reading and writing objects in the bucket.  The permissions required are the minimal set you'd expect: GetObject and PutObject.  It's highly recommended to also grant ListBucket permissions, otherwise the log will contain misleading 403 error messages for every cache miss.  Also ensure the user has access to the KMS key if using bucket encryption.

If you're using a Directory Bucket AKA Express One Zone there's a few things to configure:
* Ensure `storageclass` is set to "EXPRESS_ONEZONE" 
* The bucket contains the full name including suffix. For example: `my-tilegroxy-cache--use1-az6--x-s3`
* An endpoint is configured in the format "https://s3express-{az_id}.{region}.amazonaws.com" For example: "https://s3express-use1-az6.us-east-1.amazonaws.com"

Name should be "s3"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| bucket | string | Yes | None | The name of the bucket to use |
| path | string | No | / | The path prefix to use for storing tiles |
| region | string | No | None | The AWS region containing the bucket. Required if region is not specified via other means. Consult [AWS documentation](https://docs.aws.amazon.com/general/latest/gr/rande.html#regional-endpoints) for possible values |
| access | string | No | None | The AWS Access Key ID to authenticate with. This is not recommended; it is offered as a fallback authentication method only. Consult [AWS documentation](https://docs.aws.amazon.com/cli/v1/userguide/cli-chap-authentication.html) for better options |
| secret | string | No | None | The AWS Secret Key to authenticate with. This is not recommended; it is offered as a fallback authentication method only. Consult [AWS documentation](https://docs.aws.amazon.com/cli/v1/userguide/cli-chap-authentication.html) for better options |
| profile | string | No | None | The profile to use to authenticate against the AWS API. Consult [AWS documentation for specifics](https://docs.aws.amazon.com/sdkref/latest/guide/file-format.html#file-format-profile) |
| storageclass | string | No | STANDARD | The storage class to use for the object. You probably can leave this blank and use the bucket default. Consult [AWS documentation](https://aws.amazon.com/s3/storage-classes/) for an overview of options. The following are currently valid: STANDARD REDUCED_REDUNDANCY STANDARD_IA ONEZONE_IA INTELLIGENT_TIERING GLACIER DEEP_ARCHIVE OUTPOSTS GLACIER_IR SNOW EXPRESS_ONEZONE |
| endpoint | string | No | AWS Auto | Override the S3 API Endpoint we talk to. Useful if you're using S3 outside AWS or using a directory bucket |

## Authentication

Implements incoming authentication schemes. 

These authentication options are not comprehensive and do not support authorization. That is, anyone who authenticates can access all layers. For complex use cases it is recommended to implement authentication and authorization in compliance with your business logic as a proxy/gateway before tilegroxy.

Requests that do not comply with authentication requirements will receive a 401 Unauthorized HTTP status code.

### None

No incoming authentication, all requests are allowed. Ensure you have an external authentication solution before exposing this to the internet.

Name should be "none"

### Static Key

Requires incoming requests have a specific key supplied as a "Bearer" token in a "Authorization" Header.

It is recommended you employ caution with this option. It should be regarded as a protection against casual web scrapers but not true security. It is recommended only for development and internal ("intranet") use-cases.

Name should be "static key"

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| key | string | No | Auto | The bearer token to require be supplied. If not specified `tilegroxy` will generate a random token at startup and output it in logs |

### JWT

Requires incoming requests include a [JSON Web Token (JWT)](https://jwt.io/). The signature of the token is verified against a fixed secret and grants are validated.

Currently this implementation only supports a single key specified in configuration against a single signing algorithm. Expanding that to allow multiple keys and keys pulled from secret stores is a desired future roadmap item.

Name should be "jwt"


Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| VerificationKey | string | Yes | None | The key for verifying the signature. The public key if using asymmetric signing. |
| Algorithm | string | Yes | None | Algorithm to allow for JWT signature. One of: "HS256", "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512", "EdDSA" |
| HeaderName | string | No | Authorization | The header to extract the JWT from. If this is "Authorization" it removes "Bearer " from the start |
| MaxExpirationDuration | uint32 | No | 1 day | How many seconds from now can the expiration be. JWTs more than X seconds from now will result in a 401 |
| ExpectedAudience | string | No | None | Require the "aud" grant to be this string |
| ExpectedSubject | string | No | None | Require the "sub" grant to be this string |
| ExpectedIssuer | string | No | None | Require the "iss" grant to be this string |

### External

TODO. Not yet implemented.

## Server

Configures how the HTTP server should operate

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| BindHost | string | No | 127.0.0.1 | IP address to bind HTTP server to |
| Port | int | No | 8080 | Port to bind HTTP server to |
| ContextRoot | string | No | /tiles | The root HTTP Path to serve tiles under. The default of /tiles will result in a path that looks like /tiles/{layer}/{z}/{x}/{y} |
| StaticHeaders | map[string]string | No | None | Include these headers in all response from server |
| Production | bool | No | false | Hardens operation for usage in production. For instance, controls serving splash page, documentation, x-powered-by header. |
| Timeout | uint | No | 60 | How long (in seconds) a request can be in flight before we cancel it and return an error |
| Gzip | bool | No | false | Whether to gzip compress HTTP responses |


## Client

Configures how the HTTP client should operate for tile requests that require calling an external HTTP(s) server.
 
Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| UserAgent | string | No | tilegroxy/VERSION | The user agent to include in outgoing http requests. |
| MaxResponseLength | int | No | 10 MiB | The maximum Content-Length to allow incoming responses | 
| AllowUnknownLength | bool | No | false | Allow responses that are missing a Content-Length header, this could lead to excessive memory usage |
| AllowedContentTypes | string[] | No | image/png, image/jpg | The content-types to allow remote servers to return. Anything else will be interpreted as an error |
| AllowedStatusCodes | int[] | No | 200 | The status codes from the remote server to consider successful |
| StaticHeaders | map[string]string | No | None | Include these headers in requests |

## Log

Configures how the application should log during operation.

### Main Log

Configures application log messages

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| EnableStandardOut | bool | No | true | Whether to write application logs to standard out |
| Path | string | No | None | The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth |
| Format | string | No | plain | The format to output application logs in. Applies to both standard out and file out. Possible values: plain, json |
| Level | string | No | info | The most-detailed log level that should be included. Possible values: debug, info, warn, error |

### Access Log

Configures logs for incoming HTTP requests. Primarily outputs in standard Apache Access Log formats.

Configuration options:

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| EnableStandardOut | bool | No | true | Whether to write access logs to standard out |
| Path | string | No | None | The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth |
| Format | string | No | common | The format to output access logs in. Applies to both standard out and file out. Possible values: common, combined |

