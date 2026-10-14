.PHONY: generate install-tools patch-spec fetch-spec test clean all

OPENAPI_SPEC_URL := https://www.servercontrolpanel.de/scp-core/api/v1/openapi
SPEC_FILE := api/openapi.json

# oapi-codegen runs through `go run` at the version pinned in go.mod (see generate.go),
# so there is nothing to install. Kept for compatibility.
install-tools:
	go mod download

# Fetch the latest OpenAPI spec from upstream
fetch-spec:
	curl -fsSL "$(OPENAPI_SPEC_URL)" -o $(SPEC_FILE)

# Patch known issues in the upstream spec for oapi-codegen compatibility
patch-spec:
	python3 scripts/patch-spec.py $(SPEC_FILE)

# Generate Go client from OpenAPI spec
generate: fetch-spec patch-spec
	go generate ./...
	go mod tidy

# Build, vet and test the client
test:
	go build ./...
	go vet ./...
	go test ./...

# Remove generated files
clean:
	rm -f scp/client.gen.go

# Full pipeline: install tools, fetch spec, generate
all: install-tools generate
