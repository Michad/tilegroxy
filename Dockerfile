FROM golang:1.22.4-alpine3.20@sha256:794964a2e6ad0eefa86be3c20256ee93b29f9d8bfaa82ff07b9f0d64257f5cdd AS build_stage

COPY . .

# TODO: Separate unit tests from integration (testcontainer) tests so we can at least run unit tests in this build
RUN apk update && \
    apk add make git && \
    make build

FROM alpine:3.20@sha256:77726ef6b57ddf65bb551896826ec38bc3e53f75cdde31354fbffb4f25238ebd

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