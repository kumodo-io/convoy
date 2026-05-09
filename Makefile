MODULE := github.com/kumodo-io/convoy
TOOLS_BIN := internal/tools/bin

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
		--output-file zz_generated.deepcopy.go \
		$(MODULE)/api/...

	$(TOOLS_BIN)/conversion-gen \
		--output-file zz_generated.conversion.go \
		$(MODULE)/api/...

	$(TOOLS_BIN)/applyconfiguration-gen \
		--output-dir internal/generated/applyconfiguration \
		--output-pkg $(MODULE)/internal/generated/applyconfiguration \
		$(MODULE)/api/...

	$(TOOLS_BIN)/client-gen \
		--output-dir internal/generated/clientset \
		--output-pkg $(MODULE)/internal/generated/clientset \
		--clientset-name versioned \
		--input-base $(MODULE)/api \
		$(MODULE)/api/...

	$(TOOLS_BIN)/lister-gen \
		--output-dir internal/generated/listers \
		--output-pkg $(MODULE)/internal/generated/listers \
		$(MODULE)/api/...

	$(TOOLS_BIN)/informer-gen \
		--output-dir internal/generated/informers \
		--output-pkg $(MODULE)/internal/generated/informers \
		--versioned-clientset-package $(MODULE)/internal/generated/clientset/versioned \
		--listers-package $(MODULE)/internal/generated/listers \
		$(MODULE)/api/...

	$(TOOLS_BIN)/openapi-gen \
		--output-dir internal/generated/openapi \
		--output-pkg $(MODULE)/internal/generated/openapi \
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
