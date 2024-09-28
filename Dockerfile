
FROM node:20-alpine3.20@sha256:1a526b97cace6b4006256570efa1a29cd1fe4b96a5301f8d48e87c5139438a45 AS docs_stage


WORKDIR /usr/app
COPY . /usr/app

RUN npm ci && node_modules/antora/bin/antora antora-playbook.yml

FROM golang:1.22.7-alpine3.20@sha256:48eab5e3505d8c8b42a06fe5f1cf4c346c167cc6a89e772f31cb9e5c301dcf60 AS build_stage

COPY . .
COPY --from=docs_stage /usr/app/build/site internal/website/resources/

RUN apk update && \
    apk add make git && \
    make clean unit build



FROM alpine:3.20.3@sha256:a8f120106f5549715aa966fd7cefaf3b7045f6414fed428684de62fec8c2ca4b

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