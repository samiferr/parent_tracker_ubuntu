# 12 – Design System: Lumen

> **Requirement.** The web dashboard MUST be built with the **Lumen design
> system** (the project owner's design system,
> [Lumen](https://claude.ai/artifact/DxE4oAx3r7jjqn1gZBV81j)). Every screen
> uses Lumen tokens and components; nothing in the UI is styled with ad-hoc
> colours, sizes or icons.
>
> Scope: the dashboard (and anything else rendered in a browser). The CLI is not
> a Lumen surface, but its text follows the same content rules (12.3).

Reference revision when this document was written: Lumen artifact version
`1791732233-de71` (read 2026-10-11). The implementation pins its own revision
(DS-10).

## 12.1 Requirements

| ID | Priority | Requirement |
| --- | --- | --- |
| DS-1 | MUST | Lumen is the **only** UI kit. No second component library, CSS framework or icon set on any screen. |
| DS-2 | MUST | **Tokens only.** No hard-coded colour, font size, radius, spacing or control height outside the vendored `tokens.css`. Palette steps such as `blue-900` are allowed **only** inside charts and illustrations, as Lumen specifies. |
| DS-3 | MUST | Use the Lumen component wherever one exists (map in 12.5). Where none exists, build the piece from tokens, in the same shapes (12.6), and document it in the repo. |
| DS-4 | MUST | Support Lumen's **`light` and `dark`** themes. Default follows the OS (`prefers-color-scheme`); a manual override may be kept in the browser. Components are written once for both themes. |
| DS-5 | MUST | UI copy follows Lumen's **content fundamentals** (12.3): plain direct sentences, sentence case, buttons are verbs, say what happened and what to do next, no emoji, no exclamation marks. |
| DS-6 | MUST | **Status is always a word**, never colour or an icon alone. A state or health indicator pairs a label with a dot/icon (`StatusLight`, `Badge`, `InlineAlert`). |
| DS-7 | MUST | Keyboard operable; the 2 px `focus-ring` is never removed; `prefers-reduced-motion` is respected (including live counters and any chart transitions). |
| DS-8 | MUST | Icons come only from Lumen's `Icon` (Lucide set, `Lumen.iconNames`). Icon-only controls carry an `aria-label`. If a needed glyph is missing, use a text label rather than another icon set. |
| DS-9 | MUST | **Self-contained assets.** `tokens.css`, `components/bundle.css`, `components/bundle.js`, React 18 / ReactDOM 18, and the fonts are vendored in the repository and embedded in the daemon. The dashboard makes **no request to any other origin** (no CDN, no web-font service, no analytics) — NFR-5.2. |
| DS-10 | MUST | The vendored Lumen files are **unmodified copies** at a recorded revision (a `LUMEN_VERSION` file naming the Lumen artifact version). Upgrading is a deliberate change that re-runs the conformance checks (12.9). |
| DS-11 | MUST | Third-party notices ship with the package (12.4). |
| DS-12 | SHOULD | The dashboard is usable on a phone/tablet (NFR-8.1) even though Lumen implements the desktop scale only: responsive layout from Lumen spacing tokens, larger control sizes (`l` / `xl`) on narrow screens. |
| DS-13 | SHOULD | Custom pieces (charts, heatmap, timeline strip) meet Lumen's contrast rules: text 4.5:1, meaningful graphics and control boundaries 3:1, and states are never told apart by hue alone. |
| DS-14 | MAY | Reusable custom pieces needed on two or more screens are proposed back to Lumen rather than forked in this repo. |

## 12.2 What Lumen provides

- **Delivery.** `tokens.css`, `components/bundle.css`, React 18, then
  `components/bundle.js`; components are on `window.Lumen`, typed in
  `components/index.d.ts`, each with guidelines in `components/<Name>/README.md`.
- **Themes.** `light` and `dark` (same token names, different values).
- **Tokens.** Colour (palette steps plus semantic tokens such as
  `background-base`, `background-layer-1/2`, `content-default`,
  `content-subdued`, `accent-background`, `informative|positive|notice|negative`
  families with `*-subtle` tints, `focus-ring`), spacing, corner radius,
  component heights, borders, shadows, icon sizes, and type styles.
- **Type.** Source Sans 3 (code: Source Code Pro), referenced **by family name
  only — Lumen ships no font files.** DS-9 therefore makes this project
  responsible for self-hosting them.
- **Components (31).** Icon; Button, ActionButton, Menu; TextField, TextArea,
  SearchField, Dropdown, Checkbox, Switch, RadioGroup, LoginForm; Tabs, Tree,
  Pagination, Steps; Card, Table, List, Accordion, Disclosure, Drawer;
  StatusLight, Badge, ProgressBar, InlineAlert, EmptyState, Avatar,
  AvatarGroup, Timeline; Divider.
- **Icons.** 56 Lucide outline icons.
- **Lineage.** Palette, spacing, radius and size values are adapted from Adobe's
  open-source Spectrum tokens (Apache-2.0); Lumen is independent and not
  affiliated with Adobe.

## 12.3 Rules this project adopts from Lumen

- **Surfaces.** Page = `background-base`; raised panels = `background-layer-1`;
  cards and fields = `background-layer-2`; floating = `background-elevated`.
- **Text.** `content-heading` for headings, `content-default` body,
  `content-subdued` secondary; `content-disabled` is never used for live content.
- **Accent.** One accent hue (blue). `accent-background` fills the **single main
  action** on a screen; `accent-content` colours links.
- **Shape and size.** Buttons, badges, switches and status dots are fully round;
  fields, cards and alerts use `corner-radius-500`. Controls come in the
  `component-height-*` scale (default 32 px).
- **Copy.** Sentence case; verbs on buttons ("Save", "Export report", never "OK"
  or "Submit"); numbers as digits with tabular figures ("12 files", "64%");
  errors say what to do ("Enter a complete date."). Tone stays factual and
  non-shaming (NFR-8.3).
- **Alerts.** Do not stack more than two `InlineAlert`s; combine or prioritise.
- **Tabs.** Three to six tabs.
- **Fields.** Always keep a visible label; never use placeholder text as the label.

## 12.4 Packaging, offline use and licences

- **Vendor directory (proposed):** `internal/server/web/vendor/lumen/` with
  `tokens.css`, `bundle.css`, `bundle.js`, `index.d.ts`, `LUMEN_VERSION`, and
  `vendor/react/` with the React 18 / ReactDOM 18 production builds; fonts under
  `vendor/fonts/`. All embedded into the daemon binary.
- **Fonts.** Obtain Source Sans 3 and Source Code Pro (SIL Open Font License 1.1)
  as `woff2`, declare them with `@font-face` pointing at local files, and keep
  the fallback stack from Lumen's tokens (`system-ui`, …). Never load a font
  from a font-hosting service.
- **Icons.** Bundled inside `bundle.js`; nothing fetched.
- **Third-party notices** (in `/usr/share/doc/parent-tracker/copyright` and the
  About/Help page): Lumen and the Spectrum-derived values (Apache-2.0), Lucide
  (ISC; some shapes MIT via Feather), React (MIT), Source Sans 3 and Source Code
  Pro (SIL OFL 1.1).
- **Provenance gap.** Lumen currently lives as a private design-system artifact.
  Producing a reproducible vendored snapshot from it (export, version stamp,
  checksum) is an open task — see
  [10 – Open Questions](10-open-questions.md) Q15.

## 12.5 Screen-to-component map

| Dashboard element | Lumen component(s) |
| --- | --- |
| Top-level navigation (Today, Hourly history, Trends, Apps, Web*, Alerts) | `Tabs` (max six; *Web only when enabled*). Settings opens from an `ActionButton` into a `Drawer`. |
| User selector | `Dropdown` (visible label) with `Avatar` |
| Date range / view switch (Day, 7 days, 30 days) | `RadioGroup` (horizontal) or `Tabs`; date input per 12.6 |
| Live "Now" card | `Card` + `StatusLight` (state, live indicator) + custom stat figures |
| Degraded-state / stale banners | `InlineAlert` (`notice` or `negative`) |
| Top applications, domains, per-hour app list | `Table` (sortable, `density` regular), `ProgressBar` for share where useful |
| Alerts, tracking gaps, audit events | `Timeline` (newest first) with variant per event type; `Badge` for counts |
| Hour detail (top apps in an hour) | `Drawer` |
| "Show as table" alternative for charts | `Table` |
| Empty and first-run states | `EmptyState` |
| What-is-collected / disclosure text | `Accordion` / `Disclosure` |
| Settings forms | `TextField`, `Dropdown`, `Switch`, `Checkbox`, `RadioGroup`, `Button` |
| Export / report actions | `Menu`, `Button` |
| Paged long tables | `Pagination` |
| Sign-in | See 12.6 (LoginForm is not a drop-in) |

## 12.6 What Lumen does not provide (build from tokens)

Lumen says anything outside its 31 components is built "from tokens in the same
shapes". The dashboard needs these:

| Piece | How to build it |
| --- | --- |
| **Hourly bar chart** (24 stacked bars: Active / Idle / Locked, hatched gap) | SVG from tokens. Categories differ by **lightness and pattern**, not hue alone; legend uses `StatusLight`-style dot + word; "Show as table" toggle. |
| **Hour-of-day heatmap** (days × 24) | SVG/CSS grid with a single-hue sequential ramp from a Lumen palette family; ≥ 5 steps; cell values available as text on focus/hover and in the table view; hatched cells for tracking gaps; bedtime hours outlined. |
| **Day timeline strip** and live "now" edge | Segmented bar using state colours + pattern; gaps drawn as explicit hatched segments (SEC-32). |
| **Date picker** | Lumen has none: compose a `TextField` (`type=date`) with a visible label, or build a popover from tokens. |
| **Tooltip, dialog, toast** | From tokens (`background-elevated`, `shadow-elevated`, `corner-radius-800` for dialogs); keep use minimal. Destructive operations stay in the CLI, so no delete dialogs are needed. |
| **Sign-in screen** | `LoginForm` validates its first field as an **email address** and exposes "remember me"; the dashboard username (default `parent`) is not an email. Compose the screen from `TextField`, `Button` and `InlineAlert` and follow LoginForm's behaviour rules: visible labels, validate on submit, focus the first invalid field, keep the username after a failure, vague server error ("Username or password is incorrect."), `autocomplete` hints. |

## 12.7 Mapping product concepts to Lumen status

State colours must be **non-judgemental** (NFR-8.3): the child's activity is never
shown with `positive` or `negative`. Those semantic variants are reserved for the
**health of the tracker itself**.

| Concept | Exact label | Component / variant |
| --- | --- | --- |
| Session state: active | Active | `StatusLight` `informative` |
| Session state: idle | Idle | `StatusLight` `neutral` |
| Session state: locked | Locked | `StatusLight` `purple` |
| Session state: screen off | Screen off | `StatusLight` `cyan` |
| Session state: not signed in / asleep | Not signed in | plain text, no light |
| Live connection healthy | Live | `StatusLight` `positive` |
| Live connection retrying | Reconnecting | `StatusLight` `notice` |
| Live data not current | Stale | `StatusLight` `negative` |
| Tracking gap (service not recording) | Tracking gap | `StatusLight` `notice` / `Timeline` `notice` |
| App tracking unavailable on this session | App tracking limited | `InlineAlert` `notice` (title + next step) |
| Foreground-app sensor lost (extension disabled) | App sensor lost | `InlineAlert` `notice`, plus `Timeline` entry |

The same state colours are used consistently in the Now card, the day timeline,
the hourly chart legend and the heatmap legend.

## 12.8 Known Lumen limitations and how the project handles them

| Limitation (from Lumen's own conformance notes) | Project policy |
| --- | --- |
| Resting text-field border (`border-field`) is ≈ 1.4:1 on white — below WCAG 1.4.11's 3:1 for control boundaries. | Keep visible labels (Lumen requires them), use the darker hover/focus border. Settings forms are few. If strict WCAG 2.2 AA conformance becomes a MUST, raise it with Lumen rather than patching locally. |
| Borderless `Dropdown` fill is ≈ 1.2:1. | Always show a visible label and chevron; same escalation path. |
| Desktop scale only; larger sizes not matched for fields, checkboxes, radios, switches. | DS-12: responsive layout, larger buttons, adequate spacing; test at phone width (AC-117). |
| No chart, date-picker, dialog or tooltip components. | Built from tokens (12.6). |
| Icon set is 56 glyphs. | No moon/monitor/etc.; use words (DS-8). |

## 12.9 Verification

Automated and manual checks are listed in
[08 – Testing & Acceptance](08-testing-acceptance.md), group L (AC-110…AC-117):
no hard-coded design values, light/dark contrast, zero external requests, local
fonts, keyboard/focus/reduced motion, copy rules, third-party notices, phone
width.
