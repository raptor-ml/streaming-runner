VERSION ?= $(shell git rev-parse --short HEAD)

IMAGE_BASE ?= ghcr.io/raptor-ml/streaming-runner

## Configuring the environment mode
ENV ?= dev
ifneq ($(origin PROD),undefined)
  ENV = prod
endif

ifeq ($(ENV),prod)
  CONTEXT ?= gke_raptor-test_europe-west3-a_raptor-test
  $(info $(shell tput setaf 1)-+-+ PROD MODE -+-+$(shell tput sgr0))
else
  CONTEXT ?= kind-kind
  $(info $(shell tput setaf 2)+-+- DEV MODE +-+-$(shell tput sgr0))
endif
$(info $(shell tput setaf 3)Context: $(shell tput sgr0)$(CONTEXT))
$(info $(shell tput setaf 3)Version: $(shell tput sgr0)$(VERSION))
$(info $(shell tput setaf 3)Base Image: $(shell tput sgr0)$(IMAGE_BASE))
$(info )
.DEFAULT_GOAL := help

LOCALBIN ?= $(shell pwd)/bin

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

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
.PHONY: golangci-lint
golangci-lint:
	@[ -f $(GOLANGCI_LINT) ] || { \
		set -e ;\
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) v1.50.1 ;\
	}

.PHONY: check-license-headers
check-license:  ## Check the headers for the license.
	./hack/check-headers-for-license.sh

##@ Build

.PHONY: build
build: fmt lint ## Build core binary.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="-s -w -X main.version=\${{ steps.version.outputs.version }}" \
		-o bin/runner cmd/streaming/*

.PHONY: docker-build
docker-build: build ## Build docker image with the Runner.
	docker build -t ${IMAGE_BASE}:${VERSION} . -f hack/release.Dockerfile

.PHONY: kind-load
kind-load: ## Load the runner into kind.
	kind load docker-image ${IMAGE_BASE}:${VERSION}