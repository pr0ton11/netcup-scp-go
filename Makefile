.PHONY: generate install-tools patch-spec fetch-spec clean all

OPENAPI_SPEC_URL := https://www.servercontrolpanel.de/scp-core/api/v1/openapi
SPEC_FILE := api/openapi.json

# Install required tools
install-tools:
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# Fetch the latest OpenAPI spec from upstream
fetch-spec:
	curl -sL "$(OPENAPI_SPEC_URL)" -o $(SPEC_FILE)

# Patch known issues in the upstream spec for oapi-codegen compatibility
patch-spec:
	python3 scripts/patch-spec.py $(SPEC_FILE)

# Generate Go client from OpenAPI spec
generate: fetch-spec patch-spec
	go generate ./...
	go mod tidy

# Remove generated files
clean:
	rm -f scp/client.gen.go

# Full pipeline: install tools, fetch spec, generate
all: install-tools generate
