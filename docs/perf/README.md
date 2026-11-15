# The performance charts

The five SVG files here are the charts in the top-level README. They
show the load tests of 15 November 2026: rented virtual machines
(Hetzner Cloud, Helsinki), load generators on separate machines, one run
each. The machines have since been deleted.

| File | Shows | From `data.json` |
| --- | --- | --- |
| `ladder-resources.svg` | CPUs Churr used and peak memory, for each community size and machine | `ladder` |
| `ladder-delivery.svg` | Delivery time, p50 / p95 / p99, for the same runs, on a logarithmic scale | `ladder` |
| `hardest-case.svg` | 50,000 online all viewing one channel: delivered and median delivery after each fix | `hardest_case` |
| `frame-sizes.svg` | Bytes per frame as JSON and protobuf, compressed and not | `frame_bytes` |
| `hub-delivery.svg` | Delivery time for three servers through one hub, at three rates | `hub` |

`data.json` holds every figure the charts use, and the call test, which
has a table in the README and no chart. A `null` is a figure that was
not recorded. Nothing in it is worked out from anything else.

To draw them again after changing the data:

    node docs/perf/make-charts.mjs

The script has no dependencies and writes plain SVG text. Check the
result with `xmllint --noout docs/perf/*.svg` and by opening the files
on a light and on a dark page.

How they are made to be read:

- Each SVG carries its own background panel and two sets of colours,
  chosen by `prefers-color-scheme`, so it is legible on a light or a
  dark page whichever set it draws in.
- Text is text, in a font stack that falls back to the system's fonts.
  Archivo and Martian Mono are used where they are installed; nothing is
  loaded.
- The colours are the tokens in `docs/STYLE.md`. Where three series
  share a chart the third is the orange of the high-distinction palette,
  not pink, because pink and cyan are hard to tell apart with red-green
  colour blindness. Series also differ by shape, and every chart's
  figures are in a table in the README.
- No chart has two scales on one axis. Delivery times use a logarithmic
  scale, with dots and no bars, because they span 7 ms to 3 s.
- Each has a `<title>` and a `<desc>` that gives its figures in words.
