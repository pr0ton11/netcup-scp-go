# A Go Client for the netcup Server Control Panel

This project provides a generated Go client for the [netcup Server Control Panel (SCP)](https://www.servercontrolpanel.de) REST API.

## Features

- Generated client based on the [OpenAPI Spec of the netcup SCP](https://www.servercontrolpanel.de/scp-core/api/v1/openapi)
- Automated daily sync via GitHub Actions -- a PR is opened automatically when the upstream spec changes
- Uses [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) for idiomatic Go code generation

## Installation

```bash
go get github.com/pr0ton11/netcup-scp-go
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/pr0ton11/netcup-scp-go/scp"
)

func main() {
	// Create a client pointing to the netcup SCP API
	client, err := scp.NewClientWithResponses("https://www.servercontrolpanel.de/scp-core")
	if err != nil {
		log.Fatal(err)
	}

	// Example: Ping the API
	resp, err := client.GetApiPingWithResponse(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Status: %d\n", resp.StatusCode())
}
```

### Authentication

The netcup SCP API uses OpenID Connect for authentication. You can provide credentials using a custom `RequestEditorFn`:

```go
bearerAuth := func(ctx context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

client, err := scp.NewClientWithResponses(
	"https://www.servercontrolpanel.de/scp-core",
	scp.WithRequestEditorFn(bearerAuth),
)
```

## Development

### Prerequisites

- Go (version from `go.mod`)
- Python 3 (for `scripts/patch-spec.py`)

oapi-codegen runs via `go run` at the version pinned in `go.mod`, so it needs no separate install.

### Regenerating the client

To fetch the latest upstream OpenAPI spec and regenerate the Go client:

```bash
# Fetch spec + patch + generate
make generate

# Build, vet and test
make test
```

### Spec patches

`scripts/patch-spec.py` adjusts the upstream spec before generation:

- drops the duplicate `application/hal+json` responses and maps `application/merge-patch+json` request bodies to `application/json`;
- gives the custom method paths (`…/firewall:reapply`, `…/snapshots:dryrun`, …) an `operationId`. The paths themselves are kept, since they are the URLs the server serves;
- converts the OpenAPI 3.1 constructs upstream uses (`"type": ["string", "null"]`, `anyOf` with `null`, `examples`) to 3.0, because oapi-codegen does not support 3.1. The script fails on any other 3.1 keyword, so a new one shows up as a failed run instead of a wrong client.

Or run the full pipeline including tool installation:

```bash
make all
```

### Project structure

```
.
├── .github/workflows/generate.yml  # Automated spec sync & code generation
├── api/openapi.json                # Local copy of the netcup SCP OpenAPI spec
├── scp/client.gen.go               # Generated Go client (types + HTTP client)
├── generate.go                     # go:generate directive
├── tools.go                        # Tool dependency (oapi-codegen)
├── oapi-codegen.yaml               # Code generator configuration
├── Makefile                        # Development commands
└── README.md
```

### How automation works

A GitHub Action runs daily (and can be triggered manually) to:

1. Fetch the latest OpenAPI spec from the [netcup SCP](https://www.servercontrolpanel.de/scp-core/api/v1/openapi)
2. Patch any known upstream spec issues (e.g. HAL+JSON content types, colon-style paths)
3. Regenerate the Go client code
4. If changes are detected, open a Pull Request for review

## License

See [LICENSE](LICENSE) for details.
