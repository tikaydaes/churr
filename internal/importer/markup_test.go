package importer

import "testing"

type fakeNames struct{}

func (fakeNames) username(id string) (string, bool) {
	if id == "U1" {
		return "ann", true
	}
	return "", false
}

func (fakeNames) channel(id string) (string, bool) {
	if id == "C1" {
		return "0193a0c7-0000-7000-8000-000000000001", true
	}
	return "", false
}

// The table of docs/FEATURES.md section 25.3, and what must be left alone.
func TestConvert(t *testing.T) {
	for in, want := range map[string]string{
		"*bold*":                        "**bold**",
		"_italic_":                      "*italic*",
		"~strike~":                      "~~strike~~",
		"*_both_*":                      "***both***",
		"a *bold* and _it_ word":        "a **bold** and *it* word",
		"<@U1>":                         "@ann",
		"hi <@U1>, meet <@U9|zed>":      "hi @ann, meet @zed",
		"<@U9>":                         "@unknown",
		"see <#C1|general>":             "see <#0193a0c7-0000-7000-8000-000000000001>",
		"see <#C9|secret-plans>":        "see #secret-plans",
		"<!channel> and <!everyone>":    "@channel and @channel",
		"<!here> now":                   "@here now",
		"<!subteam^S1|@designers> look": "@designers look",
		"<https://x.example/a?b=1&amp;c=2|the label>": "[the label](https://x.example/a?b=1&c=2)",
		"<https://x.example>":                         "https://x.example",
		"<mailto:a@b.example|a@b.example>":            "a@b.example",
		"1 &lt; 2 &amp;&amp; 3 &gt; 2":                "1 < 2 && 3 > 2",
		"&gt; quoted":                                 "> quoted",
		":tada: done":                                 ":tada: done",
		// Code is never touched, entities aside.
		"`*not bold* <@U1>`":                 "`*not bold* <@U1>`",
		"```\nif a &lt; b { *p = _x_ }\n```": "```\nif a < b { *p = _x_ }\n```",
		"run `make` then *wait*":             "run `make` then **wait**",
		// Marks that are not emphasis stay as they are.
		"snake_case_name":  "snake_case_name",
		"2*3*4":            "2*3*4",
		"a * b * c":        "a * b * c",
		"*not closed":      "*not closed",
		"_one line\nonly_": "_one line\nonly_",
		"wait... *what*?":  "wait... **what**?",
		"":                 "",
		"<not a reference": "<not a reference",
	} {
		if got := convert(in, fakeNames{}); got != want {
			t.Errorf("convert(%q)\n  got  %q\n  want %q", in, got, want)
		}
	}
}
