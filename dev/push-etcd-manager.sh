#!/usr/bin/env bash
# Copyright 2020 The Kubernetes Authors.
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

set -o errexit -o nounset -o pipefail
set -x;

# cd to the repo root
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [[ -z "${VERSION:-}" ]]; then
  VERSION=$(git describe --always --match 'etcd-manager/*' | sed s@etcd-manager/@@g)
fi

if [[ -z "${DOCKER_REGISTRY:-}" ]]; then
  DOCKER_REGISTRY=us-central1-docker.pkg.dev
fi

if [[ -z "${DOCKER_IMAGE_PREFIX:-}" ]]; then
  DOCKER_IMAGE_PREFIX=k8s-staging-images/etcd-manager/
fi

if [[ -n "${INSTALL_BAZELISK:-}" ]]; then
  DOWNLOAD_URL="https://github.com/bazelbuild/bazelisk/releases/download/v1.20.0/bazelisk-linux-amd64"
  echo "Downloading bazelisk from $DOWNLOAD_URL"
  curl -L --output "/tmp/bazelisk" "${DOWNLOAD_URL}"
  chmod +x "/tmp/bazelisk"
  # Install to /usFixr/local/bin
  mv "/tmp/bazelisk" "/usr/local/bin/bazelisk"
  # Use bazelisk for commands that invoke bazel
  ln -sf "/usr/local/bin/bazelisk" "/usr/local/bin/bazel"
fi

# Build and upload etcd-manager images & binaries
DOCKER_REGISTRY=${DOCKER_REGISTRY} DOCKER_IMAGE_PREFIX=${DOCKER_IMAGE_PREFIX} DOCKER_TAG=${VERSION} make push

# Create a custom builder that is multi-arch capable
# See https://docs.docker.com/build/building/multi-platform/
docker buildx create \
  --name container-builder \
  --driver docker-container \
  --use \
  --bootstrap \
  || true # Ignore errors, assume the error is that the builder already exists

# Build and upload non-bazel images
BUILD_PLATFORMS=linux/amd64,linux/arm64 BUILD_ARGS=--push IMAGE_PREFIX=${DOCKER_REGISTRY}/${DOCKER_IMAGE_PREFIX} IMAGE_TAG=${VERSION} ${REPO_ROOT}/dev/tasks/build-images