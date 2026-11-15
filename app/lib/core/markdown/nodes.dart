/// The node tree of Churr Markdown (docs/CLIENTS.md section 18,
/// testdata/markdown/README.md). The parser makes it from the text alone;
/// a renderer draws it together with the entities the server sent.
library;

/// The kinds of block.
enum BlockKind { paragraph, heading, quote, list, codeBlock, table, rule }

/// The kinds of inline span.
enum InlineKind {
  text,
  bold,
  italic,
  strike,
  spoiler,
  code,
  link,
  noLink,
  image,
  bareLink,
  mention,
  channel,
  emoji,
  lineBreak,
  escape,
  instant,
}

/// How a table column is aligned.
enum CellAlign { none, left, center, right }

/// Limits the format fixes (MESSAGE-FORMAT.md section 2 and the corpus
/// README's decisions).
const maxHeadingLevel = 3;
const maxListDepth = 3;
const maxQuoteDepth = 3;
const maxTableColumns = 20;
const maxTableRows = 200;

/// A span inside a paragraph, heading, list item or table cell.
///
/// [at] and [len] count UTF-16 code units in the message text, marks
/// included, which is the same unit entities use, so a renderer can match
/// a candidate ([InlineKind.mention], [InlineKind.channel],
/// [InlineKind.emoji], [InlineKind.bareLink]) to its entity by position.
class Inline {
  Inline(
    this.kind,
    this.at,
    this.len, {
    this.text = '',
    this.url = '',
    this.name = '',
    this.server = '',
    this.id = '',
    this.local = '',
    this.zone = '',
    this.children = const [],
  });

  final InlineKind kind;
  final int at;
  int len;

  /// The characters of a text span, the code of a code span, or the one
  /// escaped character.
  String text;

  /// The address of a link, image or bare link.
  final String url;

  /// The name of a candidate mention or emoji, and its server for the
  /// `@name@server` and `:name@server:` forms.
  final String name;
  final String server;

  /// The conversation identifier of a candidate channel link.
  final String id;

  /// An instant's date and time as "YYYY-MM-DDTHH:MM[:SS[.f]]", and its
  /// zone exactly as written, or '' when none was (ruling R69).
  final String local;
  final String zone;

  List<Inline> children;

  /// Where the span ends.
  int get end => at + len;
}

/// A table cell.
class Cell {
  Cell(this.at, this.len, this.inlines);
  final int at;
  final int len;
  final List<Inline> inlines;
}

/// One item of a list.
class ListItem {
  ListItem({
    required this.at,
    required this.len,
    required this.task,
    required this.checked,
    required this.textAt,
    required this.inlines,
  });

  /// The marker, and the length from it to the end of the item's own line.
  final int at;
  final int len;

  /// Whether this is a checklist item, and what the author typed in the
  /// box. The current state lives in the message's `tasks`, not here.
  final bool task;
  final bool checked;

  /// Where the item's text starts, after the marker and the box. A task
  /// entity's `at` is this position, which is how a renderer finds the
  /// item's identifier.
  final int textAt;

  final List<Inline> inlines;

  /// Lists nested under this item.
  final List<Block> lists = [];
}

/// A block of a message.
class Block {
  Block(this.kind, this.at, this.len);

  final BlockKind kind;
  final int at;
  int len;

  /// Heading: 1 to 3.
  int level = 0;

  /// Paragraph and heading: the content.
  List<Inline> inlines = const [];

  /// Quote: the blocks inside.
  List<Block> blocks = const [];

  /// List: numbered or bulleted, the first number, the items.
  bool ordered = false;
  int start = 0;
  final List<ListItem> items = [];

  /// Code block: the canonical language ('' for none) and the code.
  String lang = '';
  String code = '';

  /// Table: alignment per column, the header row and the body rows.
  List<CellAlign> align = const [];
  List<Cell> head = const [];
  final List<List<Cell>> rows = [];
}

/// A parsed message text.
class MarkdownDoc {
  MarkdownDoc(this.blocks);
  final List<Block> blocks;

  /// The tree in the compact JSON form of the shared corpus, so the Dart
  /// parser can be compared with the Go and JavaScript ones case by case.
  List<Map<String, Object?>> toTree() => _blocks(blocks);
}

const _blockNames = {
  BlockKind.paragraph: 'p',
  BlockKind.heading: 'h',
  BlockKind.quote: 'quote',
  BlockKind.list: 'list',
  BlockKind.codeBlock: 'codeblock',
  BlockKind.table: 'table',
  BlockKind.rule: 'rule',
};

const _inlineNames = {
  InlineKind.text: 'text',
  InlineKind.bold: 'bold',
  InlineKind.italic: 'italic',
  InlineKind.strike: 'strike',
  InlineKind.spoiler: 'spoiler',
  InlineKind.code: 'code',
  InlineKind.link: 'link',
  InlineKind.noLink: 'nolink',
  InlineKind.image: 'image',
  InlineKind.bareLink: 'url',
  InlineKind.mention: 'mention',
  InlineKind.channel: 'channel',
  InlineKind.emoji: 'emoji',
  InlineKind.lineBreak: 'br',
  InlineKind.escape: 'esc',
  InlineKind.instant: 'instant',
};

List<Map<String, Object?>> _blocks(List<Block> blocks) => [
  for (final b in blocks)
    {
      't': _blockNames[b.kind],
      'at': b.at,
      'len': b.len,
      if (b.kind == BlockKind.paragraph || b.kind == BlockKind.heading)
        'c': _inlines(b.inlines),
      if (b.kind == BlockKind.heading) 'level': b.level,
      if (b.kind == BlockKind.quote) 'b': _blocks(b.blocks),
      if (b.kind == BlockKind.list) ...{
        'kind': b.ordered ? 'numbered' : 'bullet',
        if (b.ordered) 'start': b.start,
        'items': [
          for (final it in b.items)
            {
              't': 'item',
              'at': it.at,
              'len': it.len,
              'c': _inlines(it.inlines),
              if (it.task) 'checked': it.checked,
              if (it.lists.isNotEmpty) 'b': _blocks(it.lists),
            },
        ],
      },
      if (b.kind == BlockKind.codeBlock) ...{'lang': b.lang, 'text': b.code},
      if (b.kind == BlockKind.table) ...{
        'align': [for (final a in b.align) a.name],
        'head': _cells(b.head),
        'rows': [for (final r in b.rows) _cells(r)],
      },
    },
];

List<Map<String, Object?>> _cells(List<Cell> cells) => [
  for (final c in cells)
    {'t': 'cell', 'at': c.at, 'len': c.len, 'c': _inlines(c.inlines)},
];

List<Map<String, Object?>> _inlines(List<Inline> spans) => [
  for (final s in spans)
    {
      't': _inlineNames[s.kind],
      'at': s.at,
      'len': s.len,
      if (s.kind == InlineKind.code) 'text': s.text,
      if (s.kind == InlineKind.escape) 'ch': s.text,
      if (s.kind == InlineKind.link ||
          s.kind == InlineKind.image ||
          s.kind == InlineKind.bareLink)
        'url': s.url,
      if (s.kind == InlineKind.mention || s.kind == InlineKind.emoji) ...{
        'name': s.name,
        'server': s.server,
      },
      if (s.kind == InlineKind.channel) 'id': s.id,
      if (s.kind == InlineKind.instant) ...{
        'local': s.local,
        if (s.zone.isNotEmpty) 'zone': s.zone,
      },
      'c': _inlines(s.children),
    },
];
