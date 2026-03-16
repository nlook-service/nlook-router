# Contributing to nlook-router

Thank you for your interest in contributing!

## Development Setup

```bash
git clone https://github.com/nlook-service/nlook-router.git
cd nlook-router
go mod download
go build -o nlook-router ./cmd/nlook-router
```

## Running Tests

```bash
go test ./...
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Wrap errors with context: `fmt.Errorf("action: %w", err)`
- Pass `context.Context` to all I/O functions
- Never log credentials (API keys, passwords, private keys)

## Pull Requests

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Commit with descriptive messages
4. Push and open a PR

## Reporting Issues

Open an issue at https://github.com/nlook-service/nlook-router/issues
