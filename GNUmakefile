SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
VERSION ?= dev
PKG ?= ./internal/...
RUN ?= TestAcc
TARGET ?= dev
export ZEROPS_TARGET := $(TARGET)

.PHONY: build zguard test testacc sweep smoke generate docs-check fmt lint release-local
build:
	if [ -f .goenv ]; then source .goenv; fi; go build -ldflags "-X main.version=$(VERSION)" -o bin/terraform-provider-zerops .
zguard:
	if [ -f .goenv ]; then source .goenv; fi; go build -o bin/zguard ./tools/zguard
test:
	if [ -f .goenv ]; then source .goenv; fi; TF_ACC= go test ./...
testacc: zguard
	if [ -f .goenv ]; then source .goenv; fi; source tools/target-guard.sh; zerops_target_precheck; zerops_production_run_lock; for cli in /usr/bin/terraform /usr/bin/tofu; do test -x "$$cli"; TF_ACC=1 TF_ACC_TERRAFORM_PATH="$$cli" go test -p 1 -parallel 1 $(PKG) -run '$(RUN)' -timeout 240m; done
sweep: zguard
	if [ -f .goenv ]; then source .goenv; fi; source tools/target-guard.sh; zerops_target_precheck; zerops_production_run_lock; go test ./internal/sweep -sweep="$(TARGET)" -sweep-run=zerops_project -timeout 120m
smoke: build zguard
	bash tools/smoke.sh
generate:
	if [ -f .goenv ]; then source .goenv; fi; mkdir -p .generated-docs; if [ -x "$${GOBIN:-}/tfplugindocs" ]; then generator=("$$GOBIN/tfplugindocs"); else generator=(go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0); fi; "$${generator[@]}" generate --provider-name zerops --rendered-provider-name Zerops --provider-dir . --rendered-website-dir .generated-docs; for part in index.md resources data-sources; do if test -e ".generated-docs/$$part"; then cp -R ".generated-docs/$$part" docs/; fi; done
docs-check: generate
	git diff --exit-code -- docs; test -z "$$(git ls-files --others --exclude-standard -- docs)"
fmt:
	if [ -f .goenv ]; then source .goenv; fi; go fmt ./...; terraform fmt -recursive examples
lint:
	if [ -f .goenv ]; then source .goenv; fi; go vet ./...; terraform fmt -check -recursive examples
release-local:
	if [ -f .goenv ]; then source .goenv; fi; command -v goreleaser >/dev/null || { echo 'goreleaser is required' >&2; exit 1; }; goreleaser release --snapshot --clean --skip=publish
