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

.PHONY: all
all: test

.PHONY: test
test:
	go test -v -short ./...

# Must match AllEtcdVersions in pkg/etcdversions/mappings.go
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
	dev/download-etcd.sh 3.5.0
	dev/download-etcd.sh 3.5.1
	dev/download-etcd.sh 3.5.3
	dev/download-etcd.sh 3.5.4
	dev/download-etcd.sh 3.5.6
	dev/download-etcd.sh 3.5.7

.PHONY: test-integration
test-integration: download-etcd-versions
	go test -v ./test/integration/backuprestore

.PHONY: test-backuprestore
test-backuprestore: download-etcd-versions
	go test -v ./test/integration/backuprestore

.PHONY: test-upgradedowngrade
test-upgradedowngrade: download-etcd-versions
	go test -v ./test/integration/upgradedowngrade

.PHONY: gofmt
gofmt:
	gofmt -w -s cmd/ pkg/

.PHONY: goimports
goimports:
	goimports -w cmd/ pkg/ test/

.PHONY: build-etcd-manager-amd64 build-etcd-manager-arm64
build-etcd-manager-amd64 build-etcd-manager-arm64: build-etcd-manager-%:
	mkdir -p dist/linux/$*
	GOOS=linux GOARCH=$* go build -o dist/linux/$*/etcd-manager sigs.k8s.io/etcd-manager/cmd/etcd-manager

.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor

.PHONY: goget
goget:
	go get $(shell go list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -mod=mod -m all)

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

.PHONY: images-amd64
images-amd64: export-etcd-manager-ctl-amd64 export-etcd-manager-slim-amd64 export-etcd-backup-amd64 export-etcd-dump-amd64

.PHONY: images-arm64
images-arm64: export-etcd-manager-ctl-arm64 export-etcd-manager-slim-arm64 export-etcd-backup-arm64 export-etcd-dump-arm64

.PHONY: images
images: images-amd64 images-arm64

.PHONY: push
push: push-etcd-manager-slim push-etcd-backup push-etcd-backup push-etcd-dump

.PHONY: export-etcd-manager-ctl-amd64 export-etcd-manager-ctl-arm64
export-etcd-manager-ctl-amd64 export-etcd-manager-ctl-arm64: export-etcd-manager-ctl-%:
	mkdir -p dist
	KO_DOCKER_REPO="registry.k8s.io/etcd-manager/etcd-manager-ctl" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/$* -B --push=false --tarball=dist/etcd-manager-ctl-$*.tar ./cmd/etcd-manager/
	gzip -f dist/etcd-manager-ctl-$*.tar

.PHONY: push-etcd-manager-ctl
push-etcd-manager-ctl:
	KO_DOCKER_REPO="${IMAGE_BASE}etcd-manager-ctl" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/amd64,linux/arm64 --bare ./cmd/etcd-manager-ctl/

.PHONY: export-etcd-manager-slim-amd64 export-etcd-manager-slim-arm64
export-etcd-manager-slim-amd64 export-etcd-manager-slim-arm64: export-etcd-manager-slim-%:
	mkdir -p dist
	KO_DOCKER_REPO="registry.k8s.io/etcd-manager/etcd-manager-slim" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/$* -B --push=false --tarball=dist/etcd-manager-slim-$*.tar ./cmd/etcd-manager/
	gzip -f dist/etcd-manager-slim-$*.tar

.PHONY: push-etcd-manager-slim
push-etcd-manager-slim: push-etcd-manager-ctl
	KO_DEFAULTBASEIMAGE="${IMAGE_BASE}etcd-manager-ctl:${STABLE_DOCKER_TAG}" KO_DOCKER_REPO="${IMAGE_BASE}etcd-manager-slim" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/amd64,linux/arm64 --bare ./cmd/etcd-manager/

.PHONY: export-etcd-backup-amd64 export-etcd-backup-arm64
export-etcd-backup-amd64 export-etcd-backup-arm64: export-etcd-backup-%:
	mkdir -p dist
	KO_DOCKER_REPO="registry.k8s.io/etcd-manager/etcd-backup" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/$* -B --push=false --tarball=dist/etcd-backup-$*.tar ./cmd/etcd-backup/
	gzip -f dist/etcd-backup-$*.tar

.PHONY: push-etcd-backup
push-etcd-backup:
	KO_DOCKER_REPO="${IMAGE_BASE}etcd-backup" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/amd64,linux/arm64 --bare ./cmd/etcd-backup/

.PHONY: export-etcd-dump-amd64 export-etcd-dump-arm64
export-etcd-dump-amd64 export-etcd-dump-arm64: export-etcd-dump-%:
	mkdir -p dist
	KO_DOCKER_REPO="registry.k8s.io/etcd-manager/etcd-dump" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/$* -B --push=false --tarball=dist/etcd-dump-$*.tar ./cmd/etcd-dump/
	gzip -f dist/etcd-dump-$*.tar

.PHONY: push-etcd-dump
push-etcd-dump:
	KO_DOCKER_REPO="${IMAGE_BASE}etcd-dump" ${KO} build --tags ${STABLE_DOCKER_TAG} --platform=linux/amd64,linux/arm64 --bare ./cmd/etcd-dump/

