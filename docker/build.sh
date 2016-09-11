#!/bin/bash

set -e

cd ..
GOOS=linux GOARCH=amd64 caddydev -o docker/caddy awslambda
cd docker
sudo docker build -t coopernurse/caddy-awslambda .
rm -f caddy
