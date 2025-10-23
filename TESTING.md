# Testing Guide

This document describes how to run tests for the Aerospike Kubernetes Operator.

## Quick Reference

| Command                           | Description                              |
|-----------------------------------|------------------------------------------|
| `make pkg-test`                   | Run pkg unit tests (CI command)          |
| `make pkg-test-coverage`          | Run tests + open coverage report         |
| `make cluster-test`               | Run cluster integration tests            |
| `make backup-service-test`        | Run backup-service integration tests     |
| `make backup-test`                | Run backup integration tests             |
| `make restore-test`               | Run restore integration tests            |
| `make all-test`                   | Run all tests                            |
| `go test ./pkg/...`               | Run all pkg tests manually               |
| `go test -v -race ./pkg/utils`    | Run specific package with race detection |
| `go test -run TestName ./pkg/...` | Run specific test                        |

## Quick Start

### Run PKG Unit Tests (Recommended)

```bash
# Run pkg unit tests (same command used in CI)
make pkg-test

# Run pkg unit tests and open coverage report in browser
make pkg-test-coverage
```

## Available Test Targets

### Unit Tests

#### `make pkg-test`
Runs all unit tests in the `pkg/` directory with race detection and coverage reporting.

**What it does:**
- Runs `go test -v -race -coverprofile=coverage.out ./pkg/...`
- Displays coverage summary at the end
- Same command used by GitHub Actions CI

**Example output:**
```
Running pkg unit tests...
=== RUN   TestGetFailedPodGracePeriod
--- PASS: TestGetFailedPodGracePeriod (0.00s)
...
PASS
coverage: 29.8% of statements

Coverage Summary:
total: (statements) 18.6%
```

#### `make pkg-test-coverage`
Runs pkg unit tests and opens an HTML coverage report in your browser.

**Use this when:**
- You want to see detailed line-by-line coverage
- You're improving test coverage
- You need to identify untested code paths

### Integration Tests

#### `make cluster-test`
Runs cluster integration tests using Ginkgo.

#### `make backup-service--test`
Runs backup-service integration tests.

#### `make backup-test`
Runs backup integration tests.

#### `make restore-test`
Runs restore integration tests.

#### `make all-test`
Runs all tests (unit + integration).

## Running Tests Manually

### Run all pkg tests:
```bash
go test -v -race ./pkg/...
```

### Run specific package:
```bash
go test -v -race ./pkg/utils
go test -v -race ./pkg/jsonpatch
go test -v -race ./pkg/merge
```

### Run specific test:
```bash
go test -v -race ./pkg/utils -run TestGetFailedPodGracePeriod
```

### Run with coverage:
```bash
go test -v -race -coverprofile=coverage.out ./pkg/...
go tool cover -func=coverage.out
go tool cover -html=coverage.out
```

## Test Coverage

### Current Coverage

Run `make pkg-test` to see current coverage:
```
pkg/jsonpatch: 64.1% of statements
pkg/merge:     89.3% of statements
pkg/utils:     29.8% of statements
total:         18.6% of statements
```

## Additional Resources
- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [GitHub Actions Workflows](.github/workflows/README.md)

