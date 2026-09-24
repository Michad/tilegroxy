
FROM node:26-alpine3.23@sha256:996a99c9a50ceca8b2e362a02dc88749eb1b97ad4f652130a99e52de4549a1f3 AS docs_stage


WORKDIR /usr/app
COPY . /usr/app

RUN npm ci && node_modules/antora/bin/antora antora-playbook.yml

FROM golang:1.27-alpine3.23@sha256:cabba651db628d291c68ad3217c7fe837b865d60276d9dfe1baa931311b1720c AS build_stage

COPY . .
COPY --from=docs_stage /usr/app/build/site internal/website/resources/

RUN apk update && \
    apk add make git && \
    make clean unit build



FROM alpine:3.24.2@sha256:3cf95fe0816180395592b8373f3ec60663f076127617bbacb4eacf9667afe2e9

ENV UID=1000
ENV GID=1000

COPY --from=build_stage /go/tilegroxy /usr/local/bin/tilegroxy

RUN apk update && \
    apk upgrade --no-cache && \
    mkdir /tilegroxy && \
    mkdir /tilegroxy/cache && \
    mkdir /tilegroxy/work && \
    chown -R 1000 /tilegroxy

EXPOSE 8080
WORKDIR /tilegroxy
ENTRYPOINT [ "tilegroxy"]