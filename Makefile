MODULE := github.com/kumodo-io/convoy
TOOLS_BIN := bin
BOILERPLATE := hack/boilerplate.go.txt

# Add ./bin to path
# export PATH := $(abspath $(TOOLS_BIN)):$(PATH)

# External apply configurations: appended to the built-in defaults (meta/v1).
# applyconfiguration-gen errors silently on referenced external types it doesn't know about.
EXTERNAL_APPLYCONFIGS := k8s.io/api/core/v1.ConfigMapKeySelector:k8s.io/client-go/applyconfigurations/core/v1,k8s.io/api/core/v1.SecretKeySelector:k8s.io/client-go/applyconfigurations/core/v1,k8s.io/api/core/v1.EnvVar:k8s.io/client-go/applyconfigurations/core/v1,k8s.io/api/core/v1.SecretReference:k8s.io/client-go/applyconfigurations/core/v1

CODE_GEN_VERSION := $(shell go list -m -f '{{.Version}}' k8s.io/code-generator 2>/dev/null)
KUBE_OPENAPI_VERSION := $(shell go list -m -f '{{.Version}}' k8s.io/kube-openapi 2>/dev/null)
GOLANGCI_LINT_VERSION := $(shell go list -m -f '{{.Version}}' github.com/golangci/golangci-lint/v2 2>/dev/null)
HERMIT_VERSION := $(shell go list -m -f '{{.Version}}' github.com/cashapp/hermit 2>/dev/null)
TILT_VERSION := 0.37.3
KUSTOMIZE_VERSION := 5.8.1
KIND_VERSION := 0.32.0
KUBECTL_VERSION := 1.35.2

CODE_GEN_TOOLS := applyconfiguration-gen client-gen conversion-gen deepcopy-gen informer-gen lister-gen

# Tools

.PHONY: hermit-init
hermit-init: tools
	$(TOOLS_BIN)/hermit init --no-git

.PHONY: tilt
tilt:
	$(TOOLS_BIN)/hermit install tilt-$(TILT_VERSION)

.PHONY: kustomize
kustomize:
	$(TOOLS_BIN)/hermit install kustomize-$(KUSTOMIZE_VERSION)

.PHONY: kind
kind:
	$(TOOLS_BIN)/hermit install kind-$(KIND_VERSION)

.PHONY: kubectl
kubectl:
	$(TOOLS_BIN)/hermit install kubectl-$(KUBECTL_VERSION)

.PHONY: tools
tools: $(addprefix $(TOOLS_BIN)/,$(CODE_GEN_TOOLS)) $(TOOLS_BIN)/hermit $(TOOLS_BIN)/openapi-gen $(TOOLS_BIN)/golangci-lint

$(TOOLS_BIN)/openapi-gen:
	GOBIN=$(abspath $(TOOLS_BIN)) go install k8s.io/kube-openapi/cmd/openapi-gen@$(KUBE_OPENAPI_VERSION)

$(TOOLS_BIN)/golangci-lint:
	GOBIN=$(abspath $(TOOLS_BIN)) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(TOOLS_BIN)/%:
	GOBIN=$(abspath $(TOOLS_BIN)) go install k8s.io/code-generator/cmd/$*@$(CODE_GEN_VERSION)

$(TOOLS_BIN)/hermit:
	GOBIN=$(abspath $(TOOLS_BIN)) go install github.com/cashapp/hermit/cmd/hermit@$(HERMIT_VERSION)

# Code generation

.PHONY: generate
generate: tools
	$(TOOLS_BIN)/deepcopy-gen \
		--go-header-file $(BOILERPLATE) \
		--output-file zz_generated.deepcopy.go \
		$(MODULE)/api/...

	$(TOOLS_BIN)/conversion-gen \
		--go-header-file $(BOILERPLATE) \
		--output-file zz_generated.conversion.go \
		$(MODULE)/api/...

	$(TOOLS_BIN)/applyconfiguration-gen \
		--go-header-file $(BOILERPLATE) \
		--external-applyconfigurations "$(EXTERNAL_APPLYCONFIGS)" \
		--output-dir api/generated/applyconfiguration \
		--output-pkg $(MODULE)/api/generated/applyconfiguration \
		$(MODULE)/api/core/v1alpha1 \
		$(MODULE)/api/source/v1alpha1 \
		$(MODULE)/api/extensions/v1alpha1 \
		$(MODULE)/api/auth/v1alpha1

	$(TOOLS_BIN)/client-gen \
		--go-header-file $(BOILERPLATE) \
		--output-dir api/generated/clientset \
		--output-pkg $(MODULE)/api/generated/clientset \
		--clientset-name versioned \
		--input-base $(MODULE)/api \
		--input core/v1alpha1 \
		--input source/v1alpha1 \
		--input extensions/v1alpha1

	$(TOOLS_BIN)/lister-gen \
		--go-header-file $(BOILERPLATE) \
		--output-dir api/generated/listers \
		--output-pkg $(MODULE)/api/generated/listers \
		$(MODULE)/api/core/v1alpha1 \
		$(MODULE)/api/source/v1alpha1 \
		$(MODULE)/api/extensions/v1alpha1

	$(TOOLS_BIN)/informer-gen \
		--go-header-file $(BOILERPLATE) \
		--output-dir api/generated/informers \
		--output-pkg $(MODULE)/api/generated/informers \
		--versioned-clientset-package $(MODULE)/api/generated/clientset/versioned \
		--listers-package $(MODULE)/api/generated/listers \
		$(MODULE)/api/core/v1alpha1 \
		$(MODULE)/api/source/v1alpha1 \
		$(MODULE)/api/extensions/v1alpha1

	$(TOOLS_BIN)/openapi-gen \
		--go-header-file $(BOILERPLATE) \
		--output-dir api/generated/openapi \
		--output-pkg $(MODULE)/api/generated/openapi \
		--report-filename hack/openapi-violation-exceptions.list \
		--output-model-name-file zz_generated.model_name.go \
		--readonly-pkg k8s.io/apimachinery/pkg/apis/meta/v1 \
		--readonly-pkg k8s.io/apimachinery/pkg/runtime \
		--readonly-pkg k8s.io/apimachinery/pkg/version \
		--readonly-pkg k8s.io/apimachinery/pkg/util/intstr \
		--readonly-pkg k8s.io/apimachinery/pkg/api/resource \
		--readonly-pkg k8s.io/api/core/v1 \
		--readonly-pkg k8s.io/api/batch/v1 \
		--readonly-pkg k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1 \
		$(MODULE)/api/... \
		k8s.io/apimachinery/pkg/apis/meta/v1 \
		k8s.io/apimachinery/pkg/runtime \
		k8s.io/apimachinery/pkg/version \
		k8s.io/apimachinery/pkg/util/intstr \
		k8s.io/apimachinery/pkg/api/resource \
		k8s.io/api/core/v1 \
		k8s.io/api/batch/v1 \
		k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1

# Dependencies

.PHONY: tidy
tidy:
	go mod tidy

# Build

.PHONY: build
build:
	go build ./...

# Test

.PHONY: test
test:
	go test ./...

.PHONY: lint
lint: $(TOOLS_BIN)/golangci-lint
	$(TOOLS_BIN)/golangci-lint run ./...

# Local dev cluster (kind + Tilt)

KIND_CLUSTER     := convoy-dev
API_SERVER_IMAGE := convoy-apiserver:dev
CONTROLLER_IMAGE := convoy-controller-manager:dev

# Build static linux binaries matching the local arch (kind nodes are linux/$GOARCH).
.PHONY: build-linux
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(shell go env GOARCH) go build -o bin/convoy-apiserver ./cmd/convoy-apiserver
	CGO_ENABLED=0 GOOS=linux GOARCH=$(shell go env GOARCH) go build -o bin/convoy-controller-manager ./cmd/convoy-controller-manager

.PHONY: image
image:
	docker build --target apiserver -t $(API_SERVER_IMAGE) .
	docker build --target controller-manager -t $(CONTROLLER_IMAGE) .

.PHONY: kind-up
kind-up: kind
	$(TOOLS_BIN)/kind get clusters | grep -qx $(KIND_CLUSTER) || $(TOOLS_BIN)/kind create cluster --config hack/kind/kind-config.yaml

.PHONY: kind-down
kind-down: kind
	$(TOOLS_BIN)/kind delete cluster --name $(KIND_CLUSTER)

# Build, load, and deploy convoy into the kind cluster (no Tilt required).
.PHONY: kind-deploy
kind-deploy: image kind kubectl
	$(TOOLS_BIN)/kind load docker-image $(API_SERVER_IMAGE) --name $(KIND_CLUSTER)
	$(TOOLS_BIN)/kind load docker-image $(CONTROLLER_IMAGE) --name $(KIND_CLUSTER)
	$(TOOLS_BIN)/kubectl apply -k hack/kind
	$(TOOLS_BIN)/kubectl -n convoy-system rollout status deploy/convoy-etcd --timeout=120s
	$(TOOLS_BIN)/kubectl -n convoy-system rollout status deploy/convoy-apiserver --timeout=120s
	$(TOOLS_BIN)/kubectl wait --for=condition=Available --timeout=120s \
		apiservice/v1alpha1.core.convoy.kumodo.io \
		apiservice/v1alpha1.source.convoy.kumodo.io \
		apiservice/v1alpha1.extensions.convoy.kumodo.io
	$(TOOLS_BIN)/kubectl -n convoy-system rollout status deploy/convoy-controller-manager --timeout=120s

# Smoke-test the served API end-to-end.
.PHONY: kind-test
kind-test: kubectl
	$(TOOLS_BIN)/kubectl apply -f hack/kind/examples/cluster.yaml
	$(TOOLS_BIN)/kubectl get clusters -A
	$(TOOLS_BIN)/kubectl api-resources --api-group=core.convoy.kumodo.io

# Tilt dev loop. Builds, deploys, and live-reloads the apiserver in place on
# code changes.
.PHONY: tilt-up
tilt-up: tilt kustomize
	$(TOOLS_BIN)/tilt up

.PHONY: tilt-down
tilt-down: tilt
	$(TOOLS_BIN)/tilt down

# One-shot: ensure the kind cluster exists, then start Tilt.
.PHONY: dev
dev: kind-up tilt-up
