
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Previously we produced CRDs that work back to Kubernetes 1.11 (no version conversion),
# but now we'll support only 1.16+.
CRD_OPTIONS ?= "crd"

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

ENVTEST_ASSETS_DIR = $(shell pwd)/testbin
ENVTEST = $(shell pwd)/bin/setup-envtest
ENVTEST_K8S_VERSION ?= 1.26.1

testenv:
	mkdir -p ${ENVTEST_ASSETS_DIR}
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

test: manifests generate fmt vet testenv ## Run tests.
	KUBEBUILDER_ASSETS="$(shell ${ENVTEST} use ${ENVTEST_K8S_VERSION} --bin-dir ${ENVTEST_ASSETS_DIR} -p path)" go test -v ./... -coverprofile cover.out

##@ Build

# Build the docker image
DOCKER_BIN ?= docker
VERSION ?= latest
LABELS ?= --label org.opencontainers.image.licenses="Apache-2.0" \
    --label org.opencontainers.image.vendor="Google LLC" \
    --label org.opencontainers.image.version="${VERSION}"

BUILD_TIME = $(shell date)

docker-build: test
	${DOCKER_BIN} build ${DOCKER_FLAGS} ${LABELS} . -t ${IMG}

docker-buildx: test
	docker buildx build ${DOCKER_FLAGS} ${LABELS} . -t ${IMG}

# Push the docker image
docker-push:
	${DOCKER_BIN} push ${DOCKER_FLAGS} ${IMG}

build: generate fmt vet ## Build manager binary.
	go build -ldflags='-X "main.BuildTime=${BUILD_TIME}"' -o bin/manager main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run -ldflags='-X "main.BuildTime=${BUILD_TIME}"' ./main.go

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


CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.3)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Used for autoneg project releases
#

# Release image
RELEASE_IMG ?= ghcr.io/googlecloudplatform/gke-autoneg-controller/gke-autoneg-controller

# Make deployment manifests but do not deploy
autoneg-manifests: manifests
	cd config/manager && $(KUSTOMIZE) edit set image controller=${RELEASE_IMG}:${VERSION}
	cp hack/boilerplate.bash.txt deploy/autoneg.yaml
	$(KUSTOMIZE) build config/default >> deploy/autoneg.yaml

# Make release image
release-image: docker-build
	${DOCKER_BIN} tag ${IMG} ${RELEASE_IMG}:${VERSION}

# Push release image
release-push: release-image
	${DOCKER_BIN} push ${RELEASE_IMG}:${VERSION}

helm: helm-docs helm-lint

helm-lint:
	helm lint deploy/chart

helm-docs:
	helm-docs
