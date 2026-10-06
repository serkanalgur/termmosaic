# Contributing

TermMosaic v1.0.0 was released on 2026-10-06 with a stability promise: the
public API is frozen at that tag and Semantic Versioning applies. Contributions
are welcome — [STATUS.md](STATUS.md) records what is decided, open and deferred.

## Before you start

If you want to change architecture, open an issue first. The backend strategy,
buffer representation, renderer mode and layout engine are decided and recorded
in [docs/adr/](adr/README.md); reversing one is a release-defining act, not a
pull request. A PR that quietly commits to a new architectural direction will
not be reviewed.

## Getting set up

```bash
git clone git@github.com:serkanalgur/termmosaic.git
cd termmosaic
go build ./...
go test ./...
```

Go 1.23 or newer is required.

## Running examples

```bash
go run ./examples/...
```

## Working agreement

- **From v1.0.0, the API is frozen and SemVer applies.** We use conventional
  commits and keep a changelog. Changing the frozen surface is a breaking
  change and needs an issue first; behaviour fixes go in patch releases.
- Run `go vet ./...` and `gofmt -l .` before opening a PR.
- Every widget needs: a test, a runnable example, and a documented public API.
  A widget without an example is not done.
- Renderer and widget code must be testable without a real terminal. Use the
  headless backend in tests.
- Performance-sensitive changes (the buffer, the diff, the layout engine) need
  a benchmark.

## Commit messages

Conventional Commits:

```
feat(buffer): add SoA cell storage
fix(diff): skip row compare when width matches
docs(status): mark backend strategy as decided
```

## Reporting bugs

Include your terminal emulator, OS, shell, and whether you are inside tmux or
an SSH session. Terminal-specific bugs are usually environment-specific, and
this information is usually the answer.