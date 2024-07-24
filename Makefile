OUT := tilegroxy
PKG := github.com/Michad/tilegroxy
VERSION := $(shell git describe --tag --abbrev=0 --dirty)
REF := $(shell git rev-parse --short HEAD)
DATE := $(shell date -Iseconds --u)

all: clean test build version

build:
	go build -v -o ${OUT} -ldflags="-X \"${PKG}/pkg/static.tilegroxyVersion=${VERSION}\" -X \"${PKG}/pkg/static.tilegroxyBuildRef=${REF}\" -X \"${PKG}/pkg/static.tilegroxyBuildDate=${DATE}\"" -tags viper_bind_struct

test:
	@go test ./internal/... ./pkg/... ./cmd/... -v -count=1 -tags viper_bind_struct

unit:
	@go test ./internal/... $(go list ./pkg/... | grep -v mod) ./cmd/... -v -count=1 -tags "unit,viper_bind_struct"

cover:
	@go install github.com/dave/courtney@latest
	@courtney ./internal/... ./pkg/... ./cmd/...
	@go tool cover -func=coverage.out -o=coveragef.out

cover-out:
	@tail -1 coveragef.out
	@go tool cover -html=coverage.out

coverage: cover cover-out

libyears:
	@go install github.com/nieomylnieja/go-libyear/cmd/go-libyear@latest
	@go-libyear --json go.mod < /dev/null

lint:
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1
	@golangci-lint run --fix -E asciicheck,bidichk,bodyclose,canonicalheader,dogsled,exhaustive,gocheckcompilerdirectives,gocritic,godox,gofmt,durationcheck,errname,errorlint,fatcontext,goheader,interfacebloat,intrange,maintidx,makezero,mirror,misspell,nonamedreturns,prealloc,predeclared,unconvert,wastedassign

version:
	@./${OUT} version --json

install:
	cp ${OUT} /usr/local/bin

clean:
	@go clean
	-@rm ${OUT}

.PHONY: build test clean version
