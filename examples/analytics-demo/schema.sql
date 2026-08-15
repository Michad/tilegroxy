-- Tilegroxy never issues DDL, the analytics table has to exist before it starts. Postgres runs
-- everything in /docker-entrypoint-initdb.d on first boot, so compose applies this automatically.
--
-- The columns here match the defaults of the postgres analytics module. Anything selected via
-- `fields` or `extraFields` lands in the extra JSONB column, so collecting more attributes later
-- doesn't need a migration.

CREATE TABLE tilegroxy_analytics (
    time      TIMESTAMPTZ NOT NULL,
    layer     TEXT        NOT NULL,
    z         INTEGER     NOT NULL,
    x         INTEGER     NOT NULL,
    y         INTEGER     NOT NULL,
    user_id   TEXT,
    extra     JSONB
);

CREATE INDEX ON tilegroxy_analytics (layer, time);
