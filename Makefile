OUT := tilegroxy
PKG := github.com/Michad/tilegroxy
VERSION := $(shell git describe --tag --abbrev=0 --dirty)
REF := $(shell git rev-parse --short HEAD)
DATE := $(shell date -Iseconds --u)

all: clean test build version

build:
	go build -v -o ${OUT} -ldflags="-X \"${PKG}/internal.tilegroxyVersion=${VERSION}\" -X \"${PKG}/internal.tilegroxyBuildRef=${REF}\" -X \"${PKG}/internal.tilegroxyBuildDate=${DATE}\"" 

test:
	@go test ./... -v -count=1

unit:
	@go test ./... -v -count=1 -tags=unit

version:
	@./${OUT} version --json

install:
	cp ${OUT} /usr/local/bin

clean:
	-@rm ${OUT}

.PHONY: build test clean version
