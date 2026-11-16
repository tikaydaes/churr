package markdown

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// The corpus lives at the repository root so that the web chat and the apps
// can run the same files.
const corpusDir = "../../testdata/markdown"

// resolverFile holds the names that resolve in every case that does not
// bring its own. It is the one .json file in the directory that is not a
// list of cases.
const resolverFile = "resolver.json"

// minCases is the smallest corpus that still counts as covering the format.
const minCases = 60

var (
	update = flag.Bool("update", false, "rewrite the expected entities, tasks, plain and tree of every corpus case from what the parser gives now; read the diff before keeping it")
	dump   = flag.Bool("dump", false, "print every corpus case with the text each node and entity covers, to review expectations by eye")
)

// corpusCase is one case, in the shape testdata/markdown/README.md documents.
// The expected fields are kept as raw JSON so that the comparison is of JSON
// with JSON: exactly what another implementation will do.
type corpusCase struct {
	Name     string          `json:"name"`
	Note     string          `json:"note,omitempty"`
	Text     string          `json:"text"`
	Resolver *corpusResolver `json:"resolver,omitempty"`
	Entities json.RawMessage `json:"entities"`
	Tasks    json.RawMessage `json:"tasks"`
	Plain    string          `json:"plain"`
	Tree     json.RawMessage `json:"tree"`
}

// corpusTask is how a Task is written in the corpus.
type corpusTask struct {
	At      int    `json:"at"`
	Len     int    `json:"len"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

// corpusResolver is the names that resolve in a case. It implements Resolver.
type corpusResolver struct {
	Accounts       map[string]string `json:"accounts"`
	RemoteAccounts map[string]string `json:"remote_accounts"` // "username@server.name"
	Groups         map[string]string `json:"groups"`
	Broadcasts     bool              `json:"broadcasts"`
	Channels       map[string]string `json:"channels"`
	Custom         map[string]string `json:"emoji"` // custom emoji: "name" or "name@server.name"
	// Zone is the author's IANA zone, in which an instant typed without
	// one is read (ruling R69). Absent leaves such an instant plain.
	Zone string `json:"zone,omitempty"`
	// Patterns are the reference patterns that apply where the text is
	// written (ruling R73).
	RefPatterns []model.ReferencePattern `json:"patterns,omitempty"`
	Hints       []model.ObjectHint       `json:"object_hints,omitempty"`
}

func (r *corpusResolver) Patterns() []model.ReferencePattern { return r.RefPatterns }

// Hints are what the author chose in the composer for ranges of the text.
func (r *corpusResolver) ObjectHints() []model.ObjectHint { return r.Hints }

func (r *corpusResolver) AuthorZone() string { return r.Zone }

func (r *corpusResolver) Account(username string) (model.AccountID, bool) {
	id, ok := r.Accounts[username]
	return model.AccountID(id), ok
}

func (r *corpusResolver) RemoteAccount(username, server string) (model.AccountID, bool) {
	id, ok := r.RemoteAccounts[username+"@"+server]
	return model.AccountID(id), ok
}

func (r *corpusResolver) Group(handle string) (model.GroupID, bool) {
	id, ok := r.Groups[handle]
	return model.GroupID(id), ok
}

func (r *corpusResolver) Broadcast(scope string) bool { return r.Broadcasts }

// Channel answers for a channel link: `channels` maps the identifier of each
// channel the author can see to its name.
func (r *corpusResolver) Channel(id model.ConversationID) (string, bool) {
	name, ok := r.Channels[string(id)]
	return name, ok
}

func (r *corpusResolver) Emoji(name, server string) (model.EmojiID, bool) {
	if server != "" {
		name += "@" + server
	}
	id, ok := r.Custom[name]
	return model.EmojiID(id), ok
}

// corpusFiles returns the case files, in name order.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(corpusDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range all {
		if filepath.Base(f) != resolverFile {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("no case files in %s", corpusDir)
	}
	return files
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// canon puts a value through JSON so that two values are equal exactly when
// their JSON is the same apart from spacing and the order of object keys.
func canon(t *testing.T, v any) any {
	t.Helper()
	data, ok := v.(json.RawMessage)
	if !ok {
		var err error
		if data, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	if len(data) == 0 {
		return nil
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%v in %s", err, data)
	}
	return out
}

// result is everything the parser says about one case.
type result struct {
	entities []model.Entity
	tasks    []corpusTask
	plain    string
	tree     []Node
}

func run(c corpusCase, def *corpusResolver) result {
	r := def
	if c.Resolver != nil {
		r = c.Resolver
	}
	doc := Parse(c.Text)
	res := result{entities: Resolve(doc, r), tasks: []corpusTask{}, tree: doc.Tree()}
	if res.entities == nil {
		res.entities = []model.Entity{}
	}
	for _, task := range Tasks(doc) {
		res.tasks = append(res.tasks, corpusTask(task))
	}
	res.plain = PlainNames(c.Text, res.entities, r.Channel)
	return res
}

func TestCorpus(t *testing.T) {
	var def corpusResolver
	readJSON(t, filepath.Join(corpusDir, resolverFile), &def)

	total := 0
	names := map[string]string{}
	for _, file := range corpusFiles(t) {
		var cases []corpusCase
		readJSON(t, file, &cases)
		total += len(cases)
		base := filepath.Base(file)

		for i := range cases {
			c := &cases[i]
			if c.Name == "" {
				t.Errorf("%s: case %d has no name", base, i)
			}
			if other, dup := names[c.Name]; dup {
				t.Errorf("%s: case name %q is already used in %s", base, c.Name, other)
			}
			names[c.Name] = base
			got := run(*c, &def)

			if *dump {
				dumpCase(t, base, *c, got)
			}
			if *update {
				c.Entities = compact(t, got.entities)
				c.Tasks = compact(t, got.tasks)
				c.Plain = got.plain
				c.Tree = compact(t, got.tree)
				continue
			}

			t.Run(strings.TrimSuffix(base, ".json")+"/"+c.Name, func(t *testing.T) {
				if want, have := canon(t, c.Entities), canon(t, got.entities); !reflect.DeepEqual(want, have) {
					t.Errorf("entities\n text: %q\n want: %s\n have: %s", c.Text, c.Entities, compact(t, got.entities))
				}
				if want, have := canon(t, c.Tasks), canon(t, got.tasks); !reflect.DeepEqual(want, have) {
					t.Errorf("tasks\n text: %q\n want: %s\n have: %s", c.Text, c.Tasks, compact(t, got.tasks))
				}
				if c.Plain != got.plain {
					t.Errorf("plain\n text: %q\n want: %q\n have: %q", c.Text, c.Plain, got.plain)
				}
				if want, have := canon(t, c.Tree), canon(t, got.tree); !reflect.DeepEqual(want, have) {
					t.Errorf("tree\n text: %q\n want: %s\n have: %s", c.Text, c.Tree, compact(t, got.tree))
				}

				// Things that hold for every text, checked here too so
				// that a wrong expectation cannot hide a broken rule.
				names := def.Channel
				if c.Resolver != nil {
					names = c.Resolver.Channel
				}
				// With nil entities every link the resolver knows is
				// named, so a case with a link inside a checklist item,
				// which has no entity, is left out of this check.
				if nilPlain := PlainNames(c.Text, nil, names); nilPlain != got.plain && !strings.Contains(got.plain, UnknownChannel) {
					t.Errorf("Plain with nil entities gave %q, with entities %q", nilPlain, got.plain)
				}
				doc := Parse(c.Text)
				checkEntities(t, c.Text, got.entities)
				checkTasks(t, c.Text, Tasks(doc), got.entities)
				checkTree(t, c.Text, doc)
			})
		}

		if *update {
			if err := os.WriteFile(file, formatCases(t, cases), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("rewrote %s (%d cases)", base, len(cases))
		}
	}
	if total < minCases {
		t.Errorf("the corpus has %d cases; it needs at least %d", total, minCases)
	}
	t.Logf("%d cases", total)
}

// compact writes v as one line of JSON in ASCII: every character outside
// ASCII becomes a \u escape, a pair of them for a character outside the
// basic plane. The corpus is about counting UTF-16 code units, and written
// this way a reader can count them, and no editor can quietly change a
// combining mark or a no-break space.
func compact(t *testing.T, v any) json.RawMessage {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, r := range strings.TrimSuffix(buf.String(), "\n") {
		switch {
		case r < 0x7F:
			out.WriteRune(r)
		case r >= 0x10000:
			hi, lo := (r-0x10000)>>10+0xD800, (r-0x10000)&0x3FF+0xDC00
			fmt.Fprintf(&out, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&out, `\u%04x`, r)
		}
	}
	return json.RawMessage(out.String())
}

// formatCases writes a case file the way the corpus keeps it: one field per
// line, and one entity, task or top-level block per line, so that a change
// to a rule shows up as a small diff.
func formatCases(t *testing.T, cases []corpusCase) []byte {
	t.Helper()
	var b strings.Builder
	list := func(name string, raw json.RawMessage, last bool) {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatal(err)
		}
		end := ",\n"
		if last {
			end = "\n"
		}
		if len(items) == 0 {
			fmt.Fprintf(&b, "    %q: []%s", name, end)
			return
		}
		fmt.Fprintf(&b, "    %q: [\n", name)
		for i, item := range items {
			sep := ","
			if i == len(items)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "      %s%s\n", item, sep)
		}
		fmt.Fprintf(&b, "    ]%s", end)
	}
	b.WriteString("[\n")
	for i, c := range cases {
		b.WriteString("  {\n")
		fmt.Fprintf(&b, "    \"name\": %s,\n", compact(t, c.Name))
		if c.Note != "" {
			fmt.Fprintf(&b, "    \"note\": %s,\n", compact(t, c.Note))
		}
		fmt.Fprintf(&b, "    \"text\": %s,\n", compact(t, c.Text))
		if c.Resolver != nil {
			fmt.Fprintf(&b, "    \"resolver\": %s,\n", compact(t, c.Resolver))
		}
		list("entities", c.Entities, false)
		list("tasks", c.Tasks, false)
		fmt.Fprintf(&b, "    \"plain\": %s,\n", compact(t, c.Plain))
		list("tree", c.Tree, true)
		if i == len(cases)-1 {
			b.WriteString("  }\n")
		} else {
			b.WriteString("  },\n")
		}
	}
	b.WriteString("]\n")
	return []byte(b.String())
}

// dumpCase prints a case the way a reviewer wants to read it: every node and
// entity beside the text it covers.
func dumpCase(t *testing.T, file string, c corpusCase, got result) {
	t.Helper()
	u := toUnits(c.Text)
	cut := func(at, n int) string {
		if at < 0 || n < 0 || at+n > len(u) {
			return "!!out of range!!"
		}
		return fmt.Sprintf("%q", unitsString(u[at:at+n]))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== %s / %s\n", file, c.Name)
	if c.Note != "" {
		fmt.Fprintf(&b, "note:  %s\n", c.Note)
	}
	fmt.Fprintf(&b, "text:  %q\nplain: %q\n", c.Text, got.plain)
	for _, e := range got.entities {
		fmt.Fprintf(&b, "entity %-12s %3d+%-3d %s %s\n", e.Type, e.At, e.Len, cut(e.At, e.Len), compact(t, e))
	}
	for _, task := range got.tasks {
		fmt.Fprintf(&b, "task   checked=%-5v %3d+%-3d %s\n", task.Checked, task.At, task.Len, cut(task.At, task.Len))
	}
	var walk func(nodes []Node, depth int)
	walk = func(nodes []Node, depth int) {
		for _, n := range nodes {
			fmt.Fprintf(&b, "%s%s %d+%d %s", strings.Repeat("  ", depth+1), n.T, n.At, n.Len, cut(n.At, n.Len))
			if n.Level != 0 {
				fmt.Fprintf(&b, " level=%d", n.Level)
			}
			if n.Kind != "" {
				fmt.Fprintf(&b, " kind=%s", n.Kind)
			}
			if n.Start != nil {
				fmt.Fprintf(&b, " start=%d", *n.Start)
			}
			if n.Checked != nil {
				fmt.Fprintf(&b, " checked=%v", *n.Checked)
			}
			if n.Lang != "" {
				fmt.Fprintf(&b, " lang=%s", n.Lang)
			}
			if n.Text != "" {
				fmt.Fprintf(&b, " text=%q", n.Text)
			}
			if n.URL != "" {
				fmt.Fprintf(&b, " url=%q", n.URL)
			}
			if n.Name != "" {
				fmt.Fprintf(&b, " name=%s", n.Name)
			}
			if n.Server != "" {
				fmt.Fprintf(&b, " server=%s", n.Server)
			}
			if n.Ch != "" {
				fmt.Fprintf(&b, " ch=%q", n.Ch)
			}
			if n.Align != nil {
				fmt.Fprintf(&b, " align=%v", n.Align)
			}
			b.WriteString("\n")
			walk(n.C, depth+1)
			walk(n.B, depth+1)
			walk(n.Items, depth+1)
			if n.Head != nil {
				fmt.Fprintf(&b, "%shead\n", strings.Repeat("  ", depth+2))
				walk(n.Head, depth+2)
			}
			for i, row := range n.Rows {
				fmt.Fprintf(&b, "%srow %d\n", strings.Repeat("  ", depth+2), i)
				walk(row, depth+2)
			}
		}
	}
	walk(got.tree, 0)
	t.Log(b.String())
}

// checkEntities checks what section 3 of the format promises of every
// entity list: inside the text, not empty, in order, not overlapping.
func checkEntities(t *testing.T, text string, entities []model.Entity) {
	t.Helper()
	n := utf16Len(text)
	end := 0
	for i, e := range entities {
		if e.Type == "" {
			t.Errorf("entity %d has no type", i)
		}
		if e.Len <= 0 || e.At < 0 || e.At+e.Len > n {
			t.Errorf("entity %d (%s %d+%d) is not inside the text of %d units", i, e.Type, e.At, e.Len, n)
		}
		if e.At < end {
			t.Errorf("entity %d (%s %d+%d) starts before the previous one ends at %d", i, e.Type, e.At, e.Len, end)
		}
		end = e.At + e.Len
	}
}

// checkTasks checks that every task covers its own text and has a task
// entity over the same range, in the same order.
func checkTasks(t *testing.T, text string, tasks []Task, entities []model.Entity) {
	t.Helper()
	u := toUnits(text)
	var ents []model.Entity
	for _, e := range entities {
		if e.Type == model.EntityTask {
			ents = append(ents, e)
		}
	}
	if len(ents) != len(tasks) {
		t.Errorf("%d task entities for %d tasks", len(ents), len(tasks))
		return
	}
	for i, task := range tasks {
		if task.Len <= 0 || task.At < 0 || task.At+task.Len > len(u) {
			t.Errorf("task %d (%d+%d) is not inside the text", i, task.At, task.Len)
			continue
		}
		if got := unitsString(u[task.At : task.At+task.Len]); got != task.Text {
			t.Errorf("task %d: text %q, but its range covers %q", i, task.Text, got)
		}
		if ents[i].At != task.At || ents[i].Len != task.Len {
			t.Errorf("task %d covers %d+%d but its entity covers %d+%d", i, task.At, task.Len, ents[i].At, ents[i].Len)
		}
		if want := fmt.Sprintf("t%d", i+1); ents[i].ItemID != want {
			t.Errorf("task %d has item id %q, want %q", i, ents[i].ItemID, want)
		}
	}
}

// checkTree checks the shape every tree must have whatever the text: each
// node inside its parent, siblings in order without overlap, text spans
// holding exactly the characters they cover, and nothing nested deeper than
// the format allows.
func checkTree(t *testing.T, text string, doc Doc) {
	t.Helper()
	u := toUnits(text)

	var inlines func(spans []Inline, lo, hi, depth int)
	inlines = func(spans []Inline, lo, hi, depth int) {
		if depth > 12 {
			t.Errorf("inline spans nested %d deep", depth)
			return
		}
		at := lo
		for _, s := range spans {
			if s.Len <= 0 || s.At < at || s.At+s.Len > hi {
				t.Errorf("span %s %d+%d does not fit in %d..%d after %d", s.Kind, s.At, s.Len, lo, hi, at)
				return
			}
			at = s.At + s.Len
			if s.Kind == Text {
				if got := unitsString(u[s.At:at]); got != s.Text {
					t.Errorf("text span %d+%d holds %q but covers %q", s.At, s.Len, s.Text, got)
				}
			}
			inlines(s.Children, s.At, at, depth+1)
		}
	}

	var blocks func(bs []Block, lo, hi, quotes, lists int)
	blocks = func(bs []Block, lo, hi, quotes, lists int) {
		at := lo
		for _, b := range bs {
			if b.Len <= 0 || b.At < at || b.At+b.Len > hi {
				t.Errorf("block %s %d+%d does not fit in %d..%d after %d", b.Kind, b.At, b.Len, lo, hi, at)
				return
			}
			end := b.At + b.Len
			at = end
			switch b.Kind {
			case Paragraph:
				inlines(b.Inlines, b.At, end, 0)
			case Heading:
				if b.Level < 1 || b.Level > MaxHeadingLevel {
					t.Errorf("heading level %d", b.Level)
				}
				inlines(b.Inlines, b.At, end, 0)
			case Quote:
				if quotes+1 > MaxQuoteDepth {
					t.Errorf("quote nested %d deep", quotes+1)
				}
				if len(b.Blocks) == 0 {
					t.Errorf("empty quote at %d", b.At)
				}
				blocks(b.Blocks, b.At, end, quotes+1, 0)
			case List:
				if lists+1 > MaxListDepth {
					t.Errorf("list nested %d deep", lists+1)
				}
				if len(b.Items) == 0 {
					t.Errorf("empty list at %d", b.At)
				}
				itemAt := b.At
				for _, it := range b.Items {
					if it.Len <= 0 || it.At < itemAt || it.At+it.Len > end {
						t.Errorf("item %d+%d does not fit in list %d+%d", it.At, it.Len, b.At, b.Len)
						return
					}
					if len(it.Inlines) == 0 {
						t.Errorf("item at %d has no text", it.At)
					}
					if it.Checked && !it.Task {
						t.Errorf("item at %d is ticked but is not a checklist item", it.At)
					}
					inlines(it.Inlines, it.At, it.At+it.Len, 0)
					blocks(it.Lists, it.At+it.Len, end, quotes, lists+1)
					itemAt = it.At + it.Len
					if n := len(it.Lists); n > 0 {
						itemAt = it.Lists[n-1].At + it.Lists[n-1].Len
					}
				}
			case Table:
				if len(b.Head) == 0 || len(b.Head) > MaxTableColumns || len(b.Rows)+1 > MaxTableRows {
					t.Errorf("table of %d columns and %d rows", len(b.Head), len(b.Rows)+1)
				}
				if len(b.Align) != len(b.Head) {
					t.Errorf("table has %d alignments for %d columns", len(b.Align), len(b.Head))
				}
				for _, row := range append([][]Cell{b.Head}, b.Rows...) {
					if len(row) != len(b.Head) {
						t.Errorf("table row of %d cells under a header of %d", len(row), len(b.Head))
					}
					for _, c := range row {
						if c.Len < 0 || c.At < b.At || c.At+c.Len > end {
							t.Errorf("cell %d+%d is outside table %d+%d", c.At, c.Len, b.At, b.Len)
							continue
						}
						inlines(c.Inlines, c.At, c.At+c.Len, 0)
					}
				}
			}
		}
	}
	blocks(doc.Blocks, 0, len(u), 0, 0)
}

// everything resolves any name, so that odd input makes as many entities as
// it possibly can.
type everything struct{}

func (everything) Account(string) (model.AccountID, bool)            { return "a", true }
func (everything) RemoteAccount(_, _ string) (model.AccountID, bool) { return "r", true }
func (everything) Group(string) (model.GroupID, bool)                { return "g", true }
func (everything) Broadcast(string) bool                             { return true }
func (everything) Channel(model.ConversationID) (string, bool)       { return "c", true }
func (everything) Emoji(_, _ string) (model.EmojiID, bool)           { return "e", true }

// groupsOnly resolves every name as a group and nothing else.
type groupsOnly struct{ everything }

func (groupsOnly) Account(string) (model.AccountID, bool) { return "", false }

// exercise runs everything the package offers on one text and checks what
// must hold for any input at all.
func exercise(t *testing.T, text string) {
	t.Helper()
	doc := Parse(text)
	entities := Resolve(doc, everything{})
	checkEntities(t, text, entities)
	checkTasks(t, text, Tasks(doc), entities)
	checkTree(t, text, doc)
	if _, err := json.Marshal(doc.Tree()); err != nil {
		t.Errorf("tree does not marshal: %v", err)
	}

	plain := Plain(text, entities)
	if utf8.ValidString(text) && !utf8.ValidString(plain) {
		t.Errorf("Plain made invalid UTF-8: %q", plain)
	}
	if got := Plain(text, nil); got != plain {
		t.Errorf("Plain with nil entities %q, with entities %q", got, plain)
	}
	// Entities that have nothing to do with the text must be shrugged off.
	n := utf16Len(text)
	Plain(text, []model.Entity{
		{Type: model.EntityAccount, At: -1, Len: 5},
		{Type: model.EntityLink, At: n / 2, Len: n},
		{Type: model.EntityEmoji, At: 1, Len: 1},
		{Type: model.EntityAccount, At: 0, Len: n},
		{Type: model.EntityGroup, At: n, Len: 1 << 62},
		{Type: "unknown", At: 0, Len: 1},
	})
	if t.Failed() {
		t.Logf("text: %q", text)
	}
}

// oddStrings makes a few hundred texts meant to trip a parser: random
// bytes, random mixes of every piece of syntax, deep nesting, and marks
// that are never closed. The seed is fixed so a failure can be repeated.
func oddStrings() []string {
	rng := rand.New(rand.NewSource(20261113))
	pieces := []string{
		"*", "**", "***", "****", "_", "__", "~", "~~", "|", "||", "`", "``", "```", "```go",
		"\n", "\n\n", "\r\n", "\r", " ", "  ", "    ", "\t",
		"- ", "* ", "- [ ] ", "- [x] ", "- [X] ", "1. ", "007. ", "> ", ">", "# ", "#### ", "---", "|---|", "|:-:|--:|",
		"[", "]", "(", ")", "![", "](", "<", ">", "\"", "'", ".", ",", "!", "?", ":", ";", "\\", "@", "#",
		"https://x.example/a_(b)", "http://", "HTTPS://Y.EXAMPLE", "mailto:a@b.example", "javascript:alert(1)",
		"@sam", "@sam.", "@sam@other.example", "@channel", "@here", "@everyone", "#ops", "#dev_ops-",
		":ok:", ":party_parrot:", ":wave@other.example:", ":+1:",
		"a", "B", "word", "é", "e\u0301", "\u00a0", "\u200d", "日本語", "🎉", "👨\u200d👩\u200d👧", "🇳🇱", "𝐀", "\u202e", "\x00", "\xff",
	}
	var out []string

	for i := 0; i < 120; i++ {
		b := make([]byte, rng.Intn(200))
		rng.Read(b)
		out = append(out, string(b))
	}
	for i := 0; i < 260; i++ {
		var sb strings.Builder
		for n := rng.Intn(60); n > 0; n-- {
			sb.WriteString(pieces[rng.Intn(len(pieces))])
		}
		out = append(out, sb.String())
	}

	deep := func(n int) []string {
		return []string{
			strings.Repeat("> ", n) + "x",
			strings.Repeat(">", n) + " x",
			strings.Repeat("  ", n) + "- x",
			nestedList(n),
			strings.Repeat("*", n) + "a" + strings.Repeat("*", n),
			strings.Repeat("**a ", n) + strings.Repeat(" b**", n),
			strings.Repeat("*a **b ", n) + strings.Repeat("c** d* ", n),
			strings.Repeat("*a _b ~~c ||d ", n) + strings.Repeat("e|| f~~ g_ h* ", n),
			strings.Repeat("[", n) + "a" + strings.Repeat("](https://x.example)", n),
			strings.Repeat("[a](", n) + strings.Repeat(")", n),
			strings.Repeat("![", n) + strings.Repeat("]", n),
			strings.Repeat("`", n) + "a",
			strings.Repeat("` ", n),
			strings.Repeat("`a``b```c ", n),
			strings.Repeat("\\", n),
			strings.Repeat("```\n", n),
			strings.Repeat("```go\n", n),
			strings.Repeat("|a", n) + "\n" + strings.Repeat("|-", n) + "\n" + strings.Repeat("|b", n),
			"|a|\n|-|\n" + strings.Repeat("|b|\n", n),
			strings.Repeat("- [ ] @sam https://x.example :ok:\n", n),
			strings.Repeat("@sam", n),
			strings.Repeat("@a.", n),
			strings.Repeat(":a", n) + ":",
			strings.Repeat("#a#", n),
			strings.Repeat("https://", n),
			"https://x.example/" + strings.Repeat("(", n) + strings.Repeat(")", n*2),
			"https://x.example/" + strings.Repeat(").", n),
			strings.Repeat("(", n) + "https://x.example/" + strings.Repeat(")", n),
			strings.Repeat("🎉", n) + "@sam",
			strings.Repeat("e\u0301", n) + " @sam",
			strings.Repeat("a", n) + "@sam",
			strings.Repeat("\r", n),
			strings.Repeat(" \n", n),
			strings.Repeat("||", n),
			strings.Repeat("~~a", n),
			strings.Repeat("_a_", n),
		}
	}
	out = append(out, deep(3)...)
	out = append(out, deep(40)...)
	out = append(out, deep(2500)...)
	return out
}

// nestedList is a list that goes one level deeper on every line.
func nestedList(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(strings.Repeat("  ", i))
		if i%2 == 0 {
			sb.WriteString("- [ ] x\n")
		} else {
			sb.WriteString("1. x\n")
		}
	}
	return sb.String()
}

// TestOddInput is the promise that nothing a person or a program can send
// makes the parser panic, hang or hand back an entity it should not.
func TestOddInput(t *testing.T) {
	texts := oddStrings()
	if len(texts) < 300 {
		t.Fatalf("only %d odd strings", len(texts))
	}
	for i, text := range texts {
		exercise(t, text)
		if t.Failed() {
			t.Fatalf("odd string %d failed", i)
		}
	}
}

// FuzzParse lets `go test -fuzz=FuzzParse` look for more. Without -fuzz it
// runs the seeds, which include every text in the corpus.
func FuzzParse(f *testing.F) {
	for _, file := range corpusFilesF(f) {
		var cases []corpusCase
		data, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		if err := json.Unmarshal(data, &cases); err != nil {
			f.Fatal(err)
		}
		for _, c := range cases {
			f.Add(c.Text)
		}
	}
	f.Add("- [ ] **a** @sam https://x.example/a_(b). :ok:\r\n> ```go\n> x\n> ```")
	f.Fuzz(func(t *testing.T, text string) {
		exercise(t, text)
	})
}

func corpusFilesF(f *testing.F) []string {
	all, err := filepath.Glob(filepath.Join(corpusDir, "*.json"))
	if err != nil {
		f.Fatal(err)
	}
	var files []string
	for _, file := range all {
		if filepath.Base(file) != resolverFile {
			files = append(files, file)
		}
	}
	return files
}

func TestUTF16Helpers(t *testing.T) {
	cases := []struct {
		text  string
		units int
	}{
		{"", 0},
		{"abc", 3},
		{"é", 1},               // one code point, U+00E9
		{"e\u0301", 2},         // e and a combining acute accent
		{"日本語", 3},             // three characters in the basic plane
		{"🎉", 2},               // outside the basic plane: a surrogate pair
		{"👨\u200d👩\u200d👧", 8}, // three pairs joined by two zero-width joiners
		{"🇳🇱", 4},              // a flag is two regional indicators, each a pair
		{"a\xffb", 3},          // a byte that is not UTF-8 counts as U+FFFD
		{"\x00", 1},            // NUL is a character like any other
		{"a\r\nb", 4},          // a line ending is not folded
		{"👍🏽", 4},              // a skin tone is a second pair
		{"𝐀@sam", 6},           // a letter outside the basic plane
		{"\u00a0", 1},          // no-break space
		{"\U0010FFFF", 2},      // the last code point
	}
	for _, c := range cases {
		u := toUnits(c.text)
		if len(u) != c.units {
			t.Errorf("toUnits(%q) has %d units, want %d", c.text, len(u), c.units)
		}
		if n := utf16Len(c.text); n != c.units {
			t.Errorf("utf16Len(%q) = %d, want %d", c.text, n, c.units)
		}
		if utf8.ValidString(c.text) {
			if back := unitsString(u); back != c.text {
				t.Errorf("unitsString(toUnits(%q)) = %q", c.text, back)
			}
		}
	}

	// Looking one character back or forward must see a whole character
	// even when it is a surrogate pair, and must not look past the edges.
	u := toUnits("a🎉é")
	if r, ok := runeAt(u, 1, len(u)); !ok || r != '🎉' {
		t.Errorf("runeAt on a pair = %q, %v", r, ok)
	}
	if r, ok := runeBefore(u, 3, 0); !ok || r != '🎉' {
		t.Errorf("runeBefore on a pair = %q, %v", r, ok)
	}
	if r, ok := runeBefore(u, 4, 0); !ok || r != 'é' {
		t.Errorf("runeBefore = %q, %v", r, ok)
	}
	if _, ok := runeBefore(u, 0, 0); ok {
		t.Error("runeBefore at the start found something")
	}
	if _, ok := runeAt(u, len(u), len(u)); ok {
		t.Error("runeAt at the end found something")
	}
	// Cut in the middle of the pair, each half is seen alone, not joined
	// with a unit on the other side of the edge.
	if r, ok := runeBefore(u, 3, 2); !ok || !isLow(uint16(r)) {
		t.Errorf("runeBefore with half a pair in range = %U, %v", r, ok)
	}
	if r, ok := runeAt(u, 1, 2); !ok || !isHigh(uint16(r)) {
		t.Errorf("runeAt with half a pair in range = %U, %v", r, ok)
	}
}

func TestCharacterClasses(t *testing.T) {
	for _, r := range "aZ9é日ม²Ⅷ𝐀" {
		if !isWord(r) {
			t.Errorf("isWord(%q) = false", r)
		}
	}
	for _, r := range " _-.@#:*🎉\u200d\u00a0、。\u0301\ufe0f\u20e3\u0e31" {
		if isWord(r) {
			t.Errorf("isWord(%q) = true", r)
		}
	}
	for _, r := range " \t\n\r\u00a0\u2003\u3000\u2028" {
		if !isWhite(r) {
			t.Errorf("isWhite(%U) = false", r)
		}
	}
	for _, r := range "a_\u200b\u200d\ufeff\x00" {
		if isWhite(r) {
			t.Errorf("isWhite(%U) = true", r)
		}
	}
	punct := 0
	for c := uint16(0); c < 128; c++ {
		if isPunct(c) {
			punct++
		}
	}
	if punct != 32 {
		t.Errorf("%d ASCII punctuation characters, want 32", punct)
	}
}

func TestLanguages(t *testing.T) {
	// Section 2.3 lists 41 languages and 25 aliases.
	canonical := map[string]bool{}
	for _, name := range languages {
		canonical[name] = true
	}
	if len(canonical) != 41 || len(languages) != 41+25 {
		t.Errorf("%d languages and %d names in all, want 41 and 66", len(canonical), len(languages))
	}
	for word, want := range map[string]string{
		"go": "go", "GoLang": "go", "JS": "javascript", "c++": "cpp", "C#": "csharp",
		"yml": "yaml", "txt": "plaintext", "sh": "bash", "": "", "brainfuck": "", "gö": "",
	} {
		if got := language(toUnits(word)); got != want {
			t.Errorf("language(%q) = %q, want %q", word, got, want)
		}
	}
}

func TestPlainNilEntities(t *testing.T) {
	text := "# Release *notes*\n\n" +
		"**Deploy** at 5, see [the plan](https://x.example/plan) or https://x.example/b.\n" +
		"@sam and #ops :shipit: ||secret|| ~~old~~ `a*b*`\n\n" +
		"> quoted _words_\n\n" +
		"- one\n  - [x] two\n3. three\n\n" +
		"```go\nfunc main() {}\n\n\treturn\n```\n\n" +
		"| a | b |\n|---|--:|\n| 1 | **2** |\n\n---\n" +
		"![cat](https://x.example/cat.png) [bad](javascript:alert) \\*stars\\*"
	want := "Release notes\n" +
		"Deploy at 5, see the plan or https://x.example/b. @sam and #ops :shipit: secret old a*b*\n" +
		"quoted words\n" +
		"one\ntwo\nthree\n" +
		"func main() {}\n\n\treturn\n" +
		"a | b\n1 | 2\n" +
		"https://x.example/cat.png bad *stars*"
	if got := Plain(text, nil); got != want {
		t.Errorf("Plain(nil entities)\n got: %q\nwant: %q", got, want)
	}
	if got := Plain(text, []model.Entity{}); got != want {
		t.Errorf("Plain(no entities)\n got: %q\nwant: %q", got, want)
	}
	if got := Plain(text, Resolve(Parse(text), everything{})); got != want {
		t.Errorf("Plain(resolved entities)\n got: %q\nwant: %q", got, want)
	}
	for _, empty := range []string{"", " ", "\n\n", "---", "```\n```", " \t\r\n"} {
		if got := Plain(empty, nil); got != "" {
			t.Errorf("Plain(%q) = %q, want nothing", empty, got)
		}
	}
}

// TestPlainEntities checks the one thing entities change: a range the
// server stored as a mention or a link is copied as typed, even where this
// parser would have read it differently.
func TestPlainEntities(t *testing.T) {
	// A device with older rules took the whole of `sam_*x*` for a name.
	text := "hi @sam_*x* there"
	if got, want := Plain(text, nil), "hi @sam_x there"; got != want {
		t.Errorf("without entities: %q, want %q", got, want)
	}
	ents := []model.Entity{{Type: model.EntityAccount, At: 3, Len: 8, AccountID: "a"}}
	if got, want := Plain(text, ents), "hi @sam_*x* there"; got != want {
		t.Errorf("with an entity over the name: %q, want %q", got, want)
	}

	// A task entity covers text that is still formatted, so it guards nothing.
	text = "- [ ] *now*"
	ents = []model.Entity{{Type: model.EntityTask, At: 6, Len: 5, ItemID: "t1"}}
	if got, want := Plain(text, ents), "now"; got != want {
		t.Errorf("with a task entity: %q, want %q", got, want)
	}

	// An entity that would cut a surrogate pair in half is ignored.
	text = "🎉*a*"
	ents = []model.Entity{{Type: model.EntityLink, At: 1, Len: 3, URL: "x"}}
	if got, want := Plain(text, ents), "🎉a"; got != want {
		t.Errorf("with an entity inside a pair: %q, want %q", got, want)
	}
}

func TestResolveLookups(t *testing.T) {
	text := "@sam @everyone @channel #ops :shipit: https://x.example\n- [x] done"
	doc := Parse(text)

	// With no resolver, only what needs no lookup is returned.
	got := Resolve(doc, nil)
	want := []model.Entity{
		{Type: model.EntityLink, At: 38, Len: 17, URL: "https://x.example"},
		{Type: model.EntityTask, At: 62, Len: 4, ItemID: "t1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve(nil resolver) = %+v, want %+v", got, want)
	}

	// A name that is not an account is tried as a group handle.
	got = Resolve(Parse("@devs @me @system @here"), groupsOnly{})
	want = []model.Entity{
		{Type: model.EntityGroup, At: 0, Len: 5, GroupID: "g"},
		{Type: model.EntityBroadcast, At: 18, Len: 5, Scope: "here"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve(groups only) = %+v, want %+v", got, want)
	}

	// A Doc put together by hand has no text behind it. Nothing may panic.
	hand := Doc{Blocks: []Block{{Kind: List, Items: []Item{{Task: true, At: 0, Len: 9}, {Task: true, Inlines: []Inline{{Kind: Text, At: 900, Len: 4}}}}}}}
	if tasks := Tasks(hand); len(tasks) != 2 || tasks[0].Text != "" || tasks[1].Text != "" {
		t.Errorf("Tasks(hand-made doc) = %+v", tasks)
	}
	Resolve(hand, everything{})
	if tree := (Doc{}).Tree(); tree == nil || len(tree) != 0 {
		t.Errorf("Tree of an empty Doc = %#v, want an empty list", tree)
	}
}

// TestLargeInput feeds the longest text the server accepts, built to be as
// awkward as possible, and relies on the test timeout to catch a rule that
// has gone quadratic in a way that matters.
func TestLargeInput(t *testing.T) {
	const size = model.MaxMessageText
	for _, unit := range []string{"*a ", "[", "`", "` `` ", "](", "_a", "@a.", ":a", "https://", "|a", "- [ ] x\n", "> ", "**a** ", "~~||", "[a](b", "\\*"} {
		text := strings.Repeat(unit, size/len(unit))
		for i := 0; i < 5; i++ {
			exercise(t, text)
		}
	}
}
