MODULE := github.com/kumodo-io/convoy
TOOLS_BIN := bin
BOILERPLATE := hack/boilerplate.go.txt

CODE_GEN_VERSION := $(shell go list -m -f '{{.Version}}' k8s.io/code-generator 2>/dev/null)
KUBE_OPENAPI_VERSION := $(shell go list -m -f '{{.Version}}' k8s.io/kube-openapi 2>/dev/null)

CODE_GEN_TOOLS := applyconfiguration-gen client-gen conversion-gen deepcopy-gen informer-gen lister-gen

# Tools

.PHONY: tools
tools: $(addprefix $(TOOLS_BIN)/,$(CODE_GEN_TOOLS)) $(TOOLS_BIN)/openapi-gen

$(TOOLS_BIN)/openapi-gen:
	GOBIN=$(abspath $(TOOLS_BIN)) go install k8s.io/kube-openapi/cmd/openapi-gen@$(KUBE_OPENAPI_VERSION)

$(TOOLS_BIN)/%:
	GOBIN=$(abspath $(TOOLS_BIN)) go install k8s.io/code-generator/cmd/$*@$(CODE_GEN_VERSION)

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
		--output-dir api/generated/applyconfiguration \
		--output-pkg $(MODULE)/api/generated/applyconfiguration \
		$(MODULE)/api/...

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
		$(MODULE)/api/...

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
lint:
	golangci-lint run ./...
