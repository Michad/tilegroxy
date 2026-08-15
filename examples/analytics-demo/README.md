# Analytics Example

This folder contains a self-contained demo of tilegroxy's analytics support. It runs tilegroxy, a PostgreSQL database and a small dashboard app in Docker. You pan around a map served through tilegroxy and watch the resulting usage events show up in a dashboard reading the analytics table directly.

The only requirement is Docker with Compose. Tiles come from OpenStreetMap so the machine needs outbound network access.

## Running It

```
docker compose up
```

Then open http://localhost:8000 for the map and http://localhost:8000/dashboard for the usage numbers. The dashboard refreshes every two seconds.

Tear it down with `docker compose down -v`. The `-v` discards the recorded events along with the database.

## Structure

`docker-compose.yml` wires up the three containers. PostgreSQL starts first and the others wait for its health check.

`tilegroxy.yml` is the tilegroxy configuration. It defines a `postgresql` datastore, points the `postgres` analytics module at it, and serves two proxy layers plus a third with `skipAnalytics` set.

`schema.sql` creates the analytics table. Tilegroxy never issues DDL, so the table has to exist before it starts; compose applies this on the database's first boot.

`dashboard/app.py` is a small Python server that queries the analytics table and serves the two pages. `dashboard/index.html` is a leaflet map pointed at tilegroxy and `dashboard/dashboard.html` renders the stats.

## What To Look At

The batch settings in `tilegroxy.yml` are tuned for a demo, writing every 10 events or every 2 seconds so a tile you request appears almost immediately. Production values are much higher, see the [analytics documentation](../../docs/operation/modules/ROOT/pages/configuration/analytics/index.adoc) for the defaults.

Switching layers in the map's top right control changes which layer the events are attributed to. The `debug-grid` layer at `http://localhost:8080/tiles/debug-grid/1/1/1` never appears in the dashboard because it sets `skipAnalytics`.

Events are only recorded for tiles that were served successfully. Requesting a layer that doesn't exist, such as `http://localhost:8080/tiles/nope/1/1/1`, leaves the counts unchanged. A tile served from the cache still counts, since the event records that a user consumed a tile rather than that a provider generated one; reloading the map does not stop the counter.

The `extra` column in the recent events table holds the attributes selected by `fields` and `extraFields`. This demo collects duration, size and content type along with a constant `environment` tag. The `ip` and `useragent` fields are deliberately left off since they're personal data.

## Using Your Own Reporting

Nothing about the analytics table is specific to the dashboard here. It's an ordinary table you own, so Grafana, Metabase or a psql session work just as well:

```
docker compose exec postgres psql -U tilegroxy -d analytics \
  -c "SELECT layer, z, count(*) FROM tilegroxy_analytics GROUP BY layer, z ORDER BY 3 DESC"
```

For higher volumes, the [ClickHouse module](../../docs/operation/modules/ROOT/pages/configuration/analytics/clickhouse.adoc) records the same events to a database better suited to it, and the [custom module](../../docs/operation/modules/ROOT/pages/configuration/analytics/custom.adoc) sends them wherever else you need.
