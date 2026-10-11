# UI check

Optional headless-browser checks of the dashboard. They are not part of
`make check` because they need Node and a Chromium binary.

```bash
make demo                           # terminal 1: dashboard on made-up data, http://127.0.0.1:8787
cd tools/ui-check && npm ci
BASE=http://127.0.0.1:8787 node check.mjs
```

Set `CHROMIUM` to a Chromium binary if Playwright cannot find one, and `OUT` to
a directory to keep screenshots of every tab in both themes and both widths.

What it checks, for light and dark at 1280 px and 360 px: a wrong password shows
a message; the theme follows the OS; no console errors; no request to another
origin; no sideways scrolling; every control has a name; one `h1` and one `main`
per screen; keyboard focus shows a ring.
