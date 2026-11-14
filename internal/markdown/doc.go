// Package markdown is the server's reader of Churr Markdown, the fixed subset
// of Markdown that message text is written in (docs/MESSAGE-FORMAT.md
// sections 2 to 4). It does not render anything. It parses text into a tree,
// works out the entities the server stores beside a message, lists checklist
// items, and turns text into plain text for search and notifications.
//
// The same rules are implemented by the web chat (JavaScript) and the apps
// (Dart). All three run the cases in testdata/markdown, and the README there
// is the precise statement of every rule, including the ones the format
// document leaves open. If this package and that README disagree, one of
// them has a bug.
package markdown

// BlockKind names a kind of block. The values are the names used in the
// compact tree (Doc.Tree) and in the corpus.
type BlockKind string

// The kinds of block.
const (
	Paragraph BlockKind = "p"
	Heading   BlockKind = "h"
	Quote     BlockKind = "quote"
	List      BlockKind = "list"
	CodeBlock BlockKind = "codeblock"
	Table     BlockKind = "table"
	Rule      BlockKind = "rule"
)

// InlineKind names a kind of inline span. The values are the names used in
// the compact tree and in the corpus.
type InlineKind string

// The kinds of inline span.
const (
	Text     InlineKind = "text"    // plain characters
	Bold     InlineKind = "bold"    // **text**
	Italic   InlineKind = "italic"  // *text* or _text_
	Strike   InlineKind = "strike"  // ~~text~~
	Spoiler  InlineKind = "spoiler" // ||text||
	Code     InlineKind = "code"    // `text`
	Link     InlineKind = "link"    // [text](url) with an allowed address
	NoLink   InlineKind = "nolink"  // [text](url) with any other address: drawn as its text
	Image    InlineKind = "image"   // ![alt](url): drawn as a link to the address
	BareLink InlineKind = "url"     // https://… typed in the text
	Mention  InlineKind = "mention" // @name or @name@server, not yet looked up
	Channel  InlineKind = "channel" // <#conversation-id>, not yet looked up
	Emoji    InlineKind = "emoji"   // :name: or :name@server:, not yet looked up
	Break    InlineKind = "br"      // a line break inside a paragraph
	Escape   InlineKind = "esc"     // a backslash and the punctuation character it escapes
)

// Limits the format sets.
const (
	MaxHeadingLevel = 3   // more `#` are level three
	MaxListDepth    = 3   // deeper items join the third level
	MaxQuoteDepth   = 3   // a `>` inside the third quote is plain text
	MaxTableColumns = 20  // a wider table is shown as typed
	MaxTableRows    = 200 // header and body rows together; a longer table is shown as typed
)

// Doc is a parsed text: a tree of blocks holding inline spans. Every node
// carries its position in the text in UTF-16 code units.
type Doc struct {
	Blocks []Block

	// src is the text as UTF-16, kept so that Tasks can return each item's
	// text without the caller passing the text again.
	src []uint16
}

// Block is one block of a Doc. Which fields are set depends on Kind.
type Block struct {
	Kind BlockKind
	At   int // where the block starts, in UTF-16 code units
	Len  int // how many code units it covers, to the end of its last line

	Level   int      // Heading: 1 to 3
	Inlines []Inline // Paragraph, Heading: the content
	Blocks  []Block  // Quote: the blocks inside

	Ordered bool   // List: numbered rather than bulleted
	Start   int    // List: the first number of a numbered list
	Items   []Item // List

	Lang string // CodeBlock: the highlighting language, or "" for none
	Code string // CodeBlock: the lines between the fences, joined with "\n"

	Align []Align  // Table: one per column
	Head  []Cell   // Table: the header row
	Rows  [][]Cell // Table: the body rows, each as long as Head
}

// Item is one item of a list.
type Item struct {
	At      int      // the marker (`-`, `*` or `1.`)
	Len     int      // from the marker to the end of the item's own line
	Task    bool     // a checklist item, written `- [ ] ` or `- [x] `
	Checked bool     // the box the author typed was ticked
	Inlines []Inline // the item's text, after the marker and the box
	Lists   []Block  // lists nested under this item
}

// Align is how a table column is aligned.
type Align string

// The alignments of a table column.
const (
	AlignNone   Align = "none"
	AlignLeft   Align = "left"
	AlignCenter Align = "center"
	AlignRight  Align = "right"
)

// Cell is one cell of a table. At and Len cover its content without the
// spaces around it; an empty cell has Len 0.
type Cell struct {
	At      int
	Len     int
	Inlines []Inline
}

// Inline is one inline span. Which fields are set depends on Kind.
type Inline struct {
	Kind InlineKind
	At   int // where the span starts, marks included, in UTF-16 code units
	Len  int // how many code units it covers, marks included

	// Text is the characters of a Text span, the content of a Code span
	// without its backticks, or the one character an Escape stands for.
	Text string
	// URL is the address of a Link, Image or BareLink, as typed.
	URL string
	// Name is the name in a Mention or Emoji, without `@` or
	// the colons. Server is the part after the second `@`, if there is one.
	Name   string
	Server string
	// ID is the conversation identifier in a Channel link.
	ID string
	// Children are the spans inside Bold, Italic, Strike, Spoiler, Link and
	// NoLink.
	Children []Inline
}

// Node is one node of the compact tree: the form of a Doc that is written in
// the corpus and compared with the JavaScript and Dart parsers. Blocks and
// inline spans use the same shape; a field is present only where it means
// something. testdata/markdown/README.md describes it node by node.
type Node struct {
	T       string   `json:"t"`
	At      int      `json:"at"`
	Len     int      `json:"len"`
	Level   int      `json:"level,omitempty"`   // h
	Kind    string   `json:"kind,omitempty"`    // list: bullet | numbered
	Start   *int     `json:"start,omitempty"`   // numbered list
	Checked *bool    `json:"checked,omitempty"` // checklist item
	Lang    string   `json:"lang,omitempty"`    // codeblock
	Text    string   `json:"text,omitempty"`    // codeblock, code
	URL     string   `json:"url,omitempty"`     // link, image, url
	Name    string   `json:"name,omitempty"`    // mention, emoji
	ID      string   `json:"id,omitempty"`      // channel
	Server  string   `json:"server,omitempty"`  // mention, emoji
	Ch      string   `json:"ch,omitempty"`      // esc
	Align   []string `json:"align,omitempty"`   // table
	C       []Node   `json:"c,omitempty"`       // inline children
	B       []Node   `json:"b,omitempty"`       // child blocks
	Items   []Node   `json:"items,omitempty"`   // list
	Head    []Node   `json:"head,omitempty"`    // table
	Rows    [][]Node `json:"rows,omitempty"`    // table
}

// Tree returns the compact form of the tree, ready to be written as JSON. It
// is never nil, so an empty text gives `[]`.
func (d Doc) Tree() []Node { return blockNodes(d.Blocks) }

func blockNodes(blocks []Block) []Node {
	out := make([]Node, 0, len(blocks))
	for _, b := range blocks {
		n := Node{T: string(b.Kind), At: b.At, Len: b.Len}
		switch b.Kind {
		case Paragraph:
			n.C = inlineNodes(b.Inlines)
		case Heading:
			n.Level = b.Level
			n.C = inlineNodes(b.Inlines)
		case Quote:
			n.B = blockNodes(b.Blocks)
		case List:
			n.Kind = "bullet"
			if b.Ordered {
				n.Kind = "numbered"
				start := b.Start
				n.Start = &start
			}
			for _, it := range b.Items {
				in := Node{T: "item", At: it.At, Len: it.Len, C: inlineNodes(it.Inlines)}
				if it.Task {
					checked := it.Checked
					in.Checked = &checked
				}
				if len(it.Lists) > 0 {
					in.B = blockNodes(it.Lists)
				}
				n.Items = append(n.Items, in)
			}
		case CodeBlock:
			n.Lang = b.Lang
			n.Text = b.Code
		case Table:
			for _, a := range b.Align {
				n.Align = append(n.Align, string(a))
			}
			n.Head = cellNodes(b.Head)
			for _, row := range b.Rows {
				n.Rows = append(n.Rows, cellNodes(row))
			}
		}
		out = append(out, n)
	}
	return out
}

func cellNodes(cells []Cell) []Node {
	out := make([]Node, 0, len(cells))
	for _, c := range cells {
		out = append(out, Node{T: "cell", At: c.At, Len: c.Len, C: inlineNodes(c.Inlines)})
	}
	return out
}

func inlineNodes(inlines []Inline) []Node {
	if len(inlines) == 0 {
		return nil
	}
	out := make([]Node, 0, len(inlines))
	for _, s := range inlines {
		n := Node{T: string(s.Kind), At: s.At, Len: s.Len}
		switch s.Kind {
		case Code:
			n.Text = s.Text
		case Escape:
			n.Ch = s.Text
		case Link, Image, BareLink:
			n.URL = s.URL
		case Mention, Emoji:
			n.Name = s.Name
			n.Server = s.Server
		case Channel:
			n.ID = s.ID
		}
		n.C = inlineNodes(s.Children)
		out = append(out, n)
	}
	return out
}
