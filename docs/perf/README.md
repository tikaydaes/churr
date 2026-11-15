# The performance charts

The four SVG files here are the charts in the top-level README.

| File | Shows | From `data.json` |
| --- | --- | --- |
| `ladder-resources.svg` | CPUs Churr uses and peak memory, for each community size and machine | `ladder` |
| `ladder-delivery.svg` | Delivery time, p50 / p95 / p99, for the same runs, on a logarithmic scale | `ladder` |
| `frame-sizes.svg` | Bytes per frame as the web chat (compressed JSON) and the apps (compressed protobuf) send them | `frame_bytes` |
| `hub-delivery.svg` | Delivery time for three servers through one hub, at three posting rates | `hub` |

`data.json` holds every figure the charts use, and the call figures,
which have a table in the README and no chart. A `null` is a figure that
was not recorded. Conditions: synthetic text chat from `tools/loadtest`,
rented virtual machines, load generators on separate machines, one run
per row, measured 15 November 2026.

To draw the charts after changing the data:

    node docs/perf/make-charts.mjs

The script has no dependencies and writes plain SVG text. Check the
result with `xmllint --noout docs/perf/*.svg` and by opening the files
on a light and on a dark page.

Rules the charts follow:

- Each SVG carries its own background panel and two sets of colours,
  chosen by `prefers-color-scheme`, so it is legible on a light or a
  dark page whichever set it draws in.
- Text is text, in a font stack that falls back to the system's fonts.
  Archivo and Martian Mono are used where they are installed; nothing is
  loaded.
- The colours are the tokens in `docs/STYLE.md`. Where three series
  share a chart the third is the orange of the high-distinction palette,
  not pink: pink and cyan are hard to tell apart with red-green colour
  blindness. Series also differ by shape, and every chart's figures are
  in a table in the README.
- No chart has two scales on one axis. Delivery times that span 7 ms to
  1 s use a logarithmic scale, with dots and no bars.
- Each has a `<title>` and a `<desc>` that gives its figures in words.
