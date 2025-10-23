# GitHub Actions Workflows

This directory contains GitHub Actions workflows for CI/CD automation.

## Available Workflows

### 1. PKG Unit Tests (`pkg-unit-tests.yaml`)
**Unit testing workflow for pkg directory**

**Triggers:**
- Push to `master` branch
- Pull requests to `master` branch
- **Only when files in `pkg/` directory change**

**Features:**
- ✅ Runs all unit tests in `pkg/` directory with race detection
- ✅ Basic coverage reporting
- ✅ Fast execution for quick feedback
- ✅ Minimal configuration

### 2. GolangCI Lint (`golangci-lint.yaml`)
**Code quality and linting checks**

**Triggers:**
- Push to `master` branch or version tags
- Pull requests to `master` branch

**Features:**
- ✅ Runs golangci-lint with comprehensive checks
- ✅ 5-minute timeout for large codebases

### 4. CodeQL Analysis (`codeql-analysis.yml`)
**Security and code quality analysis**

**Triggers:**
- Scheduled runs and code changes

**Features:**
- ✅ Security vulnerability scanning
- ✅ Code quality analysis

### 5. Docker Image Release (`docker-image-release.yaml`)
**Container image building and publishing**

**Triggers:**
- Version tag pushes

**Features:**
- ✅ Multi-architecture Docker image builds
- ✅ Image publishing to container registry


