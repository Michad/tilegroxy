# tilegroxy Helm chart

Deploys [tilegroxy](https://github.com/Michad/tilegroxy) — a map tile proxy, cache and server — on Kubernetes.

## Installing

```sh
helm install tilegroxy ./charts/tilegroxy -f my-values.yaml
```

## How this chart is organized

Tilegroxy is configuration driven, and this chart doesn't try to wrap that configuration in
per-option values. Instead, the entire tilegroxy config goes under a single `config` key and is
rendered verbatim into a ConfigMap mounted at `configPath`:

```yaml
config:
  server:
    port: 8080
    production: true
    health:
      enabled: true
      checks:
        - name: cache
  cache:
    name: redis
    host: redis.example.svc.cluster.local
  layers:
    - id: osm
      provider:
        name: proxy
        url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
```

Anything in the [configuration reference](https://tilegroxy.com/operation/configuration/index.html)
is valid there. The chart itself only reads `server.port`, `server.health` and `server.encrypt`
to wire up container ports, the Service and the probes.

Everything outside `config` is ordinary Kubernetes deployment concerns: replicas, resources,
ingress, autoscaling, storage, scheduling.

## Secrets

Don't put credentials in `config` — it lands in a ConfigMap. Tilegroxy resolves any string
parameter written as `env.VAR_NAME` from the environment, so reference a variable in the config
and supply it as a secret:

```yaml
config:
  datastores:
    - name: postgresql
      id: db
      host: postgresql
      user: postgres
      password: env.TG_PGPASSWORD

# Managed by this chart (ends up in a Secret this chart creates):
secrets:
  TG_PGPASSWORD: hunter2

# Or, preferably, from a Secret you manage elsewhere:
existingSecretKeys:
  TG_PGPASSWORD:
    name: my-db-secret
    key: password
```

Both forms are injected into the server and any seed Jobs. Cloud credentials for the S3 cache
(`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`) work the same way, as does
[an external secret store](https://tilegroxy.com/operation/configuration/secret/index.html)
if you'd rather have tilegroxy fetch secrets itself.

### AWS

Tilegroxy's own AWS calls — the S3 cache and the AWS Secrets Manager secret source — use the
standard AWS credential chain, so on EKS neither needs credentials in the config at all. Annotate
the service account for IRSA and set the region, and make sure the token is mounted (the chart
turns it off by default):

```yaml
serviceAccount:
  automountServiceAccountToken: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/tilegroxy

env:
  AWS_REGION: us-east-1
```

With a Secrets Manager source configured, every other credential in the config (database
passwords, upstream API keys, the JWT verification key) can be a `secret.<name>` reference, so
nothing sensitive reaches the ConfigMap and the chart's `secrets` value goes unused entirely.
See [examples/aws.yaml](examples/aws.yaml) for the full picture, including the IAM permissions
required.

## Health checks and probes

Probes are only created when `config.server.health.enabled` is true, since they hit the health
port. `/` backs the liveness probe and `/health` backs readiness, so the configured checks decide
whether a pod receives traffic. Adding a `tile` check takes a pod out of the Service when an
upstream provider is failing:

```yaml
config:
  server:
    health:
      enabled: true
      port: 3000
      checks:
        - name: cache
          delay: 60
        - name: tile
          layer: osm
          validation: success
          delay: 60
```

The health port is never added to the Service, per the
[health documentation](https://tilegroxy.com/operation/configuration/health.html)'s
recommendation that it not be exposed.

A `config check` init container runs before the server starts (`configCheck.enabled`, on by
default) so an invalid config fails the rollout instead of crash looping.

## TLS

Two options:

- **Terminate upstream** (typical): leave `config.server.encrypt` unset and use `ingress` or your
  own load balancer. The Service exposes a single `http` port.
- **Terminate in tilegroxy**: set `config.server.encrypt`. The Service then exposes `https` on
  `service.tlsPort` targeting the main port, plus, when `server.encrypt.httpport` is set, an
  `http` port for the ACME challenge and redirect. With Let's Encrypt, point
  `server.encrypt.cache` at a directory on a persistent volume, and use a `ReadWriteMany` volume
  if you run more than one replica so they share the certificate cache.

## Storage

`persistence` creates one PVC mounted at `persistence.mountPath` (default `/tilegroxy/data`) on
both the server and seed Jobs. Use it for a disk cache, a Let's Encrypt certificate cache, or CGI
data, and point the matching config paths inside it. The container runs with a read-only root
filesystem, so anything tilegroxy writes must live on this volume (or under `/tmp`, which the
default `extraVolumes` provides).

A disk cache on a `ReadWriteOnce` volume is per-pod. For multiple replicas use `ReadWriteMany`, or
better, a redis, memcached or s3 cache.

## Seeding

`seed.enabled` renders one Job per entry in `seed.layers`, running `tilegroxy seed`. By default
these are `post-install,post-upgrade` Helm hooks so they run after the new config is in place:

```yaml
seed:
  enabled: true
  layers:
    - id: osm
      zoom: [0, 1, 2, 3, 4, 5]
      threads: 4
      bounds:
        minLongitude: -125
        minLatitude: 24
        maxLongitude: -66
        maxLatitude: 50
```

Seeding over 10k tiles requires `force: true`; be mindful this can OOM and that it hammers
upstream providers.

## Configuration reloading

`hotReload` passes `--hot-reload`, letting tilegroxy pick up changes to layers, caches,
authentication and datastores without a restart. It's off by default: core sections such as
`server` still require a restart, so `restartOnConfigChange` (which stamps a config checksum on
the pod template and triggers a normal rollout) is the more predictable path. See the
[reloading documentation](https://tilegroxy.com/operation/reloading.html) for the
caveats, particularly on NFS and other filesystems without change notification.

## Values

| Key | Default | Description |
| --- | --- | --- |
| `config` | a proxy of openstreetmap.org | The tilegroxy configuration, rendered to a ConfigMap |
| `existingConfigMap` | `""` | Mount a ConfigMap you manage instead of rendering `config` |
| `configPath` | `/tilegroxy/config/tilegroxy.yml` | Where the config is mounted |
| `hotReload` | `false` | Pass `--hot-reload` |
| `restartOnConfigChange` | `true` | Roll pods when the rendered config changes |
| `configCheck.enabled` | `true` | Validate the config in an init container |
| `image.repository` | `ghcr.io/michad/tilegroxy` | Image repository |
| `image.tag` | chart `appVersion` | Image tag |
| `image.digest` | `""` | Pin by digest instead of tag |
| `replicaCount` | `2` | Replicas, ignored when autoscaling |
| `env` | `{}` | Plain environment variables |
| `secrets` | `{}` | Secret values this chart creates and injects |
| `existingSecretKeys` | `{}` | Environment variables sourced from existing Secrets |
| `extraEnv` | `[]` | Raw env entries |
| `service.type` | `ClusterIP` | Service type |
| `service.port` | `80` | Service HTTP port |
| `service.tlsPort` | `443` | Service HTTPS port, used when `config.server.encrypt` is set |
| `ingress.enabled` | `false` | Create an Ingress |
| `ingress.className` | `""` | IngressClass name |
| `ingress.hosts` | one example host | Ingress hosts and paths |
| `ingress.tls` | `[]` | Ingress TLS blocks |
| `resources` | 500m/512Mi requests | Container resources |
| `autoscaling.enabled` | `false` | Create an HPA |
| `persistence.enabled` | `false` | Create and mount a PVC |
| `persistence.mountPath` | `/tilegroxy/data` | Where the PVC is mounted |
| `seed.enabled` | `false` | Run seed Jobs |
| `seed.layers` | `[]` | Layers to seed, one Job each |
| `podDisruptionBudget.enabled` | `false` | Create a PDB |
| `podSecurityContext` | non-root uid 1000 | Pod security context |
| `securityContext` | read-only root, no caps | Container security context |
| `extraVolumes` / `extraVolumeMounts` | a `/tmp` emptyDir | Additional volumes |
| `extraObjects` | `[]` | Extra manifests rendered with `tpl` |

See [values.yaml](values.yaml) for the full list with comments.

## Examples

- [examples/production.yaml](examples/production.yaml) — HA deployment with a shared cache,
  ingress, autoscaling and secrets from an existing Secret
- [examples/tls-letsencrypt.yaml](examples/tls-letsencrypt.yaml) — tilegroxy terminating TLS with
  Let's Encrypt on a shared volume
- [examples/aws.yaml](examples/aws.yaml) — a fully AWS deployment on EKS, showing every place an
  AWS service fits: S3 tile cache, Secrets Manager, IRSA, Aurora PostGIS and analytics, Cognito
  JWTs, ALB/ACM ingress, CloudFront, ADOT and CloudWatch Logs
- [examples/mapserver/](examples/mapserver/) — rendering tiles with MapServer through the cgi
  provider, including the custom image it requires, mapfiles from a ConfigMap and data from a
  volume
