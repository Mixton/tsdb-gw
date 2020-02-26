#!/bin/bash
set -x
BASE=$(dirname $0)
CODE_DIR=$(readlink -e "$BASE/../")

sudo apt-get install rpm

BUILD_ROOT=$CODE_DIR/build

ARCH="$(uname -m)"
VERSION=$(git describe --long --always)

## ubuntu 14.04
BUILD=${BUILD_ROOT}/upstart

PACKAGE_NAME="${BUILD}/tsdb-gw-${VERSION}_${ARCH}.deb"

mkdir -p ${BUILD}/usr/bin
mkdir -p ${BUILD}/etc/init
mkdir -p ${BUILD}/etc/gw

cp ${BASE}/config/tsdb-gw.ini ${BUILD}/etc/gw/
cp ${BUILD_ROOT}/tsdb-gw ${BUILD}/usr/bin/

fpm -s dir -t deb \
  -v ${VERSION} -n tsdb-gw -a ${ARCH} --description "HTTP gateway service for metrictank TSDB" \
  --deb-upstart ${BASE}/config/upstart/tsdb-gw \
  --replaces tsdb \
  -C ${BUILD} -p ${PACKAGE_NAME} .

## ubuntu 16.04, Debian 8, CentOS 7
BUILD=${BUILD_ROOT}/systemd
PACKAGE_NAME="${BUILD}/tsdb-gw-${VERSION}_${ARCH}.deb"
mkdir -p ${BUILD}/usr/bin
mkdir -p ${BUILD}/lib/systemd/system/
mkdir -p ${BUILD}/etc/gw
mkdir -p ${BUILD}/var/run/raintank

cp ${BASE}/config/tsdb-gw.ini ${BUILD}/etc/gw/
cp ${BUILD_ROOT}/tsdb-gw ${BUILD}/usr/bin/
cp ${BASE}/config/systemd/tsdb-gw.service $BUILD/lib/systemd/system

fpm -s dir -t deb \
  -v ${VERSION} -n tsdb-gw -a ${ARCH} --description "HTTP gateway service for metrictank TSDB" \
  --config-files /etc/gw/ \
  -m "Raintank Inc. <hello@raintank.io>" --vendor "raintank.io" \
  --replaces tsdb \
  --license "Apache2.0" -C ${BUILD} -p ${PACKAGE_NAME} .

BUILD=${BUILD_ROOT}/systemd-centos7

mkdir -p ${BUILD}/usr/bin
mkdir -p ${BUILD}/usr/lib/systemd/system/
mkdir -p ${BUILD}/etc/gw/
mkdir -p ${BUILD}/var/run/raintank

cp ${BASE}/config/tsdb-gw.ini ${BUILD}/etc/gw/
cp ${BUILD_ROOT}/tsdb-gw ${BUILD}/usr/bin/
cp ${BASE}/config/systemd/tsdb-gw.service $BUILD/usr/lib/systemd/system

PACKAGE_NAME="${BUILD}/tsdb-gw-${VERSION}.el7.${ARCH}.rpm"

fpm -s dir -t rpm \
  -v ${VERSION} -n tsdb-gw -a ${ARCH} --description "HTTP gateway service for metrictank TSDB" \
  --config-files /etc/gw/ \
  -m "Raintank Inc. <hello@raintank.io>" --vendor "raintank.io" \
  --replaces tsdb \
  --license "Apache2.0" -C ${BUILD} -p ${PACKAGE_NAME} .

## CentOS 6
BUILD=${BUILD_ROOT}/upstart-0.6.5

PACKAGE_NAME="${BUILD}/tsdb-gw-${VERSION}.el6.${ARCH}.rpm"

mkdir -p ${BUILD}/usr/bin
mkdir -p ${BUILD}/etc/init
mkdir -p ${BUILD}/etc/gw

cp ${BASE}/config/tsdb-gw.ini ${BUILD}/etc/gw/
cp ${BUILD_ROOT}/tsdb-gw ${BUILD}/usr/bin/
cp ${BASE}/config/upstart-0.6.5/tsdb-gw.conf $BUILD/etc/init

fpm -s dir -t rpm \
  -v ${VERSION} -n tsdb-gw -a ${ARCH} --description "HTTP gateway service for metrictank TSDB" \
  --replaces tsdb \
  -C ${BUILD} -p ${PACKAGE_NAME} .
