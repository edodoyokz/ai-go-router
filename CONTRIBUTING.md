# Contributing Guide

This guide explains how to contribute to 9router-go.

---

## Table of Contents

- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Testing](#testing)
- [Commit Guidelines](#commit-guidelines)
- [Pull Request Process](#pull-request-process)

---

## Getting Started

### Prerequisites

- Go 1.24.0 or later
- Git
- Make (optional, for build commands)

### Fork and Clone

```bash
# Fork the repository on GitHub
# Clone your fork
git clone https://github.com/your-username/ai-go-router.git
cd ai-go-router

# Add upstream remote
git remote add upstream https://github.com/edodoyokz/ai-go-router.git
```

### Install Dependencies

```bash
go mod download
```

### Build

```bash
make build
# or
go build -o bin/router ./cmd/router
```

### Run Tests

```bash
make test
# or
go test ./...
```

---

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
```

Branch naming conventions:
- `feature/feature-name` - New features
- `fix/bug-description` - Bug fixes
- `docs/documentation-update` - Documentation changes
- `refactor/refactor-description` - Code refactoring

### 2. Make Changes

Edit code following the project's code style (see below).

### 3. Test

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for specific package
go test ./internal/router
```

### 4. Build

```bash
go build ./...
```

### 5. Commit

Follow commit guidelines (see below).

### 6. Push

```bash
git push origin feature/your-feature-name
```

### 7. Create Pull Request

- Go to GitHub and create a pull request
- Fill out the PR template
- Link related issues

---

## Code Style

### Go Conventions

Follow standard Go conventions as described in [Effective Go](https://golang.org/doc/effective_go).

### Formatting

Use `gofmt`:

```bash
gofmt -w .
```

Or use `goimports` (recommended):

```bash
go install golang.org/x/tools/cmd/goimports@latest
goimports -w .
```

### Linting

Use `golangci-lint`:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run
```

### Project-Specific Guidelines

#### Package Organization

- Keep packages focused and small
- Follow the existing structure under `internal/`
- Use descriptive package names

#### Error Handling

- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use error types from `internal/providers/errors.go`
- Distinguish retryable vs non-retryable errors

#### Logging

- Use zerolog consistently
- Log at appropriate levels (Debug, Info, Warn, Error)
- Include request_id in logs when available
- Use structured logging with fields

#### Configuration

- Prefer config-driven behavior over hardcoded logic
- Add YAML fields for new behavior instead of code changes
- Update example config when adding new fields

#### Interfaces

- Keep provider interface minimal
- Don't add methods until multiple providers need them
- Use composition over inheritance

---

## Testing

### Unit Tests

Write unit tests for new functionality:

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {
            name:    "valid input",
            input:   "test",
            want:    "result",
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := MyFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("MyFunction() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("MyFunction() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Integration Tests

For integration tests, use the existing pattern in `internal/router/integration_test.go`.

### Test Coverage

Aim for reasonable test coverage. Use:

```bash
go test -cover ./...
```

---

## Commit Guidelines

### Commit Message Format

Follow conventional commits:

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Examples

```
feat(api): add streaming support for OpenAI provider

- Implement SSE scanner helper
- Add StreamChatCompletion method to OpenAIAdapter
- Update handleStreamingChatCompletion in server.go

Closes #123
```

```
fix(router): correct round-robin index handling

The index was not being incremented correctly when
switching between routes.

Fixes #456
```

```
docs: update API reference with new endpoints

Added documentation for:
- GET /api/logs
- GET /api/providers/{name}/accounts/{account}/health
```

---

## Pull Request Process

### Before Submitting

1. **Code Review**
   - Self-review your changes
   - Ensure code follows project style
   - Check for TODOs or FIXMEs

2. **Tests**
   - All tests pass
   - Add tests for new functionality
   - Ensure no regressions

3. **Documentation**
   - Update relevant documentation
   - Add comments for complex code
   - Update CHANGELOG.md for user-facing changes

### PR Template

```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Checklist
- [ ] Code follows project style
- [ ] Tests pass locally
- [ ] Documentation updated
- [ ] CHANGELOG.md updated (if user-facing)
```

### Review Process

1. Automated checks (CI) must pass
2. At least one maintainer approval required
3. Address review feedback
4. Squash commits if requested
5. Maintainer merges

---

## Adding a New Provider

To add a new AI provider:

1. **Create Adapter**
   - Create `internal/providers/myprovider.go`
   - Implement `Adapter` interface from `internal/providers/providers.go`
   - Add error classification

2. **Register Provider**
   - Add provider type to validation in `internal/config/config.go`
   - Register in `internal/app/app.go`

3. **Add Tests**
   - Create `internal/providers/myprovider_test.go`
   - Write unit tests for adapter methods

4. **Update Documentation**
   - Add to provider guide: `docs/provider-guide.md`
   - Update example config: `config/config.example.yaml`
   - Update CHANGELOG.md

5. **Submit PR**
   - Include example configuration
   - Include test results
   - Reference provider documentation

### Adapter Interface

```go
type Adapter interface {
    Name() string
    ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error)
    StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error)
    ClassifyError(err error) ErrorClassification
    FetchUsage(ctx context.Context, model string) (*Usage, error)
}
```

---

## Adding a New Endpoint

To add a new API endpoint:

1. **Add Handler**
   - Add handler method to `internal/api/server.go`
   - Follow existing pattern (check auth, validate, respond)

2. **Register Route**
   - Add route in `Handler()` method
   - Use appropriate middleware (auth, logging)

3. **Add Tests**
   - Add test in `internal/api/server_test.go`
   - Test success and error cases

4. **Update Documentation**
   - Add to API reference: `docs/api-reference.md`
   - Update CHANGELOG.md

---

## Reporting Issues

When reporting issues:

1. **Search existing issues** first
2. **Use issue template** if available
3. **Include**:
   - Go version
   - OS and architecture
   - Config file (redact secrets)
   - Error messages
   - Steps to reproduce
   - Logs (redact secrets)

---

## Questions?

- Check documentation in `docs/`
- Review existing code for patterns
- Ask in GitHub Discussions
- Open an issue for bugs or feature requests

---

## License

By contributing, you agree that your contributions will be licensed under the same license as the project (see LICENSE file).
