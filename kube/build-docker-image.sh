#!/bin/bash -ex
#
# This script builds and uploads a Docker image for vmsg.

IMAGE="b.gcr.io/vmsg/vmsg:latest"

cd $(dirname $0)
jiri go build -o ./vmsg -a -ldflags '-extldflags "-lpthread -static"' messenger/vmsg
docker build -t "${IMAGE}" .

gcloud docker push "${IMAGE}"
