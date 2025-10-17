# # /bin/sh does not support source command needed in make all-test
#SHELL := /bin/bash

ROOT_DIR=$(shell git rev-parse --show-toplevel)
# Openshift platform supported version
OPENSHIFT_VERSION="v4.10"

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
# TODO: Version must be pulled from git tags
VERSION ?= 4.1.1

# Platforms supported
PLATFORMS ?= linux/amd64,linux/arm64

# bundle channels
CHANNELS ?= stable
DEFAULT_CHANNEL ?= stable

OS := $(shell uname -s)
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%S%Z")
# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-dockerfile docker-buildx-catalog' will build and push both
# aerospike.com/aerospike-kubernetes-operator-bundle:$VERSION and aerospike.com/aerospike-kubernetes-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= aerospike/aerospike-kubernetes-operator-nightly

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
    BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing operator manager image targets
IMG ?= controller:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

BUNDLE_DIR:= bundle
CATALOG_DIR:= catalog
ANNOTATIONS_FILE_PATH:= $(BUNDLE_DIR)/metadata/annotations.yaml
OVERLAYS_DIR:= $(ROOT_DIR)/config/overlays/manifests/olm

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	cp $(ROOT_DIR)/config/crd/bases/asdb.aerospike.com_aerospikeclusters.yaml $(ROOT_DIR)/helm-charts/aerospike-kubernetes-operator/crds/customresourcedefinition_aerospikeclusters.asdb.aerospike.com.yaml
	cp $(ROOT_DIR)/config/crd/bases/asdb.aerospike.com_aerospikebackupservices.yaml $(ROOT_DIR)/helm-charts/aerospike-kubernetes-operator/crds/customresourcedefinition_aerospikebackupservices.asdb.aerospike.com.yaml
	cp $(ROOT_DIR)/config/crd/bases/asdb.aerospike.com_aerospikebackups.yaml $(ROOT_DIR)/helm-charts/aerospike-kubernetes-operator/crds/customresourcedefinition_aerospikebackups.asdb.aerospike.com.yaml
	cp $(ROOT_DIR)/config/crd/bases/asdb.aerospike.com_aerospikerestores.yaml $(ROOT_DIR)/helm-charts/aerospike-kubernetes-operator/crds/customresourcedefinition_aerospikerestores.asdb.aerospike.com.yaml

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

.PHONY: go-lint
go-lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run

.PHONY: go-lint-fix
go-lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: all-test
all-test: manifests generate fmt vet setup-envtest cluster-test backup-service-test backup-test restore-test ## Run tests.

.PHONY: cluster-test
cluster-test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" cd $(shell pwd)/test/cluster; mkdir -p ../test-results; go run github.com/onsi/ginkgo/v2/ginkgo --grace-period=10m -p --procs=8 -coverprofile ascover.out -v -show-node-events --focus="$(FOCUS)" -timeout=5h0m0s --junit-report=../test-results/junit-cluster.xml  -- ${ARGS}

.PHONY: backup-service-test
backup-service-test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" cd $(shell pwd)/test/backup_service; mkdir -p ../test-results; go run github.com/onsi/ginkgo/v2/ginkgo --grace-period=10m -p --procs=8 -coverprofile abscover.out -v -show-node-events -timeout=1h0m0s --junit-report=../test-results/junit-backup-service.xml  -- ${ARGS}

.PHONY: backup-test
backup-test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" cd $(shell pwd)/test/backup; mkdir -p ../test-results; go run github.com/onsi/ginkgo/v2/ginkgo -coverprofile abcover.out -v -show-node-events -timeout=1h0m0s --junit-report=../test-results/junit-backup.xml  -- ${ARGS}

.PHONY: restore-test
restore-test: manifests generate fmt vet setup-envtest ## Run tests.
	# KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" cd $(shell pwd)/test/restore; mkdir -p ../test-results; go run github.com/onsi/ginkgo/v2/ginkgo -coverprofile arcover.out -v -show-node-events -timeout=1h0m0s --junit-report=../test-results/junit-restore.xml  -- ${ARGS}

PHONY: env-test
env-test:  fmt vet setup-envtest ## Run tests.
	export KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)"; cd $(shell pwd)/test/envtests; mkdir -p ../test-results; go run github.com/onsi/ginkgo/v2/ginkgo -coverprofile envcover.out -v -show-node-events -timeout=1h0m0s  --junit-report=../test-results/junit-envtests.xml -- ${ARGS}

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --no-cache --provenance=false --platform=$(PLATFORMS) --tag ${IMG} --build-arg VERSION=$(VERSION) .
	- docker buildx rm project-v3-builder

.PHONY: docker-buildx-openshift
docker-buildx-openshift: ## Build and push docker image for the manager for openshift cross-platform support
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --no-cache --provenance=false --platform=$(PLATFORMS) --tag ${IMG} --build-arg VERSION=$(VERSION) --build-arg USER=1001 .
	- docker buildx rm project-v3-builder

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl replace -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: test-deploy
test-deploy: manifests kustomize
	cp -r config test
	cd test/config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	cp test/manager-patch-for-tests.yaml test/config/manager/manager-patch-for-tests.yaml
	cd test/config/manager && $(KUSTOMIZE) edit add patch --path ./manager-patch-for-tests.yaml
	cd test/config/default && $(KUSTOMIZE) edit set namespace ${NS}
	$(KUSTOMIZE) build test/config/default | kubectl apply -f -

.PHONY: test-undeploy
test-undeploy: kustomize
	cp -r config test
	cp test/manager-patch-for-tests.yaml test/config/manager/manager-patch-for-tests.yaml
	cd test/config/manager && $(KUSTOMIZE) edit add patch --path ./manager-patch-for-tests.yaml
	cd test/config/default && $(KUSTOMIZE) edit set namespace ${NS}
	$(KUSTOMIZE) build test/config/default | kubectl delete -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.6.0
CONTROLLER_TOOLS_VERSION ?= v0.17.2
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION := $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION := $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v1.63.4
# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.40.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
	echo "Error setting up envtest"; exit 1; }

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))


# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: enable-pre-commit
enable-pre-commit:
	pip3 install pre-commit
	pre-commit install

.PHONY: submodules
submodules: ## Pull and update git submodules recursively
	git pull --recurse-submodules
	git submodule update --init --recursive

# Generate bundle manifests and metadata, then validate generated files.
# For OpenShift bundles run
# CHANNELS=stable DEFAULT_CHANNEL=stable OPENSHIFT_VERSION=v4.10 IMG=docker.io/aerospike/aerospike-kubernetes-operator-nightly:4.1.1 make bundle
.PHONY: bundle
bundle: manifests kustomize operator-sdk
	rm -rf $(ROOT_DIR)/bundle.Dockerfile $(BUNDLE_DIR)
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd $(ROOT_DIR)/config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd $(ROOT_DIR)/config/manifests/bases && $(KUSTOMIZE) edit set annotation createdAt:$(DATE)
	cd $(ROOT_DIR) && $(KUSTOMIZE) build $(OVERLAYS_DIR) | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS) --output-dir $(BUNDLE_DIR)
	$(OPERATOR_SDK) bundle validate $(BUNDLE_DIR)
	sed -i "s@containerImage: controller:latest@containerImage: $(IMG)@g" $(BUNDLE_DIR)/manifests/aerospike-kubernetes-operator.clusterserviceversion.yaml
	sed -i "/^FROM.*/a LABEL com.redhat.openshift.versions="$(OPENSHIFT_VERSION)"" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^FROM.*/a LABEL com.redhat.delivery.operator.bundle=true" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^FROM.*/a LABEL com.redhat.delivery.backport=false" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^FROM.*/a # Labels for RedHat Openshift Platform" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^annotations.*/a \  com.redhat.openshift.versions: "$(OPENSHIFT_VERSION)"" $(BUNDLE_DIR)/metadata/annotations.yaml; \
	sed -i "/^annotations.*/a \  # Annotations for RedHat Openshift Platform" $(BUNDLE_DIR)/metadata/annotations.yaml;

# Remove generated bundle
.PHONY: bundle-clean
bundle-clean:
	rm -rf $(ROOT_DIR)/bundle.Dockerfile $(BUNDLE_DIR)

# Build the bundle image.
# Bundle images are not architecture-specific. They contain only plaintext kubernetes manifests and operator metadata.
.PHONY: bundle-build
bundle-build:
	docker build --pull --no-cache -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.55.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-dockerfile BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make docker-buildx-catalog CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog
catalog: opm ## Generate a file-based catalog and its dockerfile.
	rm -rf $(CATALOG_DIR).Dockerfile $(CATALOG_DIR)
	mkdir $(CATALOG_DIR)
	$(OPM) index add --container-tool docker --mode semver --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT) --generate
	$(OPM) migrate database/index.db $(CATALOG_DIR) --output=yaml
	$(OPM) generate dockerfile $(CATALOG_DIR)
	$(OPM) validate $(CATALOG_DIR)

# Build and push multi-arch catalog image.
.PHONY: docker-buildx-catalog
docker-buildx-catalog: catalog
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --no-cache --platform=$(PLATFORMS) --tag $(CATALOG_IMG) -f $(CATALOG_DIR).Dockerfile .
	- docker buildx rm project-v3-builder
