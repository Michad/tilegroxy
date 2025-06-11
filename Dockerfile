
FROM node:24-alpine3.21@sha256:60e5a50817f987535c29bbe032a0dbd818bb5a605012bb1edd3861f63eb4b670 AS docs_stage


WORKDIR /usr/app
COPY . /usr/app

RUN npm ci && node_modules/antora/bin/antora antora-playbook.yml

FROM golang:1.24.4-alpine3.21@sha256:56a23791af0f77c87b049230ead03bd8c3ad41683415ea4595e84ce7eada121a AS build_stage

COPY . .
COPY --from=docs_stage /usr/app/build/site internal/website/resources/

RUN apk update && \
    apk add make git && \
    make clean unit build



FROM alpine:3.22.0@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715

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