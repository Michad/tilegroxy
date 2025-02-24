OUT := tilegroxy
PKG := github.com/Michad/tilegroxy
VERSION := $(shell git describe --tag --abbrev=0 --dirty)
REF := $(shell git rev-parse --short HEAD)
DATE := $(shell date -Iseconds --u)

all: clean test docs build version

build:
	go build -v -o ${OUT} -ldflags="-X \"${PKG}/pkg/static.tilegroxyVersion=${VERSION}\" -X \"${PKG}/pkg/static.tilegroxyBuildRef=${REF}\" -X \"${PKG}/pkg/static.tilegroxyBuildDate=${DATE}\"" -tags viper_bind_struct

test:
	@go test ./internal/... ./pkg/... ./cmd/... -count=1 -tags viper_bind_struct

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
	@go install github.com/nieomylnieja/go-libyear/cmd/go-libyear@v0.4.6
	@go-libyear --json go.mod < /dev/null

lint:
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63.4
	@golangci-lint run --fix -E asciicheck,bidichk,bodyclose,canonicalheader,dogsled,dupl,exhaustive,gocheckcompilerdirectives,gocritic,gofmt,durationcheck,errname,errorlint,goheader,inamedparam,interfacebloat,intrange,maintidx,makezero,mirror,misspell,mnd,noctx,nonamedreturns,perfsprint,prealloc,predeclared,revive,stylecheck,tenv,testifylint,usestdlibvars,unconvert,wastedassign

docs:
	@npm i 
	@node_modules/antora/bin/antora antora-playbook.yml
	@cp -r build/site/* internal/website/resources/

readme:
	@asciidoctor-reducer -v >/dev/null 2>&1 || gem install asciidoctor-reducer
	@asciidoctor-reducer -o README.adoc README_source.adoc
	@echo Updated README.adoc

version:
	@./${OUT} version --json

install:
	cp ${OUT} /usr/local/bin

clean:
	@go clean
	-@rm ${OUT}

.PHONY: build clean cover cover-out coverage docs lint libyears readme test unit version
