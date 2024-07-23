FROM golang:1.22.5-alpine3.20@sha256:24f1bfa32fbf21eec3d764ed190adb78ee3147c6c3558c950c2be6bf6fc23a1b AS build_stage

COPY . .

# TODO: Separate unit tests from integration (testcontainer) tests so we can at least run unit tests in this build
RUN apk update && \
    apk add make git && \
    make clean unit build

FROM alpine:3.20@sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0

ENV UID 1000
ENV GID 1000

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