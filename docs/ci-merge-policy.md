# CI merge policy

GitHub Actions runs on every pull request and every push to `main`.

Required status check for protected branches:

```text
Unit tests
```

The check must pass before merging. The workflow currently runs:

```bash
go test ./...
go vet ./...
```

Repository admins should configure the `main` branch protection rule in GitHub:

1. Require a pull request before merging.
2. Require status checks to pass before merging.
3. Select the required check named `Unit tests`.
4. Require branches to be up to date before merging.
5. Block force pushes.
