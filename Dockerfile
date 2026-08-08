
FROM node:26-alpine3.23@sha256:ce3cc39fe3b8b2602d3b1c4d63d301e46b48c550ecb627869853ddcdda418b63 AS docs_stage


WORKDIR /usr/app
COPY . /usr/app

RUN npm ci && node_modules/antora/bin/antora antora-playbook.yml

FROM golang:1.26-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS build_stage

COPY . .
COPY --from=docs_stage /usr/app/build/site internal/website/resources/

RUN apk update && \
    apk add make git && \
    make clean unit build



FROM alpine:3.23.4@sha256:4d889c14e7d5a73929ab00be2ef8ff22437e7cbc545931e52554a7b00e123d8b

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