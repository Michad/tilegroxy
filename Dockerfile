
FROM node:22-alpine3.20@sha256:c9bb43423a6229aeddf3d16ae6aaa0ff71a0b2951ce18ec8fedb6f5d766cf286 AS docs_stage


WORKDIR /usr/app
COPY . /usr/app

RUN npm ci && node_modules/antora/bin/antora antora-playbook.yml

FROM golang:1.22.9-alpine3.20@sha256:6fd249c485a54621b563b83d2eab51359c3436d06ee4669911a4a35c6b3c8d92 AS build_stage

COPY . .
COPY --from=docs_stage /usr/app/build/site internal/website/resources/

RUN apk update && \
    apk add make git && \
    make clean unit build



FROM alpine:3.20.3@sha256:beefdbd8a1da6d2915566fde36db9db0b524eb737fc57cd1367effd16dc0d06d

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