FROM golang:1.22.6-alpine3.20@sha256:1a478681b671001b7f029f94b5016aed984a23ad99c707f6a0ab6563860ae2f3 AS build_stage

COPY . .

# TODO: Separate unit tests from integration (testcontainer) tests so we can at least run unit tests in this build
RUN apk update && \
    apk add make git && \
    make clean unit build

FROM alpine:3.20@sha256:0a4eaa0eecf5f8c050e5bba433f58c052be7587ee8af3e8b3910ef9ab5fbe9f5

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