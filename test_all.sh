#!/bin/sh

set -e

export GO111MODULE=on
go test -v -covermode=count -coverprofile=coverage.out

if [ -n "$COVERALLS_TOKEN" ]; then
    $GOPATH/bin/goveralls -coverprofile=coverage.out -service=travis-ci -repotoken $COVERALLS_TOKEN
fi
