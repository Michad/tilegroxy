#!/usr/bin/env bash


REPOROOT=../../../
BASE=$REPOROOT/examples/mapserver

if [ -f $REPOROOT/.env ]; then
   . $REPOROOT/.env
fi

docker run --rm --mount=type=bind,source=$BASE/mapserver.conf,dst=/etc/mapserver.conf,readonly -v $BASE/mapfiles:/etc/mapserver/mapfiles:ro,Z -v $BASE/data:/etc/mapserver/data:ro,Z --env-file <(env | grep _) camptocamp/mapserver@sha256:abf70f1326c230eccf9558ba594f03d0c35064dbdf1bb9d81e936d288ef93e79 bash -c "unset MS_MAP_PATTERN && mapserv"
