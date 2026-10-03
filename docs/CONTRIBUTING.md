# Contributing

TermMosaic is pre-alpha and in active design. Contributions are welcome, but
the design is not settled — see [STATUS.md](STATUS.md).

## Before you start

If you want to change architecture, open an issue first. Right now the backend
strategy, buffer representation, renderer mode, and layout engine are all open
questions. A PR that quietly commits to one of them will not be reviewed.

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

- **Pre-1.0, the API will break.** We will use conventional commits and keep a
  changelog, but do not assume stability.
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