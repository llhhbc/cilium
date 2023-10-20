##### syntax=docker/dockerfile:1.2

# Copyright Authors of Cilium
# SPDX-License-Identifier: Apache-2.0

#ARG TESTER_IMAGE=quay.io/cilium/image-tester:c37f768323abfba87c90cd9c82d37136183457bc@sha256:4c9d640b6379eb4964b8590acc95ca2dfaa71df70f4467fb7d8ac076acf6a8e1
ARG GOLANG_IMAGE=registry.woqutech.com/library/golang:1.18
#ARG UBUNTU_IMAGE=docker.io/library/ubuntu:20.04@sha256:b33325a00c7c27b23ae48cf17d2c654e2c30812b35e7846c006389318f6a71c2
#
#ARG CILIUM_LLVM_IMAGE=quay.io/cilium/cilium-llvm:547db7ec9a750b8f888a506709adb41f135b952e@sha256:4d6fa0aede3556c5fb5a9c71bc6b9585475ac9b1064f516d4c45c8fb691c9d9e
#ARG CILIUM_BPFTOOL_IMAGE=quay.io/cilium/cilium-bpftool:78448c1a37ff2b790d5e25c3d8b8ec3e96e6405f@sha256:99a9453a921a8de99899ef82e0822f0c03f65d97005c064e231c06247ad8597d
#ARG CILIUM_IPROUTE2_IMAGE=registry.woqutech.com/longhui.li/cilium-iproute2:myv1.12.7
ARG CILIUM_IPROUTE2_IMAGE=registry.woqutech.com/woqutech/cilium-cilium:v1.12.7
#ARG CILIUM_IPTABLES_IMAGE=quay.io/cilium/iptables-20.04:e6f83206c57e606282056903ffd3aab0183bdaed@sha256:7ce0de449d356a5259021dc13f2b00a8bddfbea57a1c91ff8f146d455cace9e5

#FROM ${CILIUM_LLVM_IMAGE} as llvm-dist
#FROM ${CILIUM_BPFTOOL_IMAGE} as bpftool-dist
FROM ${CILIUM_IPROUTE2_IMAGE} as iproute2-dist
#FROM ${CILIUM_IPTABLES_IMAGE} as iptables-dist

FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} as gops-cni-builder

RUN apt-get update && apt-get install -y binutils-aarch64-linux-gnu binutils-x86-64-linux-gnu

# build-gops.sh will build both archs at the same time
ARG WORKDIR=/go/src/github.com/cilium/cilium/images/runtime
WORKDIR $WORKDIR
COPY build-gops.sh $WORKDIR/
RUN $WORKDIR/build-gops.sh
# download-cni.sh will build both archs at the same time
COPY download-cni.sh cni-version.sh $WORKDIR/
RUN $WORKDIR/download-cni.sh

FROM registry.woqutech.com/library/hce:v2.0 as rootfs

# Change the number to force the generation of a new git-tree SHA. Useful when
# we want to re-run 'apt-get upgrade' for stale images.
#ENV FORCE_BUILD=5

# Update ubuntu packages to the most recent versions
#RUN apt-get update && \
#    apt-get upgrade -y && \
#    apt-get install -y jq

WORKDIR /go/src/github.com/cilium/cilium/images/runtime
COPY install-runtime-deps.sh $WORKDIR/
RUN $WORKDIR/install-runtime-deps.sh

RUN curl http://10.10.88.5/data/kernel/hce.repo -o /etc/yum.repos.d/hce.repo && \
    yum install -y iptables && \
    yum install -y llvm bpftool util-linux clang && \
    yum clean all && rm -rf /etc/yum.repos.d/hce.repo
#COPY --from=iptables-dist /iptables /iptables
#RUN dpkg -i /iptables/*\.deb && rm -rf /iptables

COPY iptables-wrapper-installer.sh $WORKDIR/
RUN $WORKDIR/iptables-wrapper-installer.sh --no-sanity-check

#COPY --from=llvm-dist /usr/local/bin/clang /usr/local/bin/llc /bin/
#COPY --from=bpftool-dist /usr/local /usr/local
COPY --from=iproute2-dist /usr/lib/libbpf* /usr/lib64
COPY --from=iproute2-dist /usr/local /usr/local

ARG TARGETPLATFORM
COPY --from=gops-cni-builder /out/${TARGETPLATFORM}/bin/loopback /cni/loopback
COPY --from=gops-cni-builder /out/${TARGETPLATFORM}/bin/gops /bin/gops

#
#FROM scratch
#LABEL maintainer="maintainer@cilium.io"
#COPY --from=rootfs / /
