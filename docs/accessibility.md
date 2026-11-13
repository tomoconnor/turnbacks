# Accessibility review

Target: WCAG 2.1 level AA.

## Automated checks

`web/e2e/a11y.spec.ts` runs [axe-core](https://github.com/dequelabs/axe-core) against every main screen
(record, dashboard, problems, problem detail, business case, the "certificate needed" admin screen and the
administration screens, including the Webhooks and Audit log tabs) with the `wcag2a`, `wcag2aa`, `wcag21a` and `wcag21aa` rule sets, in both the light and
the dark theme. It currently reports **no violations**. The review found and fixed:

- the problem search combobox referenced a list that was not in the DOM (`aria-controls` invalid);
- metric switches used tab semantics without tab panels — replaced with an ARIA radio group with
  arrow-key navigation.

Automated tools catch only part of the picture; the manual review below covers the rest.

## Manual review

| Area | Result |
| --- | --- |
| **Keyboard** | Everything is reachable with Tab. Recording needs no mouse: `/` search, `1`–`9` quick problem, `1`–`6` duration, `+`/`−` how many times, `N` new problem, `Enter` submit, `Esc` clear, `?` shortcuts help. Shortcuts are disabled while typing in a field and in dialogs |
| **Focus** | Visible focus outline on all interactive elements; dialogs (Radix) trap focus and restore it on close; after recording, focus returns to search |
| **Skip link** | “Skip to content” is the first focusable element |
| **Semantics** | Landmarks (`header`, `nav` with labels, `main`), one `h1` per page, labelled form controls, presets exposed as a radio group with `aria-checked`, the chosen problem announced as text, `role="alert"` for validation errors, toasts in a live region |
| **Not colour alone** | Changes show arrows and signed percentages; status uses text badges; charts have tables or text summaries alongside (top problems table, Pareto summary sentence, before/after text list) |
| **Contrast** | Default palette meets AA for text; muted text uses a darker grey than shadcn's default |
| **Zoom and reflow** | Layout reflows to a single column at 320 CSS px; no horizontal scrolling on the record page; touch targets for presets and quick buttons are at least 44 px high |
| **Motion** | No essential animation; charts render without animation |
| **Forms** | Inputs have visible labels; errors are announced and described in text; destructive actions ask for confirmation |
| **Language** | `lang="en-GB"` |

## Known limitations

- Charts (Recharts SVG) are not fully navigable by screen readers; the same information is available
  in adjacent tables and summaries, and CSV exports provide the underlying data.
- Native `datetime-local` pickers vary in accessibility across browsers.
- Single-character shortcuts can be surprising for speech-input users; they only act when focus is
  outside text fields, and every action also has an on-screen control (WCAG 2.1.4).
