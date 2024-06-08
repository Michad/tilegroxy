FROM golang:1.22.4-alpine3.20@sha256:9bdd5692d39acc3f8d0ea6f81327f87ac6b473dd29a2b6006df362bff48dd1f8 AS build_stage

COPY . .

RUN go test ./... && go build

FROM alpine:3.20@sha256:77726ef6b57ddf65bb551896826ec38bc3e53f75cdde31354fbffb4f25238ebd

ENV UID 1000
ENV GID 1000

COPY --from=build_stage /go/tilegroxy /usr/local/bin/tilegroxy

RUN mkdir /tilegroxy && mkdir /tilegroxy/cache && mkdir /tilegroxy/work && chown -R 1000 /tilegroxy

EXPOSE 8080
WORKDIR /tilegroxy
ENTRYPOINT [ "tilegroxy"]