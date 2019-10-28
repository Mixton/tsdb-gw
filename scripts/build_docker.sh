#!/bin/bash

set -x
set -e

# Find the directory we exist within
DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
cd ${DIR}/..

VERSION=`git describe --always`

rm -rf scripts/build
mkdir scripts/build
cp build/* scripts/build/

docker build -f scripts/Dockerfile -t raintank/tsdb-gw:$VERSION .
docker tag raintank/tsdb-gw:$VERSION raintank/tsdb-gw:latest

docker build -f cmd/persister-gw/Dockerfile -t raintank/persister-gw:$VERSION .
docker tag raintank/persister-gw:$VERSION raintank/persister-gw:latest
