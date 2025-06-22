#!/usr/bin/env bash
# Copyright 2025 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

ETCD_VER="v$1"
ETCD_DIR="/tmp/etcd-${ETCD_VER}-linux-amd64"
ETCD_URL="https://github.com/etcd-io/etcd/releases/download/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz"
ETCD_TMP="/tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz"

if [ ! -f "${ETCD_DIR:-}/etcd" ]; then
  echo "Downloading etcd from ${ETCD_URL} to ${ETCD_DIR}"
  curl -Ls -o "${ETCD_TMP}" "${ETCD_URL}"
  mkdir -p "${ETCD_DIR}"
  tar xzf "${ETCD_TMP}" -C "${ETCD_DIR}" --strip-components=1 --exclude=Documentation
  rm -f "${ETCD_TMP}"
fi
