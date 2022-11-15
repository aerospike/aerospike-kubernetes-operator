# # /bin/sh does not support source command needed in make test
#SHELL := /bin/bash

ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

# Openshift platform supported version
OPENSHIFT_VERSION="v4.6"

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
# TODO: Version must be pulled from git tags
VERSION ?= 2.2.1

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
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# aerospike.com/aerospike-kubernetes-operator-bundle:$VERSION and aerospike.com/aerospike-kubernetes-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= aerospike/aerospike-kubernetes-operator-nightly

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false,maxDescLen=70"

# # ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
# ENVTEST_K8S_VERSION = 1.21

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

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

test: manifests generate fmt vet envtest ## Run tests.
	# KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" cd $(shell pwd)/test; go run github.com/onsi/ginkgo/v2/ginkgo -coverprofile cover.out -progress -v -timeout=12h0m0s -focus=${FOCUS} --junit-report="junit.xml"  -- ${ARGS}

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

# docker-build: test ## Build docker image with the manager.
docker-build: ## Build docker image with the manager.
	docker build --pull --no-cache -t ${IMG} --build-arg VERSION=$(VERSION) .

docker-build-openshift: ## Build openshift docker image with the manager.
	docker build --pull --no-cache -t ${IMG} --build-arg VERSION=$(VERSION) --build-arg USER=1001 .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

test-deploy: manifests kustomize
	cp -r config test
	cd test/config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	cp test/manager-patch-for-tests.yaml test/config/manager/manager-patch-for-tests.yaml
	cd test/config/manager && $(KUSTOMIZE) edit add patch --path ./manager-patch-for-tests.yaml
	cd test/config/default && $(KUSTOMIZE) edit set namespace ${NS}
	$(KUSTOMIZE) build test/config/default | kubectl apply -f -

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
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v4.2.0
CONTROLLER_TOOLS_VERSION ?= v0.6.1

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Generate bundle manifests and metadata, then validate generated files.
# For OpenShift bundles run
# CHANNELS=stable DEFAULT_CHANNEL=stable OPENSHIFT_VERSION=v4.6 IMG=docker.io/aerospike/aerospike-kubernetes-operator-nightly:2.2.1-5-dev make bundle
.PHONY: bundle
bundle: manifests kustomize
	rm -rf bundle.Dockerfile bundle/
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/overlays/manifests/olm | operator-sdk generate bundle -q --kustomize-dir config/overlays/manifests/olm --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle
	sed -i "s@createdAt: dateplaceholder@createdAt: $(DATE)@g" bundle/manifests/aerospike-kubernetes-operator.clusterserviceversion.yaml; \
	sed -i "s@containerImage: controller:latest@containerImage: $(IMG)@g" bundle/manifests/aerospike-kubernetes-operator.clusterserviceversion.yaml; \
	sed -i "/^FROM.*/a LABEL com.redhat.openshift.versions="$(OPENSHIFT_VERSION)"" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^FROM.*/a LABEL com.redhat.delivery.operator.bundle=true" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^FROM.*/a LABEL com.redhat.delivery.backport=false" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^FROM.*/a # Labels for RedHat Openshift Platform" $(ROOT_DIR)/bundle.Dockerfile; \
	sed -i "/^annotations.*/a \  com.redhat.openshift.versions: "$(OPENSHIFT_VERSION)"" bundle/metadata/annotations.yaml; \
	sed -i "/^annotations.*/a \  # Annotations for RedHat Openshift Platform" bundle/metadata/annotations.yaml; \
	sed -i "s@name: role-place-holder@name: aerospike-kubernetes-operator-default-ns@g" bundle/manifests/aerospike-kubernetes-operator-default-ns_rbac.authorization.k8s.io_v1_clusterrolebinding.yaml


# Remove generated bundle
.PHONY: bundle-clean
bundle-clean:
	rm -rf bundle bundle.Dockerfile

# Build the bundle image.
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
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.15.1/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)
