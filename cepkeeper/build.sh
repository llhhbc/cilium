#!/bin/bash

GOOS=linux go build -o cepKeeper .

docker buildx build -t registry.woqutech.com/woqutech/cepkeeper:v1.0.0 --push .
