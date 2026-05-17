Theming
=======

All colors live in `web/css/theme.css` as CSS Custom Properties on `:root`.

### Current Implementation

`themes.js` swaps the active theme by setting `document.documentElement.setAttribute('data-theme', ...)` and saves the preference to `localStorage`. Override blocks in `theme.css` handle the light variant:

```css
/* Default (dark) — defined on :root */
:root {
  --bg-body: #0f1117;
  --text-primary: #e6e8ef;
  --accent: #5e9eff;
  ...
}

/* Light theme overrides */
[data-theme="light"] {
  --bg-body: #f4f5f8;
  --text-primary: #12131a;
  --accent: #2b6cb0;
  ...
}
```

### Adding a New Theme

Option A — inline override (recommended for small additions):

1. Open `web/css/theme.css`.
2. Append a new attribute selector after the light block, e.g.:

```css
[data-theme="solarized"] {
  --bg-body: #002b36;
  --text-primary: #839496;
  --accent: #268bd2;
  ...
}
```

3. Wire the toggle in `web/js/themes.js` (or expose a selector UI in `index.html`) to call `apply('solarized')`.

Option B — separate file (if you prefer a stylesheet swap):

1. Create `web/css/themes/<name>.css` containing `:root { ... }` overrides.
2. Dynamically create or swap a `<link rel="stylesheet">` in `themes.js` instead of using `data-theme`.

**Rules:**
- No color literals in component styles — everything must go through `var(--*)`.
- Do not add inline styles in HTML or JS.