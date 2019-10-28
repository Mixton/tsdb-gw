#!/bin/bash

set -xe

# Find the directory we exist within
DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
cd ${DIR}

export CGO_ENABLED=0
VERSION=`git describe --always`
SOURCE_DIR=${DIR}/..
BUILD_DIR=$SOURCE_DIR/build

# Make dir
mkdir -p $BUILD_DIR

# Clean build bin dir
rm -rf $BUILD_DIR/*

# Build binary
cd ../
cd cmd/tsdb-gw
go build -ldflags "-X main.GitHash=$VERSION" -o $BUILD_DIR/tsdb-gw
cd ../persister-gw
go build -ldflags "-X main.GitHash=$VERSION" -o $BUILD_DIR/persister-gw

# delete temporary build dir of librdkafka, since it is linked statically we
# don't need it anymore
echo rm -rf $TMP_DIR
