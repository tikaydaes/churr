import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/calls/call_model.dart';
import '../../../core/models/message.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/call_service.dart';
import '../../../state/channel_detail.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../message/message_slots.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../../widgets/avatar.dart';
import '../conversation/conversation_screen.dart';
import 'call_icons.dart';
import 'call_screen.dart';
import 'call_video.dart';

bool _registered = false;

/// Puts the call button in the conversation header and the Join button
/// on a call's system message. Called once by [CallOverlay].
void registerCallWidgets() {
  if (_registered) return;
  _registered = true;
  ConversationHeaderSlots.actions.add(
    (context, server, id) =>
        CallHeaderButton(server: server, conversationId: id),
  );
  MessageSlots.system = (context, server, message) {
    final kind = message.system?['kind'];
    if (kind != 'call_started' && kind != 'call_ended') return null;
    return CallSystemLine(server: server, message: message);
  };
}

/// Wraps the shell: the call screen over it while a call fills the
/// window, the compact bar above it while the person uses the rest of
/// the app (with a floating tile on wide windows), and the incoming-call
/// prompt.
class CallOverlay extends StatefulWidget {
  const CallOverlay({super.key, required this.app, required this.child});
  final AppState app;
  final Widget child;

  @override
  State<CallOverlay> createState() => _CallOverlayState();
}

class _CallOverlayState extends State<CallOverlay> {
  @override
  void initState() {
    super.initState();
    registerCallWidgets();
  }

  @override
  Widget build(BuildContext context) {
    final calls = CallService.of(widget.app);
    return ListenableBuilder(
      listenable: Listenable.merge([calls, widget.app]),
      builder: (context, _) {
        calls.attachAll();
        final a = calls.active;
        final showScreen = a != null && calls.expanded;
        final showBar = a != null && !calls.expanded;
        final wide =
            WidthClass.of(MediaQuery.sizeOf(context).width) !=
            WidthClass.compact;
        return Column(
          children: [
            if (showBar) CallBar(service: calls),
            Expanded(
              child: Stack(
                fit: StackFit.expand,
                children: [
                  // The shell stays built under the call screen, so
                  // shrinking the call puts the person back where they were.
                  Offstage(offstage: showScreen, child: widget.child),
                  if (showScreen)
                    Positioned.fill(child: CallScreen(service: calls)),
                  if (showBar && wide && a.session.ended == null)
                    _FloatingTile(service: calls),
                  if (calls.incoming != null)
                    Positioned(
                      top: 8,
                      right: 8,
                      left: wide ? null : 8,
                      child: IncomingCallPrompt(service: calls),
                    ),
                ],
              ),
            ),
          ],
        );
      },
    );
  }
}

/// The compact bar that keeps the call going while the person uses the
/// rest of the app.
class CallBar extends StatelessWidget {
  const CallBar({super.key, required this.service});
  final CallService service;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final a = service.active!;
    final s = a.session;
    final conv = a.server.conversation(a.conversationId);
    final title = conv == null ? '' : channelTitle(a.server, conv);
    final ended = s.ended != null;
    return Material(
      color: c.cyan,
      child: SafeArea(
        bottom: false,
        child: InkWell(
          key: const Key('call-bar'),
          onTap: () => service.setExpanded(true),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            child: Row(
              children: [
                Glyph(CallIcons.camera, size: 16, color: c.onCyan),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    ended
                        ? t.callEndedEnded
                        : t.callBarTitle(
                            title,
                            s.call?.participants.length ?? 0,
                          ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      color: c.onCyan,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                if (!ended)
                  IconButton(
                    key: const Key('call-bar-mic'),
                    tooltip: s.micOn ? t.callMuteMic : t.callUnmuteMic,
                    icon: Glyph(
                      s.micOn ? CallIcons.microphone : CallIcons.microphoneOff,
                      color: c.onCyan,
                    ),
                    onPressed: () => s.setMicrophone(!s.micOn),
                  ),
                IconButton(
                  tooltip: t.callExpand,
                  icon: Glyph(CallIcons.expand, color: c.onCyan),
                  onPressed: () => service.setExpanded(true),
                ),
                IconButton(
                  key: const Key('call-bar-leave'),
                  tooltip: ended ? t.close : t.callLeave,
                  icon: Glyph(
                    ended ? ChurrIcons.close : CallIcons.leave,
                    color: c.onCyan,
                  ),
                  onPressed: ended ? service.dismiss : service.leave,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// A small tile of the loudest speaker (or whoever is sharing) in a
/// corner while the call is shrunk on a wide window. Dragging moves it.
class _FloatingTile extends StatefulWidget {
  const _FloatingTile({required this.service});
  final CallService service;

  @override
  State<_FloatingTile> createState() => _FloatingTileState();
}

class _FloatingTileState extends State<_FloatingTile> {
  Offset _at = const Offset(16, 16);

  @override
  Widget build(BuildContext context) {
    final a = widget.service.active!;
    final s = a.session;
    final me = a.server.me?.id;
    final ps = s.call?.participants ?? const <CallParticipant>[];
    final others = [
      for (final p in ps)
        if (p.accountId != me) p,
    ];
    if (others.isEmpty) return const SizedBox.shrink();
    final screen = others
        .where((p) => p.track(TrackSource.screen) != null)
        .firstOrNull;
    final speaker = others
        .where((p) => s.speakers.contains(p.accountId))
        .firstOrNull;
    final who = screen ?? speaker ?? others.first;
    final track = who.track(
      screen != null ? TrackSource.screen : TrackSource.camera,
    );
    final c = ChurrColors.of(context);
    final video = track == null || track.muted
        ? null
        : CallVideo.remote(a.media, track.id, cover: screen == null);
    return Positioned(
      right: _at.dx,
      bottom: _at.dy,
      width: 240,
      height: 135,
      child: GestureDetector(
        onPanUpdate: (d) => setState(
          () => _at = Offset(_at.dx - d.delta.dx, _at.dy - d.delta.dy),
        ),
        onTap: () => widget.service.setExpanded(true),
        child: Container(
          key: const Key('call-floating'),
          decoration: BoxDecoration(
            color: c.raised,
            border: Border.all(color: c.edge),
          ),
          child:
              video ??
              Center(
                child: Avatar(
                  account: a.server.accounts[who.accountId],
                  server: a.server,
                  size: 48,
                ),
              ),
        ),
      ),
    );
  }
}

/// The incoming-call prompt: the caller, the conversation, Join and
/// Decline.
class IncomingCallPrompt extends StatelessWidget {
  const IncomingCallPrompt({super.key, required this.service});
  final CallService service;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final i = service.incoming!;
    final s = i.server;
    final caller = i.fromAccountId == null
        ? null
        : s.accounts[i.fromAccountId!];
    final conv = s.conversation(i.conversationId);
    return Semantics(
      liveRegion: true,
      container: true,
      child: Material(
        color: c.panel,
        child: Container(
          key: const Key('incoming-call'),
          width: 340,
          decoration: BoxDecoration(
            border: Border.all(color: c.lavender, width: 2),
          ),
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  Avatar(account: caller, server: s, size: 36),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          t.callIncomingFrom(caller?.name ?? t.callSomeone),
                          style: ChurrType.title(c.ink, size: 15),
                        ),
                        if (conv != null)
                          Text(
                            channelTitle(s, conv),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: monoStyle(context, size: 11),
                          ),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton(
                    key: const Key('incoming-join'),
                    onPressed: () => _answer(context, camera: false),
                    child: Text(t.callAnswer),
                  ),
                  OutlinedButton(
                    key: const Key('incoming-join-video'),
                    onPressed: () => _answer(context, camera: true),
                    child: Text(t.callAnswerVideo),
                  ),
                  TextButton(
                    key: const Key('incoming-decline'),
                    onPressed: service.decline,
                    child: Text(t.callDecline),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _answer(BuildContext context, {required bool camera}) async {
    final t = L10n.of(context);
    final messenger = ScaffoldMessenger.maybeOf(context);
    final r = await service.answer(camera: camera);
    if (r == JoinRefusal.sealedNoKeys) {
      messenger?.showSnackBar(SnackBar(content: Text(t.callSealedNoKeys)));
    }
  }
}

/// Starts or joins a conversation's call, after the pre-join choices.
Future<void> startOrJoin(
  BuildContext context,
  ServerState server,
  String conversationId,
) async {
  final t = L10n.of(context);
  final messenger = ScaffoldMessenger.maybeOf(context);
  final calls = CallService.of(AppScope.read(context));
  if (calls.isActive(server, conversationId)) {
    calls.setExpanded(true);
    return;
  }
  final call = calls.liveCall(server, conversationId);
  final choice = await showDialog<(bool, bool)>(
    context: context,
    animationStyle: AnimationStyle.noAnimation,
    builder: (_) => _PreJoin(hostedBy: call?.hostedBy),
  );
  if (choice == null) return;
  final r = await calls.join(
    server,
    conversationId,
    microphone: choice.$1,
    camera: choice.$2,
  );
  if (r == JoinRefusal.sealedNoKeys) {
    messenger?.showSnackBar(SnackBar(content: Text(t.callSealedNoKeys)));
  }
}

/// Before joining: microphone and camera on or off. (Device choice is in
/// the call's Devices control.)
class _PreJoin extends StatefulWidget {
  const _PreJoin({this.hostedBy});
  final String? hostedBy;

  @override
  State<_PreJoin> createState() => _PreJoinState();
}

class _PreJoinState extends State<_PreJoin> {
  bool _mic = true;
  bool _camera = false;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return AlertDialog(
      title: Text(t.callPreJoin),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (widget.hostedBy != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(t.callHostedElsewhere(widget.hostedBy!)),
            ),
          SwitchListTile(
            key: const Key('prejoin-mic'),
            title: Text(t.callWithMic),
            value: _mic,
            onChanged: (v) => setState(() => _mic = v),
          ),
          SwitchListTile(
            key: const Key('prejoin-camera'),
            title: Text(t.callWithCamera),
            value: _camera,
            onChanged: (v) => setState(() => _camera = v),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text(t.cancel),
        ),
        FilledButton(
          key: const Key('prejoin-join'),
          onPressed: () => Navigator.of(context).pop((_mic, _camera)),
          child: Text(t.callAnswer),
        ),
      ],
    );
  }
}

/// The header's call button: "Start a call", or "Join" with how many are
/// in it, or "In the call" for the one this device is in.
class CallHeaderButton extends StatefulWidget {
  const CallHeaderButton({
    super.key,
    required this.server,
    required this.conversationId,
  });
  final ServerState server;
  final String conversationId;

  @override
  State<CallHeaderButton> createState() => _CallHeaderButtonState();
}

class _CallHeaderButtonState extends State<CallHeaderButton> {
  @override
  void initState() {
    super.initState();
    final app = AppScope.read(context);
    unawaited(
      CallService.of(app).refresh(widget.server, widget.conversationId),
    );
  }

  @override
  Widget build(BuildContext context) {
    final calls = CallService.of(AppScope.read(context));
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final conv = widget.server.conversation(widget.conversationId);
    final level = conv?.membership?.level;
    final canJoin =
        conv != null &&
        !conv.isArchived &&
        level != null &&
        level != 'reader' &&
        (widget.server.info?.features['calls'] ?? true);
    return ListenableBuilder(
      listenable: calls,
      builder: (context, _) {
        final call = calls.liveCall(widget.server, widget.conversationId);
        final mine = calls.isActive(widget.server, widget.conversationId);
        if (call == null) {
          if (!canJoin) return const SizedBox.shrink();
          return IconButton(
            key: const Key('header-call'),
            tooltip: t.callStart,
            icon: Glyph(CallIcons.camera, color: c.dim),
            onPressed: () =>
                startOrJoin(context, widget.server, widget.conversationId),
          );
        }
        final n = call.participants.length;
        final narrow =
            WidthClass.of(MediaQuery.sizeOf(context).width) ==
            WidthClass.compact;
        if (narrow) {
          // No room for words in a phone's header: the camera on a fill,
          // with the words as its tooltip and label.
          return Tooltip(
            message: mine ? t.callInIt : t.callJoinCount(n),
            child: Semantics(
              button: true,
              label: mine ? t.callInIt : t.callJoinCount(n),
              excludeSemantics: true,
              child: Material(
                color: mine ? c.cyan : c.lavender,
                child: InkWell(
                  key: const Key('header-call'),
                  onTap: canJoin || mine
                      ? () => startOrJoin(
                          context,
                          widget.server,
                          widget.conversationId,
                        )
                      : null,
                  child: SizedBox(
                    width: 44,
                    height: 44,
                    child: Center(
                      child: Glyph(
                        CallIcons.camera,
                        size: 18,
                        color: mine ? c.onCyan : c.onLavender,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          );
        }
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 4),
          child: Material(
            color: mine ? c.cyan : c.lavender,
            child: InkWell(
              key: const Key('header-call'),
              onTap: canJoin || mine
                  ? () => startOrJoin(
                      context,
                      widget.server,
                      widget.conversationId,
                    )
                  : null,
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 8,
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Glyph(
                      CallIcons.camera,
                      size: 16,
                      color: mine ? c.onCyan : c.onLavender,
                    ),
                    const SizedBox(width: 6),
                    Text(
                      mine ? t.callInIt : t.callJoinCount(n),
                      style: TextStyle(
                        color: mine ? c.onCyan : c.onLavender,
                        fontWeight: FontWeight.w700,
                        fontSize: 13,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

/// A call's system message: "Sam started a call" with Join while it is
/// live, then "Call, 21 minutes, 4 people", or "Missed call from Sam" for
/// someone who was rung and did not join (CALLS.md section 2).
class CallSystemLine extends StatelessWidget {
  const CallSystemLine({
    super.key,
    required this.server,
    required this.message,
  });
  final ServerState server;
  final Message message;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final sys = message.system ?? const {};
    final who = server.accounts[message.authorId]?.name ?? t.callSomeone;
    final me = server.me?.id;
    final calls = CallService.of(AppScope.read(context));
    return ListenableBuilder(
      listenable: calls,
      builder: (context, _) {
        final live = calls.liveCall(server, message.conversationId);
        final isLive =
            sys['kind'] == 'call_started' &&
            (live == null || live.id == sys['call_id']);
        final String text;
        if (isLive) {
          text = t.callStartedBy(who);
        } else {
          final ids = [
            for (final x in (sys['participant_ids'] as List? ?? const [])) '$x',
          ];
          final conv = server.conversation(message.conversationId);
          final rang =
              conv != null &&
              (conv.isSealed || conv.isAdHoc) &&
              message.authorId != me;
          final secs = sys['duration_seconds'];
          if (rang && me != null && ids.isNotEmpty && !ids.contains(me)) {
            text = t.callMissedFrom(who);
          } else {
            text = t.callEndedSummary(
              secs is num ? (secs / 60).round() : 0,
              ids.length,
            );
          }
        }
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
          child: Row(
            children: [
              Glyph(CallIcons.camera, size: 14, color: c.dim),
              const SizedBox(width: 8),
              Flexible(
                child: Text(
                  text,
                  key: Key('call-system-${message.id}'),
                  style: monoStyle(context, size: 12),
                ),
              ),
              if (isLive && live != null) ...[
                const SizedBox(width: 10),
                TextButton(
                  key: Key('call-system-join-${message.id}'),
                  onPressed: () =>
                      startOrJoin(context, server, message.conversationId),
                  child: Text(
                    calls.isActive(server, message.conversationId)
                        ? t.callInIt
                        : t.callAnswer,
                  ),
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}
