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

STABLE_DOCKER_REGISTRY     := $(shell tools/get_workspace_status.sh | grep STABLE_DOCKER_REGISTRY | cut -d ' ' -f 2)
STABLE_DOCKER_IMAGE_PREFIX := $(shell tools/get_workspace_status.sh | grep STABLE_DOCKER_IMAGE_PREFIX | cut -d ' ' -f 2)
STABLE_DOCKER_TAG          := $(shell tools/get_workspace_status.sh | grep STABLE_DOCKER_TAG | cut -d ' ' -f 2)
IMAGE_BASE                 := $(STABLE_DOCKER_REGISTRY)/$(STABLE_DOCKER_IMAGE_PREFIX)

KO=go run github.com/google/ko@v0.18.0

BAZEL?=bazelisk
BAZEL_FLAGS=--features=pure --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64

.PHONY: all
all: test

.PHONY: test
test:
	${BAZEL} test //... --test_output=streamed

.PHONY: stress-test
stress-test:
	${BAZEL} test //... --test_output=streamed --runs_per_test=10

.PHONY: gofmt
gofmt:
	gofmt -w -s cmd/ pkg/

.PHONY: goimports
goimports:
	goimports -w cmd/ pkg/ test/

.PHONY: build-etcd-manager-amd64 build-etcd-manager-arm64
build-etcd-manager-amd64 build-etcd-manager-arm64: build-etcd-manager-%:
	${BAZEL} build ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_$* //cmd/etcd-manager:etcd-manager

.PHONY: push-etcd-manager
push-etcd-manager:
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //images:push-etcd-manager
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_arm64 //images:push-etcd-manager

.PHONY: push-etcd-manager-manifest
push-etcd-manager-manifest:
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create \
		$(IMAGE_BASE)etcd-manager:$(STABLE_DOCKER_TAG) \
		$(IMAGE_BASE)etcd-manager:$(STABLE_DOCKER_TAG)-amd64 \
		$(IMAGE_BASE)etcd-manager:$(STABLE_DOCKER_TAG)-arm64
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge \
		$(IMAGE_BASE)etcd-manager:$(STABLE_DOCKER_TAG)

.PHONY: push-etcd-manager-slim
push-etcd-manager-slim:
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //images:push-etcd-manager-slim
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_arm64 //images:push-etcd-manager-slim

.PHONY: push-etcd-manager-slim-manifest
push-etcd-manager-slim-manifest:
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create \
		$(IMAGE_BASE)etcd-manager-slim:$(STABLE_DOCKER_TAG) \
		$(IMAGE_BASE)etcd-manager-slim:$(STABLE_DOCKER_TAG)-amd64 \
		$(IMAGE_BASE)etcd-manager-slim:$(STABLE_DOCKER_TAG)-arm64
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge \
		$(IMAGE_BASE)etcd-manager-slim:$(STABLE_DOCKER_TAG)

.PHONY: push-etcd-dump
push-etcd-dump:
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //images:push-etcd-dump
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_arm64 //images:push-etcd-dump

.PHONY: push-etcd-dump-manifest
push-etcd-dump-manifest:
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create \
		$(IMAGE_BASE)etcd-dump:$(STABLE_DOCKER_TAG) \
		$(IMAGE_BASE)etcd-dump:$(STABLE_DOCKER_TAG)-amd64 \
		$(IMAGE_BASE)etcd-dump:$(STABLE_DOCKER_TAG)-arm64
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge \
		$(IMAGE_BASE)etcd-dump:$(STABLE_DOCKER_TAG)

.PHONY: push-etcd-backup
push-etcd-backup:
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_amd64 //images:push-etcd-backup
	${BAZEL} run ${BAZEL_FLAGS} --platforms=@io_bazel_rules_go//go/toolchain:linux_arm64 //images:push-etcd-backup

.PHONY: push-etcd-backup-manifest
push-etcd-backup-manifest:
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create \
		$(IMAGE_BASE)etcd-backup:$(STABLE_DOCKER_TAG) \
		$(IMAGE_BASE)etcd-backup:$(STABLE_DOCKER_TAG)-amd64 \
		$(IMAGE_BASE)etcd-backup:$(STABLE_DOCKER_TAG)-arm64
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge \
		$(IMAGE_BASE)etcd-backup:$(STABLE_DOCKER_TAG)

.PHONY: push-images
push-images: push-etcd-manager push-etcd-dump push-etcd-backup
	echo "pushed images"

.PHONY: push-manifests
push-manifests: push-etcd-manager-manifest push-etcd-dump-manifest push-etcd-backup-manifest
	echo "pushed manifests"

.PHONY: push
push: push-images push-manifests

.PHONY: gazelle
gazelle:
	${BAZEL} run //:gazelle -- fix
	cd tools/deb-tools && ${BAZEL} run //:gazelle -- fix
	git checkout -- vendor
	rm -f vendor/github.com/coreos/etcd/cmd/etcd
	#rm vendor/github.com/golang/protobuf/protoc-gen-go/testdata/multi/BUILD.bazel

.PHONY: dep-ensure
dep-ensure:
	echo "'make dep-ensure' has been replaced by 'make vendor'"
	exit 1

.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor
	find vendor/ -name "BUILD" -delete
	find vendor/ -name "BUILD.bazel" -delete
	${BAZEL} run //:gazelle
	make -C tools/deb-tools vendor

.PHONY: goget
goget:
	go get $(shell go list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -mod=mod -m all)
	cd tools/deb-tools; go get $(shell go list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -mod=mod -m all)

.PHONY: depup
depup: goget vendor

.PHONY: staticcheck-all
staticcheck-all:
	go list ./... | xargs go run honnef.co/go/tools/cmd/staticcheck@v0.2.1

# staticcheck-working is the subset of packages that we have cleaned up
# We gradually want to sync up staticcheck-all with staticcheck-working
.PHONY: staticcheck-working
staticcheck-working:
	go list ./... | grep -v "etcd-manager/pkg/[cepv]" | xargs go run honnef.co/go/tools/cmd/staticcheck@v0.2.1

.PHONY: ko-dist
ko-dist: ko-export-etcd-manager-slim-amd64 ko-export-etcd-manager-slim-arm64

.PHONY: ko-export-etcd-manager-slim-amd64 ko-export-etcd-manager-slim-arm64
ko-export-etcd-manager-slim-amd64 ko-export-etcd-manager-slim-arm64: ko-export-etcd-manager-slim-%:
	mkdir -p dist
	KO_DEFAULTBASEIMAGE="debian:12-slim" KO_DOCKER_REPO="registry.k8s.io/etcd-manager" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/$* -B --push=false --tarball=dist/etcd-manager-slim-$*.tar ./cmd/etcd-manager/
	gzip -f dist/etcd-manager-slim-$*.tar

.PHONY: ko-push-etcd-manager-slim
ko-push-etcd-manager-slim:
	KO_DEFAULTBASEIMAGE="debian:12-slim" KO_DOCKER_REPO="${IMAGE_BASE}etcd-manager-slim" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/amd64,linux/arm64 --bare ./cmd/etcd-manager/

.PHONY: download-etcd-versions
download-etcd-versions:
	dev/download-etcd.sh 3.1.12
	dev/download-etcd.sh 3.2.18
	dev/download-etcd.sh 3.2.24
	dev/download-etcd.sh 3.3.10
	dev/download-etcd.sh 3.3.13
	dev/download-etcd.sh 3.3.17
	dev/download-etcd.sh 3.4.3
	dev/download-etcd.sh 3.4.13
	dev/download-etcd.sh 3.5.7

.PHONY: test-integration
test-integration: download-etcd-versions
	go test -v ./test/integration
