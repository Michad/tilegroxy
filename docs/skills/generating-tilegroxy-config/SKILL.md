---
name: generating-tilegroxy-config
description: Use when writing or editing a tilegroxy configuration file (YAML or JSON) - creating a new tilegroxy.yml, adding a layer/provider/cache/authentication/analytics/secret block, or troubleshooting a config validation error
---

# Generating Tilegroxy Configuration

## Overview

Tilegroxy is configured entirely through one YAML or JSON file. Unknown keys are
rejected, so a config either matches the schema exactly or fails to start. This skill
covers the schema shape and the entity pattern shared by providers, caches,
authentication, analytics, datastores, and secrets. This skill is the map, not the
territory: it does not stand in for the field-by-field reference, so always locate and
read the actual documentation pages before writing config, using the procedure below.

## Finding the documentation

This skill may be used outside a tilegroxy git checkout, so locate the docs with this
fallback chain, in order, stopping at the first that succeeds:

1. **Local checkout.** If `docs/operation/modules/ROOT/pages/` exists relative to the
   current working directory (or a parent of it), read `.adoc` files directly from
   there. This is the fastest path and always up to date with any local changes.
2. **tilegroxy.com.** Otherwise fetch the rendered docs from `https://tilegroxy.com/`.
   A local path `docs/operation/modules/ROOT/pages/<X>.adoc` corresponds to
   `https://tilegroxy.com/operation/<X>.html` (drop the `pages/` segment, change the
   extension). For example `configuration/provider/proxy.adoc` becomes
   `https://tilegroxy.com/operation/configuration/provider/proxy.html`. Start from
   `https://tilegroxy.com/operation/configuration/index.html` and follow links from
   there if unsure a page exists.
3. **GitHub, as a last resort.** If tilegroxy.com is unreachable, fetch the same `.adoc`
   source directly from GitHub:
   `https://raw.githubusercontent.com/Michad/tilegroxy/main/docs/operation/modules/ROOT/pages/<X>.adoc`.

Read every page relevant to the config being generated - the top-level structure page
plus one page per entity `name` actually used (each provider, cache, authentication
method, etc.) - before writing or editing YAML/JSON. Do not guess a parameter name or
default from memory or from this skill's summaries alone; confirm it against the page
for that specific entity.

## Top-level structure

All keys are optional except `layers`:

```yaml
server: ...           # configuration/server.adoc
client: ...           # default HTTP client settings for outbound provider calls
logging: ...          # main + access logs
telemetry: ...        # tracing/metrics
error: ...             # how errors are surfaced to end users
secret: ...            # external secret store
authentication: ...    # incoming request auth
cache: ...             # tile cache
datastores: [...]      # named DB connections, referenced by analytics
analytics: ...         # usage event recording
layers:                # required, at least one entry
  - id: ...
    provider: ...
```

Parameter names are case-insensitive. Keys are typically written lower camel-case in
examples but any case works.

## The entity pattern

`provider`, `cache`, `authentication`, `analytics`, `datastore`, and `secret` are all
"entities": pick an implementation with a required `name` field, and the rest of the
object's shape depends on that name. Example - a cache is either:

```yaml
cache:
  name: none
```

or

```yaml
cache:
  name: memory
  maxsize: 1000
  ttl: 1000
```

Never mix parameters from two different `name` values in one block. When an entity
nests another entity (provider inside `fallback`/`blend`, cache inside `multi`/`ttl`),
each nested block needs its own `name`.

Any string value in an entity block can pull from the environment (`env.VAR_NAME`) or
from a configured secret store (`secret.key-name`) instead of a literal. This does not
apply to fixed top-level scalars like `server.port` - those instead read a derived
environment variable (`SERVER_PORT`), which is documented per-page.

## Gathering requirements before generating

A config can't be written correctly from a vague request like "make me a tilegroxy
config." Before generating one, ask the user for whatever of the following isn't
already stated, since each answer picks a different entity and therefore a different
doc page to read:

- What data source(s) back each layer - a ZXY/TMS/WMTS tile server, a WMS endpoint, a
  PostGIS database, a static image, or something else? (drives provider choice)
- Does this need to combine or fall back between multiple sources for a layer? (drives
  whether `blend`/`fallback`/`crop` are needed)
- Where should tiles be cached, if at all - none, local disk/memory, or a shared store
  like Redis/S3/Memcache? Any expiration requirements?
- Does the server need to authenticate incoming requests, and if so how (open
  internally, a static bearer token, or JWT/OIDC)?
- Is this for local development/testing or a production deployment? (affects
  `server.production`, error mode, logging verbosity, and which cache/auth options are
  appropriate)
- Any secrets (API keys, credentials) involved, and if so, should they come from
  environment variables, a secret manager, or plain config?

Don't ask about settings that have a sane default and weren't mentioned - only ask what
materially changes which entity or page to read next.

## Building a config, in order

1. **Pick a cache.** `none` for no caching, `memory` for local dev, `disk`/`redis`/`s3`/`memcache`
   for real deployments, `multi` to tier a fast cache in front of a slow one, `ttl` to wrap
   any cache with expiration.
2. **Define layers.** Each layer needs `id` and `provider`. Start with one provider per
   layer; add `pattern`/`paramValidator`/`examples` only when the layer needs
   placeholder-based matching (see `layer.adoc`).
3. **Pick a provider per layer.** `proxy` (ZXY/TMS tile server) and `url_template` (WMS)
   are the common leaves. `ref`, `fallback`, `blend`, `crop`/`cropmvt`, `transform`,
   `effect` wrap or combine other providers - see `provider/index.adoc` for the full list.
   Avoid deep nesting; it has a real performance cost.
4. **Add authentication** if this isn't purely internal. `none` (default) accepts
   everything; `static_key` is a casual deterrent only; `jwt` is the real option, and
   supports either a static key or a JWKS endpoint.
5. **Add error handling** for production: `error.mode: image` (default) or
   `image+header` so failures render as a map tile instead of leaking a raw error to
   end users.
6. **Add secret/analytics/datastores** only as needed. `datastores` are named and
   referenced by id from `analytics` blocks that write to a database.

## Validating

Never hand-verify a config by eye when the tool exists:

```
tilegroxy config check -c path/to/config.yml
tilegroxy config check -c path/to/config.yml --echo   # also prints resolved config with defaults filled in
```

`tilegroxy config create --default --yaml -o tilegroxy.yml` scaffolds a starting file
with every default value spelled out, useful as a reference for available top-level
keys.

## Complete example

```yaml
cache:
  name: multi
  tiers:
    - name: memory
      maxsize: 1000
      ttl: 1000
    - name: disk
      path: "./disk_tile_cache"
authentication:
  name: jwt
  key: env.JWT_KEY
  algorithms:
    - HS256
error:
  mode: image
layers:
  - id: osm
    provider:
      name: proxy
      url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
  - id: watermarked
    provider:
      name: blend
      mode: normal
      providers:
        - name: ref
          layer: osm
        - name: static
          image: embedded:red.png
```

More worked examples, including pattern-based layers and WMS providers, live in
`examples/configurations/` in the tilegroxy repository - `complex.yml` and
`noaa_post_storm.yml` are the most instructive. If there's no local checkout, fetch
them from
`https://raw.githubusercontent.com/Michad/tilegroxy/main/examples/configurations/<name>.yml`.

## Common mistakes

- Adding a parameter that belongs to a different `name` value in the same entity
  (fails validation with an unknown-key error, not a silent ignore).
- Forgetting `name` on an entity block.
- Nesting `multi` cache inside another `multi` (explicitly unsupported).
- Using `secret.KEY` before a `secret` source is configured - it fails startup.
- Assuming a partial config is fine at request time - all validation happens at
  startup (`Initialize`), so a bad config fails fast with `config check` rather than
  misbehaving on the first request.
