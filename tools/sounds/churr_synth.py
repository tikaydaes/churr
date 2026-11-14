"""Writes Churr's ring and start-up sounds.

The sounds are synthetic. Their numbers come from measuring a recording
of a European nightjar's churr (13 November 2026 analysis):

  slow phrase: 27.9 pulses a second, each about 5 ms wide, sweeping down
               from about 1930 Hz to 1200 Hz, strongest near 1870 Hz;
               held for 1.6 to 5 seconds (median 3.5)
  fast phrase: 45.9 pulses a second, each about 3.6 ms wide, sweeping
               from about 1580 Hz to 1200 Hz, strongest near 1450 Hz,
               about 3 dB quieter; held for 0.35 to 0.7 seconds

The bird alternates the two. No part of the recording is used.

Run:  python3 tools/sounds/churr_synth.py art/sounds
Needs numpy. Output is 44.1 kHz 16-bit mono WAV.
"""
import sys, wave, numpy as np

SR = 44100
SLOW = dict(rate=27.9, width=0.0051, f0=1930.0, f1=1200.0, gain=1.0)
FAST = dict(rate=45.9, width=0.0036, f0=1580.0, f1=1200.0, gain=0.71)


def pulse(width, f0, f1, soft):
    """One click of the churr: a quick rise, a slower fall, pitch sliding down."""
    w = width * (1.9 if soft else 1.0)
    n = int(SR * w * 4)
    t = np.arange(n) / SR
    rise = w * 0.6
    env = (t / rise) ** 2 * np.exp(-2 * (t - rise) / rise)
    env /= env.max()
    # pitch holds near the top while the pulse is loud, then drops away in
    # its tail, as measured across a pulse
    span = (f0 - f1) * (0.6 if soft else 1.0)
    freq = (f0 - span) + span / (1 + np.exp((t - rise * 2.1) / (w * 0.22)))
    phase = 2 * np.pi * np.cumsum(freq) / SR
    tone = np.sin(phase)
    if soft:
        tone += 0.35 * np.sin(phase / 2)  # an octave below, for warmth
    return env * tone


def phrase(seconds, a, b=None, soft=False, rng=None):
    """A run of pulses. With b given, glides from a's numbers to b's."""
    out = np.zeros(int(SR * seconds) + SR // 4)
    t = 0.0
    while t < seconds:
        k = t / seconds if b else 0.0
        mix = lambda key: a[key] + (b[key] - a[key]) * k if b else a[key]
        p = pulse(mix("width"), mix("f0"), mix("f1"), soft) * mix("gain")
        p *= 1 + rng.normal(0, 0.06)                     # the bird is not a metronome
        i = int(t * SR)
        out[i:i + len(p)] += p[: len(out) - i]
        t += (1 / mix("rate")) * (1 + rng.normal(0, 0.012))
    return out[: int(SR * seconds)]


def fade(x, in_s, out_s):
    n_in, n_out = int(SR * in_s), int(SR * out_s)
    x = x.copy()
    x[:n_in] *= np.linspace(0, 1, n_in) ** 2
    x[-n_out:] *= np.linspace(1, 0, n_out) ** 2
    return x


def silence(seconds):
    return np.zeros(int(SR * seconds))


def ring(soft, rng):
    """Four seconds, made to loop: slow, fast, slow, then a rest."""
    return np.concatenate([
        fade(phrase(1.40, SLOW, soft=soft, rng=rng), 0.08, 0.02),
        fade(phrase(0.50, FAST, soft=soft, rng=rng), 0.01, 0.02),
        fade(phrase(0.95, SLOW, soft=soft, rng=rng), 0.01, 0.25),
        silence(1.15),
    ])


def startup(soft, rng):
    """A short wind-up: the fast phrase easing into the slow one and away."""
    body = np.concatenate([
        fade(phrase(0.28, FAST, soft=soft, rng=rng), 0.03, 0.005),
        phrase(0.22, FAST, SLOW, soft=soft, rng=rng),
        fade(phrase(0.80, SLOW, soft=soft, rng=rng), 0.005, 0.6),
    ])
    # a faint held pair of notes two octaves under the two phrases' pitches
    t = np.arange(len(body)) / SR
    bed = 0.10 * (np.sin(2 * np.pi * 362.5 * t) + np.sin(2 * np.pi * 467.5 * t))
    bed *= np.minimum(1, t / 0.15) * np.exp(-t / 0.55)
    return np.concatenate([body + bed, silence(0.1)])


def write(path, x):
    x = x / np.abs(x).max() * 0.89
    with wave.open(path, "wb") as w:
        w.setnchannels(1); w.setsampwidth(2); w.setframerate(SR)
        w.writeframes((x * 32767).astype("<i2").tobytes())


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "."
    # The owner chose the soft ring and the faithful start-up sound
    # (14 November 2026). Pass "all" as a second argument to also write the
    # two versions that were not chosen, for comparison.
    chosen = (("ring", ring, True), ("startup", startup, False))
    for name, make, soft in chosen:
        rng = np.random.default_rng(20261114)            # same file every run
        write(f"{out}/churr-{name}.wav", make(soft, rng))
    if len(sys.argv) > 2 and sys.argv[2] == "all":
        for name, make, soft in chosen:
            rng = np.random.default_rng(20261114)
            other = "faithful" if soft else "soft"
            write(f"{out}/churr-{name}-{other}.wav", make(not soft, rng))
