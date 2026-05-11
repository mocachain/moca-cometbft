include common.mk

PACKAGES=$(shell go list ./...)
BUILDDIR?=$(CURDIR)/build
OUTPUT?=$(BUILDDIR)/cometbft
GO ?= $(shell command -v go 2>/dev/null || echo go)
GO_LOCAL_ENV ?= env -u GOROOT GOTOOLCHAIN=local
GO_GOPATH ?= $(shell $(GO_LOCAL_ENV) $(GO) env GOPATH 2>/dev/null)
GO_BIN ?= $(or $(GOBIN),$(if $(GO_GOPATH),$(GO_GOPATH)/bin,$(HOME)/go/bin))
LEFTHOOK ?= $(GO_BIN)/lefthook
LEFTHOOK_VERSION ?= v1.11.3
GOLANGCI_LINT ?= $(GO_BIN)/golangci-lint
GOLANGCI_LINT_VERSION ?= v1.64.8

HTTPS_GIT := https://github.com/cometbft/cometbft.git
CGO_ENABLED ?= 1

# Process Docker environment varible TARGETPLATFORM
# in order to build binary with correspondent ARCH
# by default will always build for linux/amd64
TARGETPLATFORM ?=
GOOS ?= linux
GOARCH ?= amd64
GOARM ?=

ifeq (linux/arm,$(findstring linux/arm,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=arm
	GOARM=7
endif

ifeq (linux/arm/v6,$(findstring linux/arm/v6,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=arm
	GOARM=6
endif

ifeq (linux/arm64,$(findstring linux/arm64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=arm64
	GOARM=7
endif

ifeq (linux/386,$(findstring linux/386,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=386
endif

ifeq (linux/amd64,$(findstring linux/amd64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=amd64
endif

ifeq (linux/mips,$(findstring linux/mips,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mips
endif

ifeq (linux/mipsle,$(findstring linux/mipsle,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mipsle
endif

ifeq (linux/mips64,$(findstring linux/mips64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mips64
endif

ifeq (linux/mips64le,$(findstring linux/mips64le,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mips64le
endif

ifeq (linux/riscv64,$(findstring linux/riscv64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=riscv64
endif

#? all: Run target build, test and install
all: build test install
.PHONY: all

include tests.mk

###############################################################################
###                                Build CometBFT                           ###
###############################################################################

#? build: Build CometBFT
build:
	CGO_ENABLED=$(CGO_ENABLED) go build $(BUILD_FLAGS) -tags '$(BUILD_TAGS)' -o $(OUTPUT) ./cmd/cometbft/
.PHONY: build

#? install: Install CometBFT to GOBIN
install:
	CGO_ENABLED=$(CGO_ENABLED) go install $(BUILD_FLAGS) -tags $(BUILD_TAGS) ./cmd/cometbft
.PHONY: install

#? hooks: Install git hooks managed by lefthook
hooks:
	@if [ ! -x "$(LEFTHOOK)" ]; then \
		echo "--> Installing lefthook $(LEFTHOOK_VERSION) into $(GO_BIN)"; \
		$(GO_LOCAL_ENV) GOBIN=$(GO_BIN) $(GO) install github.com/evilmartians/lefthook@$(LEFTHOOK_VERSION); \
	else \
		echo "--> Using lefthook binary: $(LEFTHOOK)"; \
	fi
	@$(LEFTHOOK) install
.PHONY: hooks

###############################################################################
###                               Metrics                                   ###
###############################################################################

#? metrics: Generate metrics
metrics: testdata-metrics
	go generate -run="scripts/metricsgen" ./...
.PHONY: metrics

# By convention, the go tool ignores subdirectories of directories named
# 'testdata'. This command invokes the generate command on the folder directly
# to avoid this.
#? testdata-metrics: Generate test data for metrics
testdata-metrics:
	ls ./scripts/metricsgen/testdata | xargs -I{} go generate -v -run="scripts/metricsgen" ./scripts/metricsgen/testdata/{}
.PHONY: testdata-metrics

###############################################################################
###                                Mocks                                    ###
###############################################################################

#? mockery: Generate test mocks
mockery:
	go generate -run="./scripts/mockery_generate.sh" ./...
.PHONY: mockery

###############################################################################
###                                Protobuf                                 ###
###############################################################################

#? check-proto-deps: Check protobuf deps
check-proto-deps:
ifeq (,$(shell which protoc-gen-gogofaster))
	@go install github.com/cosmos/gogoproto/protoc-gen-gogofaster@latest
endif
.PHONY: check-proto-deps

#? check-proto-format-deps: Check protobuf format deps
check-proto-format-deps:
ifeq (,$(shell which clang-format))
	$(error "clang-format is required for Protobuf formatting. See instructions for your platform on how to install it.")
endif
.PHONY: check-proto-format-deps

#? proto-gen: Generate protobuf files
proto-gen: check-proto-deps
	@echo "Generating Protobuf files"
	@go run github.com/bufbuild/buf/cmd/buf@latest generate
	@mv ./proto/tendermint/abci/types.pb.go ./abci/types/
	@cp ./proto/tendermint/rpc/grpc/types.pb.go ./rpc/grpc
.PHONY: proto-gen

# These targets are provided for convenience and are intended for local
# execution only.
#? proto-lint: Lint protobuf files
proto-lint: check-proto-deps
	@echo "Linting Protobuf files"
	@go run github.com/bufbuild/buf/cmd/buf@latest lint
.PHONY: proto-lint

#? proto-format: Format protobuf files
proto-format: check-proto-format-deps
	@echo "Formatting Protobuf files"
	@find . -name '*.proto' -path "./proto/*" -exec clang-format -i {} \;
.PHONY: proto-format

#? proto-check-breaking: Check for breaking changes in Protobuf files against local branch. This is only useful if your changes have not yet been committed
proto-check-breaking: check-proto-deps
	@echo "Checking for breaking changes in Protobuf files against local branch"
	@echo "Note: This is only useful if your changes have not yet been committed."
	@echo "      Otherwise read up on buf's \"breaking\" command usage:"
	@echo "      https://docs.buf.build/breaking/usage"
	@go run github.com/bufbuild/buf/cmd/buf@latest breaking --against ".git"
.PHONY: proto-check-breaking

proto-check-breaking-ci:
	@go run github.com/bufbuild/buf/cmd/buf@latest breaking --against $(HTTPS_GIT)#branch=v0.34.x
.PHONY: proto-check-breaking-ci

###############################################################################
###                              Build ABCI                                 ###
###############################################################################

#? build_abci: Build abci
build_abci:
	@go build -mod=readonly -i ./abci/cmd/...
.PHONY: build_abci

#? install_abci: Install abci
install_abci:
	@go install -mod=readonly ./abci/cmd/...
.PHONY: install_abci

###############################################################################
###                              Distribution                               ###
###############################################################################

# dist builds binaries for all platforms and packages them for distribution
# TODO add abci to these scripts
#? dist: Build binaries for all platforms and package them for distribution
dist:
	@BUILD_TAGS=$(BUILD_TAGS) sh -c "'$(CURDIR)/scripts/dist.sh'"
.PHONY: dist

#? go-mod-cache: Download go modules to local cache
go-mod-cache: go.sum
	@echo "--> Download go modules to local cache"
	@go mod download
.PHONY: go-mod-cache

#? go.sum: Ensure dependencies have not been modified
go.sum: go.mod
	@echo "--> Ensure dependencies have not been modified"
	@go mod verify
	@go mod tidy

#? draw_deps: Generate deps graph
draw_deps:
	@# requires brew install graphviz or apt-get install graphviz
	go get github.com/RobotsAndPencils/goviz
	@goviz -i github.com/cometbft/cometbft/cmd/cometbft -d 3 | dot -Tpng -o dependency-graph.png
.PHONY: draw_deps

get_deps_bin_size:
	@# Copy of build recipe with additional flags to perform binary size analysis
	$(eval $(shell go build -work -a $(BUILD_FLAGS) -tags $(BUILD_TAGS) -o $(OUTPUT) ./cmd/cometbft/ 2>&1))
	@find $(WORK) -type f -name "*.a" | xargs -I{} du -hxs "{}" | sort -rh | sed -e s:${WORK}/::g > deps_bin_size.log
	@echo "Results can be found here: $(CURDIR)/deps_bin_size.log"
.PHONY: get_deps_bin_size

###############################################################################
###                                  Libs                                   ###
###############################################################################

#? gen_certs: Generate certificates for TLS testing in remotedb and RPC server
gen_certs: clean_certs
	certstrap init --common-name "cometbft.com" --passphrase ""
	certstrap request-cert --common-name "server" -ip "127.0.0.1" --passphrase ""
	certstrap sign "server" --CA "cometbft.com" --passphrase ""
	mv out/server.crt rpc/jsonrpc/server/test.crt
	mv out/server.key rpc/jsonrpc/server/test.key
	rm -rf out
.PHONY: gen_certs

#? clean_certs: Delete generated certificates
clean_certs:
	rm -f rpc/jsonrpc/server/test.crt
	rm -f rpc/jsonrpc/server/test.key
.PHONY: clean_certs

###############################################################################
###                  Formatting, linting, and vetting                       ###
###############################################################################

format:
	find . -name '*.go' -type f -not -path "*.git*" -not -name '*.pb.go' -not -name '*pb_test.go' | xargs gofmt -w -s
	find . -name '*.go' -type f -not -path "*.git*"  -not -name '*.pb.go' -not -name '*pb_test.go' | xargs goimports -w -local github.com/cometbft/cometbft
.PHONY: format

#? check-go-env: Show the Go binary used for local lint tooling
check-go-env:
	@echo "--> Using Go binary: $(GO)"
	@$(GO_LOCAL_ENV) $(GO) version
	@echo "--> Ignoring external GOROOT for repository commands"
.PHONY: check-go-env

#? install-lint: Install the local golangci-lint binary used by make lint
install-lint:
	@$(GO_LOCAL_ENV) GOBIN=$(GO_BIN) $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
.PHONY: install-lint

#? check-lint: Verify the local golangci-lint binary used by make lint
check-lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not found at $(GOLANGCI_LINT)"; \
		echo "Run 'make install-lint' first."; \
		exit 1; \
	fi
	@echo "--> Using golangci-lint binary: $(GOLANGCI_LINT)"
	@$(GOLANGCI_LINT) version
.PHONY: check-lint

#? lint: Run the local golangci-lint binary
lint: check-go-env check-lint
	@echo "--> Running linter"
	@$(GOLANGCI_LINT) run --timeout 10m -v
.PHONY: lint

#? lint-changed: Run golangci-lint on local changed Go files
lint-changed: check-go-env check-lint
	@changed_files="$$( { git diff --name-only --diff-filter=ACMR HEAD; git ls-files --others --exclude-standard; } | grep '\.go$$' | sort -u || true )"; \
	if { git diff --name-only --diff-filter=ACMR HEAD; git ls-files --others --exclude-standard; } | grep -Eq '(^|/)(go\.mod|go\.sum)$$'; then \
		echo "--> go.mod/go.sum changed; running full golangci-lint..."; \
		$(GOLANGCI_LINT) run --timeout 10m -v; \
	elif [ -z "$$changed_files" ]; then \
		echo "--> No local changed Go files to lint"; \
	else \
		changed_dirs="$$(printf '%s\n' "$$changed_files" | xargs -n1 dirname | sed 's#^\.$$#./.#' | sed 's#^[^./]#./&#' | sort -u)"; \
		echo "--> Running golangci-lint on local changed Go packages..."; \
		$(GOLANGCI_LINT) run --timeout 10m -v $$changed_dirs; \
	fi
.PHONY: lint-changed

#? lint-staged: Run golangci-lint on staged Go files
lint-staged: check-go-env check-lint
	@staged_files="$$(git diff --cached --name-only --diff-filter=ACMR | grep '\.go$$' || true)"; \
	if git diff --cached --name-only --diff-filter=ACMR | grep -Eq '(^|/)(go\.mod|go\.sum)$$'; then \
		echo "--> go.mod/go.sum changed; running full golangci-lint..."; \
		$(GOLANGCI_LINT) run --timeout 10m -v; \
	elif [ -z "$$staged_files" ]; then \
		echo "--> No staged Go files to lint"; \
	else \
		staged_dirs="$$(printf '%s\n' "$$staged_files" | xargs -n1 dirname | sed 's#^\.$$#./.#' | sed 's#^[^./]#./&#' | sort -u)"; \
		echo "--> Running golangci-lint on staged Go packages..."; \
		$(GOLANGCI_LINT) run --timeout 10m -v $$staged_dirs; \
	fi
.PHONY: lint-staged

#? pre-commit: Run local checks that are safe to execute before commit
pre-commit: lint-changed
.PHONY: pre-commit

#? pre-commit-staged: Run staged checks used by git hook
pre-commit-staged: lint-staged
.PHONY: pre-commit-staged

# https://github.com/cometbft/cometbft/pull/1925#issuecomment-1875127862
# Revisit using lint-format after CometBFT v1 release and/or after 2024-06-01.
#lint-format:
#	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run --fix
#	@go run mvdan.cc/gofumpt -l -w ./..
#.PHONY: lint-format

#? vulncheck: Run latest govulncheck
vulncheck:
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...
.PHONY: vulncheck

#? lint-typo: Run codespell to check typos
lint-typo:
	which codespell || pip3 install codespell
	@codespell
.PHONY: lint-typo

#? lint-typo: Run codespell to auto fix typos
lint-fix-typo:
	@codespell -w
.PHONY: lint-fix-typo

DESTINATION = ./index.html.md


###############################################################################
###                           Documentation                                 ###
###############################################################################

#? check-docs-toc: Verify that important design docs have ToC entries.
check-docs-toc:
	@./docs/presubmit.sh
.PHONY: check-docs-toc

###############################################################################
###                            Docker image                                 ###
###############################################################################

# On Linux, you may need to run `DOCKER_BUILDKIT=1 make build-docker` for this
# to work.
#? build-docker: Build docker image cometbft/cometbft
build-docker:
	docker build \
		--label=cometbft \
		--tag="cometbft/cometbft" \
		-f DOCKER/Dockerfile .
.PHONY: build-docker

###############################################################################
###                       Local testnet using docker                        ###
###############################################################################

#? build-linux: Build linux binary on other platforms
build-linux:
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) $(MAKE) build
.PHONY: build-linux

#? build-docker-localnode: Build the "localnode" docker image
build-docker-localnode:
	@cd networks/local && make
.PHONY: build-docker-localnode

# Runs `make build COMETBFT_BUILD_OPTIONS=cleveldb` from within an Amazon
# Linux (v2)-based Docker build container in order to build an Amazon
# Linux-compatible binary. Produces a compatible binary at ./build/cometbft
build_c-amazonlinux:
	$(MAKE) -C ./DOCKER build_amazonlinux_buildimage
	docker run --rm -it -v `pwd`:/cometbft cometbft/cometbft:build_c-amazonlinux
.PHONY: build_c-amazonlinux

#? localnet-start: Run a 4-node testnet locally
localnet-start: localnet-stop build-docker-localnode
	@if ! [ -f build/node0/config/genesis.json ]; then docker run --rm -v $(CURDIR)/build:/cometbft:Z cometbft/localnode testnet --config /etc/cometbft/config-template.toml --o . --starting-ip-address 192.167.10.2; fi
	docker compose up -d
.PHONY: localnet-start

#? localnet-stop: Stop testnet
localnet-stop:
	docker compose down
.PHONY: localnet-stop

#? build-contract-tests-hooks: Build hooks for dredd, to skip or add information on some steps
build-contract-tests-hooks:
ifeq ($(OS),Windows_NT)
	go build -mod=readonly $(BUILD_FLAGS) -o build/contract_tests.exe ./cmd/contract_tests
else
	go build -mod=readonly $(BUILD_FLAGS) -o build/contract_tests ./cmd/contract_tests
endif
.PHONY: build-contract-tests-hooks

#? contract-tests: Run a nodejs tool to test endpoints against a localnet
# The command takes care of starting and stopping the network
# prerequisits: build-contract-tests-hooks build-linux
# the two build commands were not added to let this command run from generic containers or machines.
# The binaries should be built beforehand
contract-tests:
	dredd
.PHONY: contract-tests

# Implements test splitting and running. This is pulled directly from
# the github action workflows for better local reproducibility.

GO_TEST_FILES != find $(CURDIR) -name "*_test.go"

# default to four splits by default
NUM_SPLIT ?= 4

$(BUILDDIR):
	mkdir -p $@

# The format statement filters out all packages that don't have tests.
# Note we need to check for both in-package tests (.TestGoFiles) and
# out-of-package tests (.XTestGoFiles).
$(BUILDDIR)/packages.txt:$(GO_TEST_FILES) $(BUILDDIR)
	go list -f "{{ if (or .TestGoFiles .XTestGoFiles) }}{{ .ImportPath }}{{ end }}" ./... | sort > $@

split-test-packages:$(BUILDDIR)/packages.txt
	split -d -n l/$(NUM_SPLIT) $< $<.
test-group-%:split-test-packages
	cat $(BUILDDIR)/packages.txt.$* | xargs go test -mod=readonly -timeout=15m -race -coverprofile=$(BUILDDIR)/$*.profile.out

#? help: Get more info on make commands.
help: Makefile
	@echo " Choose a command run in comebft:"
	@sed -n 's/^#?//p' $< | column -t -s ':' |  sort | sed -e 's/^/ /'
.PHONY: help
