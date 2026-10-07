# Roadmap

TermMosaic is at v1.0.0, released 2026-10-06, tag `v1.0.0` at commit `53fa354`
(docs/STATUS.md:6-7, CHANGELOG.md). This file extracts decisions already
recorded in `docs/STATUS.md`, `docs/adr/*.md` and `CHANGELOG.md`; it creates no
new commitments. Standing rule from `docs/STATUS.md`: the release policy
promises no behavioural change in a patch release, so **any behavioural defect
found after v1.0.0 is a v1.1.0** (docs/STATUS.md:1135-1137).

## v1.1 — committed scope

- **`Registry.SetFocus`** — deferred to v1.1; new API on a shipped type, so a
  minor. `Describe(ScopeFocus)` is over-inclusive rather than incomplete, so the
  fix is a method whose answer must be filtered by availability
  (docs/STATUS.md:1105-1118, CHANGELOG.md:275, docs/adr/0009-command-and-keymap.md:1476).
- **A `Ctrl+K` command palette** — ADR 0009 §9 puts it in scope but explicitly
  not in that ADR; new behaviour in `Menu`/`Dialog`, so a v1.1.0
  (docs/STATUS.md:1091-1095, CHANGELOG.md:414).
- **A Windows console backend** — v1.1+, additive, interfaces unchanged
  (docs/STATUS.md:1079).
- **Making a catalog widget implement `keymap.Commandable`** — ADR 0009's own
  deferral. The trigger is the first catalog widget whose keys a program would
  rather name than switch on (docs/STATUS.md:1096-1104).
- **`examples/markets` and `examples/dashboard` moving off their own `switch`
  onto `keymap`** — two of four examples have done this; what is missing is the
  overlap, nothing in the tree has a `keymap` binding and a widget `switch`
  answering the same key (docs/STATUS.md:1119-1124).

## Deferred — decided against for v1.0, unversioned

Absent from v1.0.0 is a decision on record, not an oversight
(docs/STATUS.md:1074-1077).

- **A `Form` container** — ADR 0004's solver plus `layout` already covers
  composition; a `Form` type would be a second way to do the same thing
  (docs/STATUS.md:1080-1081).
- **IME / preedit** — ADR 0005's deliberate deferral, three named seams left
  open (`EventCompose`, the `*Compose` field, the parser entry point); CJK
  composition gets wrong behaviour, not degraded behaviour
  (docs/STATUS.md:1082-1085, docs/adr/0005-input-decoding.md:495-547).
- **Grapheme-cluster composition** — performance is measured and good; only the
  decision is open, and the failure is cosmetic (docs/STATUS.md:1089-1090).
- **Kitty graphics protocol** — currently an OPEN row; still needs a formal
  "no for v1" decision, one sentence in an ADR (docs/STATUS.md:1086-1088).
- **A theme system** — ADR 0008 decides "no theme in v1", with a trigger: the
  first role two widgets must share (docs/STATUS.md:1127-1128,
  docs/adr/0008-style-and-text.md:225).
- **TextArea rendered selection, table column selection, pager selection, a redo
  stack** — additive widget API, all four fine as v1.1.0
  (docs/STATUS.md:1125-1126).
- **A wider lint checklist** — real, self-declared, and explicitly not a
  release gate (docs/STATUS.md:1129).

## Risk register — latent v1.1.0 triggers

Because a defect fix is a minor, the risk register is a list of things likely
to be found late (docs/STATUS.md:1133-1138).

1. **Latent instances of the style-application class** — highest probability by
   a wide margin; per-widget divergence between the style a painter computes and
   the writer that carries the text, applying to every widget with a row painter
   (docs/STATUS.md:1140-1144).
2. **`keymap`'s two mechanisms overlapping in a real application** — a global
   binding shadowing a widget's own `switch`. Survivable via `Warnings()`; the
   failure is still unobserved, and the discipline that avoids it is an
   obligation on applications. The fix remains `Warnings()` reporting a chord
   both bound and handled by an attached widget, not a `Widget` change
   (docs/STATUS.md:1145-1181).
3. **`Chord` normalisation disagreeing with a real terminal** — nothing in its
   folding rules has met a real tty; a normalisation fix is a behaviour change
   (docs/STATUS.md:1182-1184).
4. **Windows being discovered by a user after v1.0.0** — mitigated entirely by
   saying "Linux and macOS" in the first screen (docs/STATUS.md:1195-1196).

Two further entries in the source register are retired, not open: risk 4 (the
colour quantiser being wrong) happened and closed by measurement, and risk 6
(`keymap` slipping a third time) was retired when the package shipped in
v0.6.0 (docs/STATUS.md:1185-1204). They are left out above because they are no
longer triggers.

## Housekeeping — open at v1.0.0

- **`deleteBranchOnMerge`** — false at the repository level; no decision made
  about changing it (docs/STATUS.md:18-19, CHANGELOG.md:210-213).
- **ADR 0003 third-party reference review** — a third-party technical reference
  in a rationale; whether to cut it is the maintainer's open call
  (docs/STATUS.md:20-21).
- **Gate item 8, documentation accuracy** — still open, partly worked; a pass
  that stops halfway is not a pass (docs/STATUS.md:1018, 1293).
- **Gate item 9, prose freeze** — what remains is the housekeeping around the
  rewritten `keymap` apology paragraph (docs/STATUS.md:1019, 1294).
- **"Open at this release" in CHANGELOG.md** — `widgets/widgettest`'s stability
  status, the Windows CI leg's fate, and `deleteBranchOnMerge`; none settled by
  v1.0.0 (CHANGELOG.md:188-213). One of the three has since closed:
  `widgets/widgettest` is explicitly excluded from the v1.0.0 stability promise,
  decided 2026-10-06 (CHANGELOG.md:60-77), and the Windows CI leg item is
  superseded by PR #26, which dropped that leg (CHANGELOG.md:47-51).
  `deleteBranchOnMerge` remains open.

## Decided — a standalone `Scrollbar` widget type was rejected

The proposal to extract a standalone reusable `Scrollbar` widget was reviewed
after v1.0.0 and REJECTED (2026-10-07; recorded in CHANGELOG.md's `[Unreleased]`
Changed entry). Two reasons, both grounded in what the widgets already do:

- A `Scrollbar` type cannot own the `geometry.Budget` decision that gives it
  space against the selection gutter. `List` declares the gutter and the
  scrollbar as competing regions (widgets/data/list.go:174-176) and resolves
  them with `geometry.Budget` inside `adapt` (widgets/data/list.go:444-451);
  the resulting `barW` is subtracted from the row view rect BEFORE rows are
  laid out (widgets/data/list.go:416-418). The budget has to be decided where
  the rows are about to be laid out, so the host would keep the
  `Scrollbar`/`ScrollbarStyle` fields, the region declaration, `barW` and the
  `Invalidate()` contract (widgets/data/list.go:278-288) — the public surface
  would not shrink. `Tree` has the identical shape (widgets/data/tree.go:141,
  710-716); `Table` resolves `barW` the same way (widgets/data/table.go:204-207,
  765) and subtracts it from the body rect (widgets/data/table.go:558-572).
- A `Scrollbar` type would need a second copy of the scroll state or to be
  pushed by its host every frame. Both contradict the Model-ownership rationale
  the virtual engine documents: the `virtual.Model` owns the arithmetic and
  knows nothing about items, the widget owns the items and paints rows, and
  "a widget that owns the arithmetic cannot be shared by three others"
  (virtual/virtual.go:22-24).

What was actually done is the unexported `paintScrollbarThumb` painter
extraction in widgets/data/data.go:198-217: thumb-only, stateless, no new
exported symbol, byte-identical rendering.

## Under consideration — proposed, not yet decided

Marked PROPOSED. These carry no version commitment. STATUS.md:25-29 defines
PROPOSED as "a concrete proposal awaiting review; expect this to move".

- **PROPOSED: a proportional scrollbar for `data.Pager`.** Currently a
  deliberate design decision, not a limitation: the pager walks one row at a
  time, so scrolling is O(one screen of text) and End is O(one screen) walking
  backwards from the last line; there is no proportional scrollbar — the status
  line reports "Ln 1204/9000" instead, which is exact and free
  (widgets/data/pager.go:55-58).
- **PROPOSED: wheel-to-scroll in `form.TextArea`.** ADR 0010 calls it "a
  plausible feature" and explicitly keeps it out of that ADR's scope: a
  `TextArea` that grows wheel-scrolling later does so as its own change, with
  its own tests. No version attached (docs/adr/0010-mouse-routing.md:280-286).
- **PROPOSED: expose `TextInput`'s horizontal scroll offset.** Currently the
  example computes the caret cell from the rune index and clamps — exact for any
  query shorter than the field, approximate beyond. No version attached
  (CHANGELOG.md:285-287).
