# TermMosaic

A terminal UI framework for Go: a cell-buffer renderer plus a catalog of
ready-to-use widgets.

> **Status: pre-alpha.** This is an active design effort. Nothing below is a
> promise yet — see [docs/STATUS.md](docs/STATUS.md) for what is decided,
> what is proposed, and what is undecided.

## Why this exists

The Go TUI ecosystem has exactly one dominant framework, and it leaves a lot
on the table:

- **Ink** (TS/React) clears and repaints the whole screen on each update. It
  works until your app gets big, then input lag becomes obvious.
- **OpenTUI** is a strong engineering artifact with a deliberately unfinished
  surface: at 13.4k stars and running in production, it ships **no
  progressbar, gauge, sparkline, bar chart, or meter**, and **no list, tree,
  pager, or virtual scroll**. Its roadmap still lists data virtualization as
  unfinished.
- Terminal.Gui and friends predate the modern bar and carry decades of
  accumulated baggage.

TermMosaic's bet is narrow and specific: **the widget catalog is the product.**
A framework nobody can build a real dashboard on is a toy, regardless of how
elegant its renderer is.

## Design pillars

1. **A complete widget catalog.** Thirty-plus widgets covering layout, forms,
   data, and visualization — the things real tools need.
2. **Data virtualization from day one.** List, Table, and Tree render 100k+
   rows at interactive speed. Not a future roadmap item.
3. **Honest rendering.** A double-buffered cell buffer with two-tier diffing
   (row-level skip, then per-cell), dirty-region tracking, and a 30–60 fps
   frame budget. No full-screen clears in the hot path.
4. **Graceful degradation.** Truecolor → 256 → 16 color. Unicode borders →
   ASCII. `NO_COLOR` respected. Works over SSH and inside tmux.
5. **Testable without a terminal.** A headless memory-sink backend means the
   renderer and every widget are unit-testable in CI.
6. **Accessible by construction.** Color is never the only signal. Visible
   focus. Full keyboard operation. Reduced-motion flag.
7. **Documented limits.** Known limitations are written down, not discovered
   by users. If we have no IME support, the README says so.

## Installation

Not yet available. See [docs/STATUS.md](docs/STATUS.md).

## Documentation

- [docs/STATUS.md](docs/STATUS.md) — current state of the design
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — architecture decisions
- [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) — how to help

## License

MIT

## Acknowledgements

This project studies and is informed by the work of
[Ratatui](https://github.com/ratatui/ratatui),
[Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Textual](https://github.com/Textualize/textual), and
[OpenTUI](https://github.com/anomalyco/opentui).