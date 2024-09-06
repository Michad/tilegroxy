
FROM node:20-alpine3.20@sha256:1a526b97cace6b4006256570efa1a29cd1fe4b96a5301f8d48e87c5139438a45 AS docs_stage

COPY . .

RUN npm i -g antora && antora antora-playbook.yml

FROM golang:1.22.6-alpine3.20@sha256:1a478681b671001b7f029f94b5016aed984a23ad99c707f6a0ab6563860ae2f3 AS build_stage

COPY . .
COPY --from=docs_stage build/site internal/website/resources/

RUN ls -hal internal/website/resources/
RUN apk update && \
    apk add make git && \
    make clean unit build



FROM alpine:3.20@sha256:0a4eaa0eecf5f8c050e5bba433f58c052be7587ee8af3e8b3910ef9ab5fbe9f5

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