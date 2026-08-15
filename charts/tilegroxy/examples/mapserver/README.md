# MapServer via the CGI provider

Renders tiles with [MapServer](https://www.mapserver.org) instead of proxying them, using
tilegroxy's [cgi provider](https://tilegroxy.michael.davis.name/operation/configuration/provider/cgi.html).
Tilegroxy takes the role Apache httpd traditionally plays, invoking the `mapserv` CGI executable
once per tile request.

This is the Kubernetes counterpart to [examples/mapserver](../../../../examples/mapserver) in the
repository root, which shows the same setup running locally.

## The catch: it needs a custom image

The cgi provider executes a binary on the local filesystem, so `mapserv` has to be inside the same
container as tilegroxy. The stock `ghcr.io/michad/tilegroxy` image doesn't have it, and it can't be
added with a one-line `apk add` either — that image is Alpine based and **MapServer is not packaged
for Alpine**.

The [Dockerfile](Dockerfile) here therefore copies the tilegroxy binary out of the official image
onto a Debian base, where `cgi-mapserver` provides `/usr/bin/mapserv`:

```sh
docker build -t my-registry/tilegroxy-mapserver:0.10.0 .
docker push my-registry/tilegroxy-mapserver:0.10.0
```

Then point `image.repository` at it, as [values.yaml](values.yaml) does.

## Files

| File | Purpose |
| --- | --- |
| `Dockerfile` | Tilegroxy + MapServer image |
| `values.yaml` | Chart values: the two layers, volumes and resources |
| `mapserver.conf` | MapServer 8+ config, restricting which mapfiles can be opened |
| `states.map` | Example mapfile serving US state boundaries |

## Deploying

The mapfiles go in a ConfigMap; the geospatial data they reference goes on a volume, since
shapefiles and rasters don't belong in a ConfigMap (and would hit its 1MiB limit fast).

```sh
# Mapfiles and the MapServer config
kubectl create configmap tilegroxy-mapfiles \
  --from-file=mapserver.conf \
  --from-file=states.map

# A volume holding the data the mapfiles reference. The example mapfile wants
# cb_2018_us_state_20m.shp (and its .dbf/.shx/.prj siblings) from
# examples/mapserver/data in the repository root.
kubectl apply -f gis-data-pvc.yaml   # your PVC, named "gis-data"

helm install tilegroxy ../../ -f values.yaml
```

Both mounts land under the provider's `workingDir` (`/tilegroxy/mapserver`), giving the layout the
mapfile's relative `DATA` path expects:

```
/tilegroxy/mapserver/
├── mapserver.conf            (ConfigMap, via subPath)
├── mapfiles/states.map       (ConfigMap)
└── data/cb_2018_us_state_20m.shp   (PVC)
```

If your data is small and changes at the same rate as your deployments, baking both the mapfiles
and the data into the image is simpler and drops the ConfigMap and PVC entirely.

## Two layers, PNG and MVT

Both layers are patterns, so a single entry serves every mapfile/layer combination —
`/tiles/states_states/{z}/{x}/{y}` renders the `states` layer of `mapfiles/states.map`.

The MVT layer needs a per-layer `client` override:

```yaml
client:
  contentTypes:
    - application/vnd.mapbox-vector-tile
    - application/x-protobuf
```

Tilegroxy's default accepted content types are `image/png`, `image/jpg` and `image/jpeg`, so
without this the MVT response is treated as an error. Setting it per layer rather than globally
keeps the PNG layer strict.

## Security

The layer name is interpolated into the `map=` parameter handed to mapserv, so an unvalidated
layer name is a path traversal vector. Two independent defenses:

1. **`paramValidator`** on the layer (`^[a-zA-Z0-9\-]+$`) rejects the request before mapserv is
   ever invoked. Keep this as strict as your naming allows.
2. **`MS_MAP_PATTERN`** in `mapserver.conf` restricts which paths mapserv will open at all, so a
   gap in the first defense still doesn't expose the filesystem.

The chart's default hardening needs no relaxing here: forking `mapserv` works fine as uid 1000
with a read-only root filesystem and all capabilities dropped. MapServer wants somewhere writable
for temporary files, which the `/tmp` emptyDir in `extraVolumes` provides.

## Operational notes

- **Rendering is CPU bound.** Unlike a proxy layer, every cache miss forks a process and rasterizes
  geometry. Size requests against your mapfile complexity and autoscale on CPU.
- **Cache aggressively.** The example uses a per-pod memory cache for simplicity; for real
  multi-replica use, switch `config.cache` to redis or s3 so replicas share the work.
- **The tile health check is worth keeping.** It exercises the whole CGI path, so a broken mapfile
  or a missing shapefile takes the pod out of the Service instead of serving errors.
- **Updating a mapfile** means updating the ConfigMap. Kubelet syncs the change into the pod within
  a minute or so, and since the provider reads the mapfile per request, no restart is needed —
  but the cache will keep serving previously rendered tiles until they expire.
