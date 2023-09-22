#!/bin/bash

# Copyright Authors of Cilium
# SPDX-License-Identifier: Apache-2.0

set -o xtrace
set -o errexit
set -o pipefail
set -o nounset

packages=(
  # Additional iproute2 runtime dependencies
  elfutils-devel
  libmnl
  # Bash completion for Cilium
  bash-completion
  # Additional misc runtime dependencies
  iptables
  ipset
  kmod
  ca-certificates
)
curl http://10.10.88.5/data/kernel/hce.repo -o /etc/yum.repos.d/hce.repo
yum update

# tzdata is one of the dependencies and a timezone must be set
# to avoid interactive prompt when it is being installed
ln -fs /usr/share/zoneinfo/UTC /etc/localtime

yum install -y "${packages[@]}"

yum clean all
rm -rf /etc/yum.repos.d/*

