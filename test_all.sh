#!/bin/sh

set -e

export GO111MODULE=on
go get github.com/caddyserver/caddy
go test -v -covermode=count -coverprofile=coverage.out
$GOPATH/bin/goveralls -coverprofile=coverage.out -service=travis-ci -repotoken $COVERALLS_TOKEN
