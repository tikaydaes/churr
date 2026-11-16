import 'package:flutter/material.dart';

import '../../../core/api/conversation_ops.dart';
import '../../../core/commands/commands.dart';
import '../../../core/commands/help.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../message/card_names.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import 'command_dialogs.dart' show commandGroupName, helpGroups;

// /help (ruling R74): every command the person can use here, grouped, with
// its one-line summary, then "Things you can type", all searchable;
// choosing a command, or naming it after /help, shows its full entry, and
// from there it can go in the message box. Everything it says comes from
// lib/core/commands/help.dart, from the source the web chat draws from too.

/// The help panel. It pops with the name of the command to put in the
/// message box, or nothing.
class HelpPanel extends StatefulWidget {
  const HelpPanel({
    super.key,
    required this.commands,
    this.query = '',
    this.sealed = false,
    this.serverPatterns = const [],
    this.herePatterns = const [],
  });

  /// The server's command list for the conversation, which leaves out what
  /// is hidden for this person.
  final List<SlashCommand> commands;

  /// What followed /help: a command's name opens its entry; anything else
  /// searches.
  final String query;
  final bool sealed;

  /// The reference patterns of the server and of this channel.
  final List<Json> serverPatterns, herePatterns;

  @override
  State<HelpPanel> createState() => _HelpPanelState();
}

class _HelpPanelState extends State<HelpPanel> {
  final search = TextEditingController();
  final thingsKey = GlobalKey();
  final scroll = ScrollController();
  HelpEntry? open;
  String? opener;

  @override
  void initState() {
    super.initState();
    final named = helpFind(widget.commands, widget.query);
    if (named != null) {
      open = helpEntry(named, widget.commands);
      opener = named.name;
    } else {
      search.text = widget.query;
    }
  }

  @override
  void dispose() {
    search.dispose();
    scroll.dispose();
    super.dispose();
  }

  void _show(SlashCommand c) => setState(() {
    open = helpEntry(c, widget.commands);
  });

  @override
  Widget build(BuildContext context) =>
      open == null ? _overview(context) : _entry(context, open!);

  // ---- the overview ----

  Widget _overview(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final groups = helpGroups(widget.commands, search.text);
    final things = helpThings(
      sealed: widget.sealed,
      server: widget.serverPatterns,
      here: widget.herePatterns,
    ).where((x) => x.matches(search.text)).toList();
    final md = helpMarkdown.where((x) => x.matches(search.text)).toList();
    final hasThings = things.isNotEmpty || md.isNotEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
          child: TextField(
            key: const Key('help-search'),
            controller: search,
            autofocus: true,
            decoration: InputDecoration(labelText: t.helpSearch),
            onChanged: (_) => setState(() {}),
          ),
        ),
        Expanded(
          // Everything is built at once (it is a few dozen rows), so the
          // jump to the things people can type can reach them.
          child: SingleChildScrollView(
            controller: scroll,
            padding: const EdgeInsets.only(bottom: 24),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (hasThings)
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      key: const Key('help-jump'),
                      onPressed: () {
                        final target = thingsKey.currentContext;
                        if (target != null) Scrollable.ensureVisible(target);
                      },
                      child: Text(helpWords('jumpThings')),
                    ),
                  ),
                if (groups.isEmpty)
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(t.helpNone, style: TextStyle(color: c.dim)),
                  ),
                for (final (g, cmds) in groups) ...[
                  _sectionName(c, commandGroupName(t, g)),
                  for (final cmd in cmds) _row(context, t, c, cmd),
                ],
                if (hasThings) ..._things(context, c, things, md),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _sectionName(ChurrColors c, String text, {Key? key}) => Padding(
    key: key,
    padding: const EdgeInsets.fromLTRB(16, 14, 16, 4),
    child: Semantics(
      header: true,
      child: Text(
        text.toUpperCase(),
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w700,
          letterSpacing: 1,
          color: c.faint,
        ),
      ),
    ),
  );

  Widget _row(BuildContext context, L10n t, ChurrColors c, SlashCommand cmd) {
    final aliases = cmd.kind == 'builtin'
        ? [for (final a in commandAliasesOf(cmd.name)) '/$a']
        : const <String>[];
    final detail = [
      if (cmd.description.isNotEmpty) cmd.description,
      if (aliases.isNotEmpty) t.helpAlso(aliases.join(', ')),
    ].join(' · ');
    return ListTile(
      key: Key('help-${cmd.name}'),
      dense: true,
      title: Text.rich(
        TextSpan(
          children: [
            TextSpan(
              text: '/${cmd.name}',
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
            if ((cmd.hint ?? '').isNotEmpty)
              TextSpan(
                text: ' ${cmd.hint}',
                style: TextStyle(color: c.dim),
              ),
          ],
        ),
      ),
      subtitle: detail.isEmpty ? null : Text(detail),
      onTap: () {
        opener = cmd.name;
        _show(cmd);
      },
      trailing: Tooltip(
        message: helpWords('open', {'name': cmd.name}),
        child: ExcludeSemantics(
          child: Glyph(ChurrIcons.chevronRight, size: 16, color: c.faint),
        ),
      ),
    );
  }

  List<Widget> _things(
    BuildContext context,
    ChurrColors c,
    List<HelpThing> things,
    List<HelpThing> md,
  ) => [
    _sectionName(c, helpWords('things'), key: thingsKey),
    _para(helpWords('thingsIntro'), color: c.dim),
    Container(
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
      decoration: BoxDecoration(
        color: c.bg,
        border: Border(left: BorderSide(color: c.lavender, width: 4)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Semantics(
            header: true,
            child: Text(
              helpWords('popupRules'),
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
          ),
          for (final r in helpPopupRules)
            Padding(padding: const EdgeInsets.only(top: 4), child: Text(r)),
        ],
      ),
    ),
    for (final x in things) _thing(context, c, x),
    if (md.isNotEmpty) ...[
      _sectionName(c, helpWords('formatting')),
      for (final m in md)
        Padding(
          key: Key('help-md-${m.id}'),
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 4),
          child: _pair(_code(c, m.type), Text(m.becomes)),
        ),
    ],
  ];

  Widget _thing(BuildContext context, ChurrColors c, HelpThing x) {
    final ex = x.example;
    return ExpansionTile(
      key: Key('help-thing-${x.id}'),
      tilePadding: const EdgeInsets.symmetric(horizontal: 16),
      childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
      expandedCrossAxisAlignment: CrossAxisAlignment.start,
      shape: const Border(),
      collapsedShape: const Border(),
      title: Text(x.title, style: const TextStyle(fontWeight: FontWeight.w700)),
      subtitle: Text(
        x.type,
        style: TextStyle(fontFamily: fontMono, fontSize: 12, color: c.dim),
      ),
      children: [
        _fact(c, helpWords('becomes'), x.becomes),
        _fact(c, helpWords('popup'), x.popup),
        if (x.patterns != null) ..._patterns(c, x.patterns!),
        if (x.units != null)
          for (final k in x.units!)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: _pair(
                Text(
                  k.title,
                  style: const TextStyle(fontWeight: FontWeight.w700),
                ),
                Text(
                  [for (final u in k.units) u.typed.join(' or ')].join(', '),
                ),
              ),
            ),
        if (ex != null)
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _label(c, helpWords('example')),
                _code(c, ex.type),
                Text(ex.then),
              ],
            ),
          ),
        if (x.sealed.isNotEmpty) _fact(c, helpWords('inSealed'), x.sealed),
      ],
    );
  }

  List<Widget> _patterns(ChurrColors c, HelpPatterns p) {
    List<Widget> list(String title, List<HelpPattern> rows) => [
      if (rows.isNotEmpty) ...[
        Padding(
          padding: const EdgeInsets.only(top: 6),
          child: _label(c, title),
        ),
        for (final r in rows)
          Text(
            '${r.name} (${r.prefix}): ${helpWords('patternExample', {'example': r.example, 'host': r.host})}',
          ),
      ],
    ];
    return [
      ...list(helpWords('patternsHere'), p.here),
      ...list(helpWords('patternsServer'), p.server),
      if (p.none)
        Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Text(helpWords('patternsNone')),
        ),
      if (p.sealed)
        Text(helpWords('patternsSealed'), style: TextStyle(color: c.dim)),
    ];
  }

  // ---- one command's entry ----

  Widget _entry(BuildContext context, HelpEntry e) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final facts = [
      if (e.aliases.isNotEmpty) (helpWords('aliases'), e.aliases.join(', ')),
      if (e.where.isNotEmpty) (helpWords('where'), e.where),
      if (e.who.isNotEmpty) (helpWords('who'), e.who),
    ];
    return Column(
      key: Key('help-entry-${e.name}'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    key: const Key('help-back'),
                    onPressed: () => setState(() => open = null),
                    icon: const Glyph(ChurrIcons.arrowLeft, size: 16),
                    label: Text(helpWords('back')),
                  ),
                ),
                Semantics(
                  header: true,
                  child: Text(
                    '/${e.name}',
                    style: const TextStyle(
                      fontSize: 24,
                      fontWeight: FontWeight.w900,
                    ),
                  ),
                ),
                if (e.summary.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 4),
                    child: Text(e.summary, style: TextStyle(color: c.dim)),
                  ),
                _heading(c, helpWords('usage')),
                _code(c, e.usage),
                for (final a in e.args)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: _pair(_code(c, a.name), Text(a.about)),
                  ),
                if (e.description.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 14),
                    child: Text(e.description),
                  ),
                if (e.examples.isNotEmpty) ...[
                  _heading(c, helpWords('examples')),
                  for (final x in e.examples)
                    Container(
                      margin: const EdgeInsets.only(top: 6),
                      padding: const EdgeInsets.all(10),
                      color: c.bg,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [_code(c, x.type), Text(x.then)],
                      ),
                    ),
                ],
                if (e.more.isNotEmpty) ...[
                  _heading(
                    c,
                    helpWords(e.moreKind == 'spreads' ? 'spreads' : 'decks'),
                  ),
                  for (final r in e.more)
                    Padding(
                      key: Key('help-more-${r.name}'),
                      padding: const EdgeInsets.only(top: 6),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text.rich(
                            TextSpan(
                              children: [
                                TextSpan(
                                  text: r.name,
                                  style: const TextStyle(
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                                if (r.aliases.isNotEmpty)
                                  TextSpan(
                                    text: ' (${r.aliases.join(', ')})',
                                    style: TextStyle(color: c.dim),
                                  ),
                                TextSpan(text: ' · ${r.count}'),
                              ],
                            ),
                          ),
                          Text(
                            r.positions.isEmpty
                                ? r.about
                                : '${tarotSpreadName(t, r.name)}: ${[for (final p in r.positions) tarotPosition(t, p)].join(', ')}',
                            style: TextStyle(color: c.dim),
                          ),
                        ],
                      ),
                    ),
                ],
                for (final (k, v) in facts) _fact(c, k, v, top: 12),
                for (final n in e.notes)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(n, style: TextStyle(color: c.dim)),
                  ),
                if (e.related.isNotEmpty) ...[
                  _heading(c, helpWords('related')),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final r in e.related)
                        OutlinedButton(
                          key: Key('help-related-$r'),
                          onPressed: () {
                            final to = widget.commands
                                .where((x) => x.name == r)
                                .firstOrNull;
                            if (to != null) _show(to);
                          },
                          child: Text('/$r'),
                        ),
                    ],
                  ),
                ],
              ],
            ),
          ),
        ),
        Container(
          color: c.raised,
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
          alignment: Alignment.centerRight,
          child: FilledButton(
            key: const Key('help-put'),
            onPressed: () => Navigator.of(context).pop(e.name),
            child: Text(t.helpInsert(e.name)),
          ),
        ),
      ],
    );
  }

  // ---- pieces ----

  Widget _heading(ChurrColors c, String text) => Padding(
    padding: const EdgeInsets.only(top: 16, bottom: 4),
    child: Semantics(header: true, child: _label(c, text)),
  );

  Widget _label(ChurrColors c, String text) => Text(
    text.toUpperCase(),
    style: TextStyle(
      fontSize: 11,
      fontWeight: FontWeight.w700,
      letterSpacing: 1,
      color: c.dim,
    ),
  );

  Widget _code(ChurrColors c, String text) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
    color: c.raised,
    child: Text(
      text,
      style: TextStyle(fontFamily: fontMono, fontSize: 12.5, color: c.ink),
    ),
  );

  Widget _fact(ChurrColors c, String label, String text, {double top = 6}) =>
      Padding(
        padding: EdgeInsets.only(top: top),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [_label(c, label), Text(text)],
        ),
      );

  Widget _para(String text, {Color? color}) => Padding(
    padding: const EdgeInsets.fromLTRB(16, 0, 16, 4),
    child: Text(text, style: TextStyle(color: color)),
  );

  /// Two things side by side on a wide screen, one above the other on a
  /// narrow one.
  Widget _pair(Widget a, Widget b) => LayoutBuilder(
    builder: (context, box) => box.maxWidth < 420
        ? Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [a, const SizedBox(height: 2), b],
          )
        : Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ConstrainedBox(
                constraints: BoxConstraints(maxWidth: box.maxWidth * 0.4),
                child: a,
              ),
              const SizedBox(width: 12),
              Expanded(child: b),
            ],
          ),
  );
}
