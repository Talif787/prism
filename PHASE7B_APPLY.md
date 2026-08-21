# Phase 7b apply guide (Terraform, CI/CD, load and chaos)

Adds Terraform (`deploy/terraform`), GitHub Actions (`.github/workflows`), k6 load
tests (`test/load`), and Chaos Mesh experiments (`test/chaos`). No Go code, so no
build, no `go mod tidy`, no module-path rename.

## 1. Unzip over the repo root
```bash
cd ~/prism
unzip -o /path/to/prism-phase7b.zip -d .
```
New: `deploy/terraform/**`, `.github/workflows/{ci,release}.yml`, `test/load/**`,
`test/chaos/**`, `docs/TESTING.md`. Changed: `docs/DEPLOYMENT.md`, `README.md`.

## 2. Validate Terraform (the gate for this phase)
```bash
cd deploy/terraform
terraform fmt -check -recursive
terraform init -backend=false
terraform validate
cd ../..
```
`fmt`, `init`, and `validate` are the real correctness gate, the way `go build` is for
the services and `helm lint` is for the chart. Applying is optional and costs money;
see `deploy/terraform/README.md`.

## 3. CI/CD
The workflows activate once pushed to GitHub. `ci.yml` runs on pull requests and
pushes to main (Go build and test, gofmt, vet, helm lint, terraform validate, a
Docker build matrix, and the no-dash conventions check). `release.yml` runs on a
`v*` tag and pushes images to GHCR. No secrets are required beyond the built-in
`GITHUB_TOKEN`; ensure GHCR package publishing is allowed for the repo.

## 4. Load and chaos (need a running cluster)
See `docs/TESTING.md`, `test/load/README.md`, and `test/chaos/README.md`. Run these
against the kind or GKE stack from Phase 7a with an API key.

## 5. Commit
This completes the backend. A `v0.9.0` tag suits Phase 7b, or a `v1.0.0` to mark the
backend complete end to end. Your call.
