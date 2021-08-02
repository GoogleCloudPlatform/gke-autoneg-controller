
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run tests
test: generate fmt vet manifests
	go test ./... -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	kustomize build config/crd | kubectl apply -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	cd config/manager && kustomize edit set image controller=${IMG}
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./..."

# Create api directory. Circumvents issue with kubebuilder without CRDs
api:
	mkdir api

# Build the docker image
VERSION ?= latest
LABELS ?= --label org.opencontainers.image.licenses="Apache-2.0" \
    --label org.opencontainers.image.vendor="Google LLC" \
    --label org.opencontainers.image.version="$VERSION"
docker-build: test api
	docker build ${LABELS} . -t ${IMG}

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.0
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# Used for autoneg project releases
#

# Release image
RELEASE_IMG ?= ghcr.io/googlecloudplatform/gke-autoneg-controller/gke-autoneg-controller

# Make deployment manifests but do not deploy
autoneg-manifests: manifests
	cd config/manager && kustomize edit set image controller=${RELEASE_IMG}:${VERSION}
	kustomize build config/default > deploy/autoneg.yaml

# Make release image
release-image: docker-build
	docker tag ${IMG} ${RELEASE_IMG}:${VERSION}

# Push release image
release-push: release-image
	docker push ${RELEASE_IMG}:${VERSION}
