package bots

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// The converters are tested from files in testdata/webhooks at the top of
// the repository, one directory per format:
//
//	<name>.in.json     the payload as the outside service sends it
//	<name>.out.json    the Payload it becomes; {"error": "<code>"} for a
//	                   refusal; null for a GitHub event that posts nothing
//	<name>.parts.json  Discord only: the file parts sent with it
//
// A GitHub case is named <event>.<case>, and the event is taken from the
// name. Run `go test ./internal/bots -run TestConvert -update` to write the
// .out.json files from what the converters produce, then read every change.
var update = flag.Bool("update", false, "rewrite the expected converter outputs")

const webhookData = "../../testdata/webhooks"

// githubNow is the arrival time every GitHub case is converted at.
var githubNow = model.Time(1794562200000) // 2026-11-13T09:30:00.000Z

// convertCases runs convert over every .in.json file of one directory and
// returns how many cases there were.
func convertCases(t *testing.T, dir string, convert func(name string, in []byte) (any, error)) int {
	t.Helper()
	inputs, err := filepath.Glob(filepath.Join(webhookData, dir, "*.in.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range inputs {
		name := strings.TrimSuffix(filepath.Base(path), ".in.json")
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := convert(name, in)
			if err != nil {
				e, ok := model.AsError(err)
				if !ok {
					t.Fatalf("an error that is not a refusal: %v", err)
				}
				got = map[string]any{"error": e.Code}
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(got); err != nil {
				t.Fatal(err)
			}
			outPath := strings.TrimSuffix(path, ".in.json") + ".out.json"
			if *update {
				if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("no expected output: %v", err)
			}
			// Compared as JSON values, so the layout of the expected
			// file does not matter.
			var g, w any
			if err := json.Unmarshal(buf.Bytes(), &g); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &w); err != nil {
				t.Fatalf("%s is not JSON: %v", outPath, err)
			}
			gb, _ := json.MarshalIndent(g, "", "  ")
			wb, _ := json.MarshalIndent(w, "", "  ")
			if !bytes.Equal(gb, wb) {
				t.Errorf("converted to\n%s\nwant\n%s", gb, wb)
			}
		})
	}
	return len(inputs)
}

func TestConvert(t *testing.T) {
	total := 0
	t.Run("slack", func(t *testing.T) {
		total += convertCases(t, "slack", func(_ string, in []byte) (any, error) { return ConvertSlack(in) })
	})
	t.Run("discord", func(t *testing.T) {
		total += convertCases(t, "discord", func(name string, in []byte) (any, error) {
			var parts []DiscordPart
			if b, err := os.ReadFile(filepath.Join(webhookData, "discord", name+".parts.json")); err == nil {
				if err := json.Unmarshal(b, &parts); err != nil {
					t.Fatal(err)
				}
			}
			return ConvertDiscord(in, parts)
		})
	})
	t.Run("github", func(t *testing.T) {
		total += convertCases(t, "github", func(name string, in []byte) (any, error) {
			event, _, _ := strings.Cut(name, ".")
			p, post, err := ConvertGitHub(event, in, githubNow)
			if err != nil || !post {
				return nil, err
			}
			return p, nil
		})
	})
	if total < 40 {
		t.Errorf("only %d payload cases; the suite must hold at least 40", total)
	}
}

func TestColors(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"good", "green"}, {"warning", "yellow"}, {"danger", "red"},
		{"#36a64f", "green"}, {"36a64f", "green"}, {"#e01e5a", "pink"}, {"#ecb22e", "yellow"}, {"#2eb67d", "green"},
		{"#36c5f0", "cyan"}, {"#4a154b", "pink"}, {"#7c3aed", "lavender"}, {"#ff00aa", "pink"}, {"#f00", "red"},
		{"#ffffff", "grey"}, {"#000000", "grey"}, {"#808080", "grey"}, {"#0a0a0a", "grey"},
		{"lavender", "lavender"}, {"Cyan", "cyan"}, {"purple", "grey"}, {"#12345", "grey"}, {"", "grey"},
		// The edges of the hue table: each bound belongs to the range
		// that starts there.
		{"#ff5500", "yellow"}, // hue 20
		{"#ff5400", "red"},    // just under 20
		{"#d4ff00", "green"},  // hue 70
		{"#00ffaa", "cyan"},   // hue 160
		{"#006aff", "lavender"},
		{"#d500ff", "pink"}, // hue 290
		{"#ff003f", "red"},  // hue 345
	} {
		if got := colorFromName(c.in); got != c.want {
			t.Errorf("colour %q is %s, want %s", c.in, got, c.want)
		}
	}
	if got := colorFromInt(15158332); got != "red" { // the example of section 13
		t.Errorf("Discord's 15158332 is %s, want red", got)
	}
	if got := colorFromInt(-1); got != "grey" {
		t.Errorf("a colour out of range is %s, want grey", got)
	}
}

func TestCardText(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"plain", "Just *text* here", "Just *text* here"},
		{"heading", "## Summary\nAll good", "**Summary**\nAll good"},
		{"heading with closing marks", "# Title #", "**Title**"},
		{"quote", "> first\n> > nested\nafter", "first\nnested\nafter"},
		{"code block", "Run:\n```sh\nmake test\nmake `x`\n```\ndone", "Run:\n`make test`\n`` make `x` ``\ndone"},
		{"list kept", "- one\n- two", "- one\n- two"},
		{"table and rule kept", "| a | b |\n|---|---|\n---", "| a | b |\n|---|---|\n---"},
	} {
		if got := cardText(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	long := cardText(strings.Repeat("x", 5000))
	if n := len([]rune(long)); n != 4000 || !strings.HasSuffix(long, "…") {
		t.Errorf("a long text is cut to %d characters ending %q", n, long[len(long)-3:])
	}
	if got := fieldValue("one\ntwo\n\nthree"); got != "one two three" {
		t.Errorf("a field value is on one line: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo wörld", 20); got != "héllo wörld" {
		t.Errorf("short text changed: %q", got)
	}
	if got := truncate("héllo wörld", 7); got != "héllo…" {
		t.Errorf("cut to %q", got)
	}
	if got := truncateAtWord("one two three four", 12); got != "one two…" {
		t.Errorf("cut at a word to %q", got)
	}
	// Slack text longer than a message is cut and ends with the mark.
	p, err := ConvertSlack([]byte(`{"text": "` + strings.Repeat("a", 20000) + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(p.Text)); n != model.MaxMessageText || !strings.HasSuffix(p.Text, "…") {
		t.Errorf("long Slack text is %d characters", n)
	}
}
