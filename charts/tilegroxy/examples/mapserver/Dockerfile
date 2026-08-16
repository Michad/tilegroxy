# Tilegroxy plus MapServer in one image.
#
# The CGI provider shells out to the mapserv executable on the local
# filesystem, so the binary has to live in the same container as tilegroxy.
# The stock ghcr.io/michad/tilegroxy image can't simply be extended with a
# package here: it's Alpine based and MapServer isn't packaged for Alpine.
# So this copies the tilegroxy binary out of the official image onto a Debian
# base, where cgi-mapserver is available.
#
# Build and push to your own registry, then point image.repository at it:
#
#   docker build -t my-registry/tilegroxy-mapserver:0.10.0 .
#   docker push my-registry/tilegroxy-mapserver:0.10.0

ARG TILEGROXY_VERSION=0.10.0

FROM ghcr.io/michad/tilegroxy:${TILEGROXY_VERSION} AS tilegroxy

FROM debian:13-slim

# cgi-mapserver provides the mapserv CGI binary at /usr/bin/mapserv, which is
# the path the config below points `exec` at. mapserver-bin adds the companion
# command line tools (shp2img, map2img) which are handy for debugging a mapfile
# inside the running pod but aren't required to serve tiles.
#
# The GDAL/PROJ data files come in as dependencies and are what let mapfiles
# read common formats and reproject.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        cgi-mapserver \
        mapserver-bin \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=tilegroxy /usr/local/bin/tilegroxy /usr/local/bin/tilegroxy

# Fail the build rather than every tile request if either binary is missing or
# the tilegroxy binary isn't portable to this base image.
RUN test -x /usr/bin/mapserv && \
    test -x /usr/local/bin/tilegroxy && \
    /usr/local/bin/tilegroxy version

# Match the uid the chart's podSecurityContext runs as.
RUN useradd --uid 1000 --create-home --shell /usr/sbin/nologin tilegroxy && \
    mkdir -p /tilegroxy && \
    chown -R 1000:1000 /tilegroxy

USER 1000
WORKDIR /tilegroxy
EXPOSE 8080
ENTRYPOINT ["tilegroxy"]
