#!/bin/bash

set -ex

GOOS=linux GOARCH=amd64  go build -o http-max-rps-amd64     github.com/buoyantio/http-max-rps
GOOS=linux GOARCH=arm    go build -o http-max-rps-arm       github.com/buoyantio/http-max-rps
GOOS=darwin GOARCH=amd64 go build -o http-max-rps-darwin    github.com/buoyantio/http-max-rps
echo "releases built:"
ls http-max-rps-*
