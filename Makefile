ROOT_DIR=$(shell git rev-parse --show-toplevel)
PACKAGE_NAME=$(shell basename $(shell git rev-parse --show-toplevel))
# Openshift platform supported version
OPENSHIFT_VERSION="v4.6"

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
# TODO: Version must be pulled from git tags
VERSION ?= 2.1.0

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

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

ifeq ($(MAKECMDGOALS), bundle)
DISTRIBUTION:= operatorhub
else ifeq ($(MAKECMDGOALS), bundle-okd)
DISTRIBUTION:= okd
else ifeq ($(MAKECMDGOALS), bundle-rhmp)
DISTRIBUTION:= rhmp
USE_IMAGE_DIGESTS:= true
endif

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
    BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
IMG ?= controller:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.24

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

ifneq ($(origin DISTRIBUTION), undefined)
#BUNDLE_DIR:= $(ROOT_DIR)/bundle/$(DISTRIBUTION)/$(VERSION)
BUNDLE_DIR:= ./bundle/$(DISTRIBUTION)/$(VERSION)
ANNOTATIONS_FILE_PATH:= $(BUNDLE_DIR)/metadata/annotations.yaml
OVERLAYS_DIR:= $(ROOT_DIR)/config/overlays/base
ifeq ($(DISTRIBUTION), rhmp)
OVERLAYS_DIR:= $(ROOT_DIR)/config/overlays/$(DISTRIBUTION)
BUNDLE_METADATA_OPTS:= $(BUNDLE_METADATA_OPTS) --use-image-digests
endif
endif

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
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

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

ifndef ignore-not-found
  ignore-not-found = false
endif

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl replace --force -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

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
KUSTOMIZE_VERSION ?= v4.5.7
CONTROLLER_TOOLS_VERSION ?= v0.9.2

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

.PHONY: bundle
bundle: manifests kustomize
	rm -rf $(ROOT_DIR)/bundle.Dockerfile $(BUNDLE_DIR)
	operator-sdk generate kustomize manifests --interactive=false -q
	cd $(ROOT_DIR)/config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd $(ROOT_DIR)/config/manifests/bases && $(KUSTOMIZE) edit set annotation containerImage:$(IMG) createdAt:$(DATE)
	cd $(ROOT_DIR) && $(KUSTOMIZE) build $(OVERLAYS_DIR) | \
    operator-sdk generate bundle $(BUNDLE_GEN_FLAGS) --output-dir $(BUNDLE_DIR); \
    sed -i "s@name: role-place-holder@name: aerospike-kubernetes-operator-default-ns@g" \
    $(BUNDLE_DIR)/manifests/aerospike-kubernetes-operator-default-ns_rbac.authorization.k8s.io_v1_clusterrolebinding.yaml
	if [ $(DISTRIBUTION) == operatorhub ]; then \
        operator-sdk bundle validate $(BUNDLE_DIR) --select-optional name=$(DISTRIBUTION); \
    else \
      if [ $(DISTRIBUTION) == okd ]; then \
          sed -i "/^annotations.*/a \  com.redhat.openshift.versions: $(OPENSHIFT_VERSION)" $(ANNOTATIONS_FILE_PATH); \
          sed -i "/^annotations.*/a \  # Annotations for RedHat Openshift Platform" $(ANNOTATIONS_FILE_PATH); \
          sed -i "/^FROM.*/a LABEL com.redhat.openshift.versions=$(OPENSHIFT_VERSION)" $(ROOT_DIR)/bundle.Dockerfile; \
          sed -i "/^FROM.*/a LABEL com.redhat.delivery.operator.bundle=true" $(ROOT_DIR)/bundle.Dockerfile; \
          sed -i "/^FROM.*/a LABEL com.redhat.delivery.backport=false" $(ROOT_DIR)/bundle.Dockerfile; \
          sed -i "/^FROM.*/a # Labels for RedHat Openshift Platform" $(ROOT_DIR)/bundle.Dockerfile; \
        else \
          mv $(BUNDLE_DIR)/manifests/$(PACKAGE_NAME).clusterserviceversion.yaml \
          $(BUNDLE_DIR)/manifests/$(PACKAGE_NAME)-$(DISTRIBUTION).clusterserviceversion.yaml; \
          sed -i "s/$(PACKAGE_NAME)/$(PACKAGE_NAME)-$(DISTRIBUTION)/" \
          $(ANNOTATIONS_FILE_PATH) $(ROOT_DIR)/bundle.Dockerfile; \
          sed -i "/^annotations.*/a \  com.redhat.openshift.versions: $(OPENSHIFT_VERSION)" $(ANNOTATIONS_FILE_PATH); \
          sed -i "/^annotations.*/a \  marketplace.openshift.io/remote-workflow: https://marketplace.redhat.com/en-us/operators/aerospike-kubernetes-operator-rhmp/pricing?utm_source=openshift_console" $(ANNOTATIONS_FILE_PATH); \
          sed -i "/^annotations.*/a \  marketplace.openshift.io/support-workflow: https://marketplace.redhat.com/en-us/operators/aerospike-kubernetes-operator-rhmp/support?utm_source=openshift_console" $(ANNOTATIONS_FILE_PATH); \
          sed -i "/^FROM.*/a LABEL com.redhat.openshift.versions=$(OPENSHIFT_VERSION)" "$(ROOT_DIR)"/bundle.Dockerfile; \
          sed -i "/^FROM.*/a LABEL marketplace.openshift.io/remote-workflow: https://marketplace.redhat.com/en-us/operators/aerospike-kubernetes-operator-rhmp/pricing?utm_source=openshift_console" $(ROOT_DIR)/bundle.Dockerfile; \
          sed -i "/^FROM.*/a LABEL marketplace.openshift.io/support-workflow: https://marketplace.redhat.com/en-us/operators/aerospike-kubernetes-operator-rhmp/support?utm_source=openshift_console" $(ROOT_DIR)/bundle.Dockerfile; \
          sed -i "/^FROM.*/a # Labels for RedHat Openshift Platform" $(ROOT_DIR)/bundle.Dockerfile; \
        fi; \
        	operator-sdk bundle validate $(BUNDLE_DIR) --select-optional suite=operatorframework ; \
        fi; \


.PHONY: bundle-okd
bundle-okd: bundle

.PHONY: bundle-rhmp
bundle-rhmp: bundle

# Remove generated bundle
.PHONY: bundle-clean
bundle-clean:
	rm -rf bundle bundle.Dockerfile
	rm -rf bundle

.PHONY: bundle-clean-operatorhub
bundle-clean-operatorhub:
	rm -rf bundle/operatorhub

.PHONY: bundle-clean-okd
bundle-clean-okd:
	rm -rf bundle/okd

.PHONY: bundle-clean-rhmp
bundle-clean-rhmp:
	rm -rf bundle/rhmp

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
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
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