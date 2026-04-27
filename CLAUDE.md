# Kuperator - Claude Code Project Guide

## Development Requirements

- Go 1.24+
- Kubernetes 1.22+ (envtest uses 1.22.1)
- Docker (for container builds)
- Kind (for e2e testing)

## Pre-commit Checklist

**IMPORTANT**: Before committing and pushing code, always run:

```bash
make build lint fmt vet
```

These commands ensure:
- `make build`: Compiles the manager binary (includes manifests, fmt, vet)
- `make lint`: Runs golangci-lint for code quality
- `make fmt`: Formats code with `go fmt`
- `make vet`: Runs `go vet` for static analysis

## Key Dependencies

- `kusionstack.io/kube-api`: CRD API definitions
- `kusionstack.io/kube-utils`: Utility packages (cert, certmanager, controller helpers)
- `kusionstack.io/kube-xset`: XSet workload framework
- `sigs.k8s.io/controller-runtime`: Kubernetes controller framework

## Related Documentation

- [Contribution Guide](docs/contributing.md)
- [Design Plans](docs/plans/)
- [Official Documentation](https://kusionstack.io/kuperator/introduction/)