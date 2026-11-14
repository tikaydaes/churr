import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import '../../../core/calls/call_layout.dart';
import '../../../core/calls/call_media.dart';
import '../../../core/calls/call_model.dart';
import '../../../core/calls/call_session.dart';
import '../../../core/calls/ringing.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/calls/webrtc_media.dart';
import '../../../state/call_service.dart';
import '../../../state/channel_detail.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../../widgets/avatar.dart';
import 'call_icons.dart';
import 'call_pickers.dart';
import 'call_video.dart';

/// Whether this device can share a screen. Desktops can (a picker for a
/// screen or a window). Phones cannot in this build: the iPhone needs a
/// broadcast extension, and Android a media-projection foreground service
/// (README.md, "Calls").
bool get canShareScreen =>
    !kIsWeb && (Platform.isMacOS || Platform.isWindows || Platform.isLinux);

bool get isPhone => !kIsWeb && (Platform.isIOS || Platform.isAndroid);

/// The call screen (docs/CLIENTS.md 9.14): tiles, the active speaker, the
/// person's own preview, the controls, and why a call ended.
class CallScreen extends StatefulWidget {
  const CallScreen({super.key, required this.service});
  final CallService service;

  @override
  State<CallScreen> createState() => _CallScreenState();
}

class _CallScreenState extends State<CallScreen> with WidgetsBindingObserver {
  LayoutMode _mode = LayoutMode.grid;
  String? _pinned;
  bool _speakerphone = true;
  bool _people = false;
  final Set<String> _mutedForMe = {};

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // On a phone in the background: no video, camera muted (CALLS.md 11).
    if (!isPhone) return;
    final s = widget.service.active?.session;
    s?.setBackground(state != AppLifecycleState.resumed);
  }

  @override
  Widget build(BuildContext context) {
    final a = widget.service.active;
    if (a == null) return const SizedBox.shrink();
    final media = a.media;
    return ListenableBuilder(
      listenable: Listenable.merge([
        widget.service,
        if (media is Listenable) media as Listenable,
      ]),
      builder: (context, _) => _build(context, a),
    );
  }

  Widget _build(BuildContext context, ActiveCall a) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final s = a.session;
    final conv = a.server.conversation(a.conversationId);
    final title = conv == null ? '' : channelTitle(a.server, conv);
    if (s.ended != null && s.ended != CallEnd.left) {
      return _Ended(
        session: s,
        server: a.server,
        onClose: widget.service.dismiss,
      );
    }
    return Material(
      color: c.bg,
      child: SafeArea(
        child: Column(
          children: [
            _TopBar(
              title: title,
              session: s,
              onShrink: () => widget.service.setExpanded(false),
              onPeople: () => setState(() => _people = !_people),
            ),
            if (s.notice != null)
              _NoticeLine(notice: s.notice!, server: a.server),
            if (s.call?.hostedBy != null)
              _InfoLine(t.callHostedElsewhere(s.call!.hostedBy!)),
            Expanded(
              child: Row(
                children: [
                  Expanded(child: _stage(context, a)),
                  if (_people)
                    SizedBox(
                      width: 260,
                      child: _PeopleList(
                        active: a,
                        mutedForMe: _mutedForMe,
                        onMuteForMe: _toggleMuteForMe,
                      ),
                    ),
                ],
              ),
            ),
            _Controls(
              session: s,
              media: a.media,
              speakerphone: _speakerphone,
              mode: _mode,
              onMode: (m) => setState(() => _mode = m),
              onSpeakerphone: (on) {
                setState(() => _speakerphone = on);
                final m = a.media;
                if (m is WebRtcMedia) unawaited(m.setSpeakerphone(on));
              },
              onLeave: widget.service.leave,
            ),
          ],
        ),
      ),
    );
  }

  void _toggleMuteForMe(ActiveCall a, String accountId) {
    final mute = !_mutedForMe.contains(accountId);
    setState(
      () => mute ? _mutedForMe.add(accountId) : _mutedForMe.remove(accountId),
    );
    final m = a.media;
    if (m is WebRtcMedia) {
      for (final r in m.remote.values) {
        if (r.accountId == accountId && r.track.kind == 'audio') {
          r.track.enabled = !mute;
        }
      }
    }
  }

  Widget _stage(BuildContext context, ActiveCall a) {
    final s = a.session;
    final me = a.server.me?.id ?? '';
    final tiles = callTiles(s, me);
    final ringing = _ringingList(a);
    return LayoutBuilder(
      builder: (context, box) {
        final layout = layoutTiles(
          tiles: tiles,
          width: box.maxWidth,
          height: box.maxHeight,
          preferred: _mode,
          pinned: _pinned,
          speaker: s.speakers.firstOrNull,
        );
        final subs = subscriptionsFor(
          layout,
          devicePixelRatio: MediaQuery.devicePixelRatioOf(context),
          max: s.limits.maxSubscriptions,
        );
        SchedulerBinding.instance.addPostFrameCallback((_) {
          if (s.ended == null) s.setSubscriptions(subs);
        });
        return Stack(
          children: [
            for (final p in layout.placements)
              Positioned(
                left: p.box.x,
                top: p.box.y,
                width: p.box.w,
                height: p.box.h,
                child: p.tile == null
                    ? _MoreTile(count: p.more)
                    : CallTile(
                        key: ValueKey(p.tile!.key),
                        tile: p.tile!,
                        server: a.server,
                        session: s,
                        media: a.media,
                        pinned: _pinned == p.tile!.key,
                        mutedForMe: _mutedForMe.contains(p.tile!.accountId),
                        onPin: () => setState(
                          () => _pinned = _pinned == p.tile!.key
                              ? null
                              : p.tile!.key,
                        ),
                        onMuteForMe: p.tile!.self
                            ? null
                            : () => _toggleMuteForMe(a, p.tile!.accountId),
                      ),
              ),
            if (ringing.isNotEmpty)
              Positioned(
                left: 12,
                top: 12,
                child: _RingingCard(server: a.server, accounts: ringing),
              ),
          ],
        );
      },
    );
  }

  /// Who is being rung while the caller is alone (CALLS.md 15: the caller
  /// sees who is rung, and a do not disturb mark on anyone who is not).
  List<String> _ringingList(ActiveCall a) {
    final s = a.session;
    final conv = a.server.conversation(a.conversationId);
    final c = s.call;
    if (conv == null || c == null) return const [];
    if (!kindRings(sealed: conv.isSealed, named: conv.isNamed)) return const [];
    final me = a.server.me?.id;
    if (c.startedById != me || c.participants.length > 1) return const [];
    if (DateTime.now().difference(c.startedAt ?? DateTime.now()) >
        const Duration(seconds: 45)) {
      return const [];
    }
    return [
      for (final id in conv.memberIds)
        if (id != me && !s.declined.contains(id)) id,
    ];
  }
}

/// The tiles of a call: every participant's camera (or avatar), every
/// shared screen, and the person's own preview.
List<TileSpec> callTiles(CallSession s, String me) {
  final out = <TileSpec>[];
  final ps = s.call?.participants ?? const <CallParticipant>[];
  if (!ps.any((p) => p.accountId == me)) {
    out.add(TileSpec(accountId: me, self: true, videoOn: s.cameraOn));
  }
  for (final p in ps) {
    if (p.accountId == me) {
      out.add(TileSpec(accountId: me, self: true, videoOn: s.cameraOn));
      if (s.screenOn) {
        out.add(
          TileSpec(
            accountId: me,
            self: true,
            source: TrackSource.screen,
            videoOn: true,
          ),
        );
      }
      continue;
    }
    final cam = p.track(TrackSource.camera);
    out.add(
      TileSpec(
        accountId: p.accountId,
        trackId: cam?.id,
        videoOn: cam != null && !cam.muted,
      ),
    );
    final scr = p.track(TrackSource.screen);
    if (scr != null) {
      out.add(
        TileSpec(
          accountId: p.accountId,
          source: TrackSource.screen,
          trackId: scr.id,
          videoOn: true,
        ),
      );
    }
  }
  return out;
}

class _TopBar extends StatelessWidget {
  const _TopBar({
    required this.title,
    required this.session,
    required this.onShrink,
    required this.onPeople,
  });
  final String title;
  final CallSession session;
  final VoidCallback onShrink;
  final VoidCallback onPeople;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final s = session;
    final phase = switch (s.phase) {
      CallPhase.joining => t.callJoining,
      CallPhase.connecting => t.callConnecting,
      CallPhase.reconnecting => t.callReconnecting,
      _ => s.signalLost ? t.callReconnecting : null,
    };
    final count = s.call?.participants.length ?? 0;
    return Container(
      color: c.panel,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      child: Row(
        children: [
          IconButton(
            key: const Key('call-shrink'),
            tooltip: t.callMinimize,
            icon: Glyph(CallIcons.shrink, color: c.dim),
            onPressed: onShrink,
          ),
          const SizedBox(width: 4),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: ChurrType.title(c.ink, size: 16),
                ),
                Text(
                  phase ?? t.callInCallNow(count),
                  key: const Key('call-phase'),
                  style: monoStyle(context, size: 10.5),
                ),
              ],
            ),
          ),
          if (s.call?.sealed ?? false)
            Semantics(
              label: t.callE2E,
              excludeSemantics: true,
              child: Row(
                children: [
                  Glyph(ChurrIcons.locked, size: 14, color: c.lavender),
                  const SizedBox(width: 4),
                  Text(t.callE2E, style: monoStyle(context, size: 10.5)),
                ],
              ),
            ),
          IconButton(
            key: const Key('call-people'),
            tooltip: t.callPeople,
            icon: Glyph(ChurrIcons.people, color: c.dim),
            onPressed: onPeople,
          ),
        ],
      ),
    );
  }
}

/// One tile: video, or the avatar when there is none; the name; a muted
/// mark (a crossed microphone on a filled square, not only a colour); a
/// speaking outline with a bars mark beside the name.
class CallTile extends StatelessWidget {
  const CallTile({
    super.key,
    required this.tile,
    required this.server,
    required this.session,
    required this.media,
    this.pinned = false,
    this.mutedForMe = false,
    this.onPin,
    this.onMuteForMe,
  });
  final TileSpec tile;
  final ServerState server;
  final CallSession session;
  final CallMedia media;
  final bool pinned;
  final bool mutedForMe;
  final VoidCallback? onPin;
  final VoidCallback? onMuteForMe;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final s = session;
    final account = server.accounts[tile.accountId];
    final name = tile.self ? t.callYou : (account?.name ?? t.callSomeone);
    final speaking =
        s.speakers.contains(tile.accountId) &&
        tile.source == TrackSource.camera;
    final micMuted = tile.self
        ? !s.micOn
        : (s.call?.participants
                  .where((p) => p.accountId == tile.accountId)
                  .firstOrNull
                  ?.track(TrackSource.microphone)
                  ?.muted ??
              true);
    final fwd = tile.trackId == null ? null : s.forwarding[tile.trackId];
    final paused = fwd?.state == TrackState.paused;
    final undecryptable = s.undecryptable.contains(tile.accountId);
    Widget? video;
    if (tile.videoOn && !paused && !undecryptable) {
      video = tile.self
          ? CallVideo.local(media, screen: tile.source == TrackSource.screen)
          : CallVideo.remote(
              media,
              tile.trackId!,
              cover: tile.source == TrackSource.camera,
            );
    }
    final label = [
      name,
      if (tile.source == TrackSource.screen) t.callScreenOf(name),
      if (micMuted) t.callMutedMark,
      if (speaking) t.callSpeakingMark,
      if (paused) t.callVideoPaused,
      if (pinned) t.callPinned,
    ].join(', ');
    return Semantics(
      label: label,
      container: true,
      child: GestureDetector(
        onDoubleTap: onPin,
        onSecondaryTapUp: (d) => _menu(context, d.globalPosition),
        onLongPressStart: (d) => _menu(context, d.globalPosition),
        child: Container(
          decoration: BoxDecoration(
            color: c.raised,
            border: speaking
                ? Border.all(color: c.cyan, width: 3)
                : pinned
                ? Border.all(color: c.lavender, width: 2)
                : null,
          ),
          child: Stack(
            fit: StackFit.expand,
            children: [
              if (video != null)
                ClipRect(child: video)
              else
                Center(
                  child: Avatar(account: account, server: server, size: 64),
                ),
              if (paused || undecryptable)
                Center(
                  child: Container(
                    color: c.panel,
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 4,
                    ),
                    child: Text(
                      undecryptable ? t.callCannotDecrypt : t.callVideoPaused,
                      style: monoStyle(context, size: 11, color: c.ink),
                    ),
                  ),
                ),
              Positioned(
                left: 6,
                bottom: 6,
                right: 6,
                child: Row(
                  children: [
                    if (micMuted || mutedForMe)
                      Container(
                        key: Key('muted-${tile.accountId}'),
                        color: c.pink,
                        padding: const EdgeInsets.all(2),
                        margin: const EdgeInsets.only(right: 4),
                        child: Glyph(
                          CallIcons.microphoneOff,
                          size: 14,
                          color: c.onPink,
                        ),
                      ),
                    if (speaking)
                      Container(
                        key: Key('speaking-${tile.accountId}'),
                        color: c.cyan,
                        padding: const EdgeInsets.all(2),
                        margin: const EdgeInsets.only(right: 4),
                        child: Glyph(
                          CallIcons.speaking,
                          size: 14,
                          color: c.onCyan,
                        ),
                      ),
                    Flexible(
                      child: Container(
                        color: c.panel,
                        padding: const EdgeInsets.symmetric(
                          horizontal: 6,
                          vertical: 2,
                        ),
                        child: Text(
                          tile.source == TrackSource.screen
                              ? t.callScreenOf(name)
                              : name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(color: c.ink, fontSize: 12),
                        ),
                      ),
                    ),
                    if (pinned) ...[
                      const SizedBox(width: 4),
                      Glyph(ChurrIcons.pinOn, size: 14, color: c.lavender),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _menu(BuildContext context, Offset at) async {
    final t = L10n.of(context);
    final v = await showMenu<String>(
      context: context,
      position: RelativeRect.fromLTRB(at.dx, at.dy, at.dx, at.dy),
      popUpAnimationStyle: AnimationStyle.noAnimation,
      items: [
        PopupMenuItem(
          value: 'pin',
          child: Text(pinned ? t.callUnpin : t.callPin),
        ),
        if (onMuteForMe != null)
          PopupMenuItem(
            value: 'mute',
            child: Text(mutedForMe ? t.callUnmuteForMe : t.callMuteForMe),
          ),
      ],
    );
    if (v == 'pin') onPin?.call();
    if (v == 'mute') onMuteForMe?.call();
  }
}

class _MoreTile extends StatelessWidget {
  const _MoreTile({required this.count});
  final int count;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      color: c.raised,
      alignment: Alignment.center,
      child: Text(
        L10n.of(context).callMore(count),
        style: ChurrType.title(c.ink, size: 18),
      ),
    );
  }
}

class _Controls extends StatelessWidget {
  const _Controls({
    required this.session,
    required this.media,
    required this.speakerphone,
    required this.mode,
    required this.onMode,
    required this.onSpeakerphone,
    required this.onLeave,
  });
  final CallSession session;
  final CallMedia media;
  final bool speakerphone;
  final LayoutMode mode;
  final ValueChanged<LayoutMode> onMode;
  final ValueChanged<bool> onSpeakerphone;
  final VoidCallback onLeave;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final s = session;
    final compact =
        WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
    final buttons = <Widget>[
      _ControlButton(
        key: const Key('call-mic'),
        icon: s.micOn ? CallIcons.microphone : CallIcons.microphoneOff,
        label: s.micOn ? t.callMuteMic : t.callUnmuteMic,
        off: !s.micOn,
        onPressed: () => s.setMicrophone(!s.micOn),
      ),
      _ControlButton(
        key: const Key('call-camera'),
        icon: s.cameraOn ? CallIcons.camera : CallIcons.cameraOff,
        label: s.cameraOn ? t.callCameraOff : t.callCameraOn,
        off: !s.cameraOn,
        onPressed: () => s.setCamera(!s.cameraOn),
      ),
      if (canShareScreen)
        _ControlButton(
          key: const Key('call-screen'),
          icon: s.screenOn ? CallIcons.screenStop : CallIcons.screen,
          label: s.screenOn ? t.callStopShare : t.callShareScreen,
          on: s.screenOn,
          onPressed: () async {
            if (s.screenOn) {
              await s.setScreen(false);
              return;
            }
            final m = media;
            if (m is WebRtcMedia) {
              final id = await pickScreen(context);
              if (id == null) return;
              m.screenSourceId = id;
            }
            await s.setScreen(true);
          },
        ),
      if (isPhone && s.cameraOn)
        _ControlButton(
          key: const Key('call-switch-camera'),
          icon: CallIcons.switchCamera,
          label: t.callSwitchCamera,
          onPressed: () {
            final m = media;
            if (m is WebRtcMedia) unawaited(m.switchCamera());
          },
        ),
      if (isPhone)
        _ControlButton(
          key: const Key('call-speaker'),
          icon: speakerphone ? CallIcons.speaker : CallIcons.earpiece,
          label: speakerphone ? t.callEarpiece : t.callSpeaker,
          onPressed: () => onSpeakerphone(!speakerphone),
        )
      else
        _ControlButton(
          key: const Key('call-devices'),
          icon: ChurrIcons.devices,
          label: t.callDevices,
          onPressed: () {
            final m = media;
            if (m is WebRtcMedia) unawaited(showDeviceChoice(context, m));
          },
        ),
      if (!compact)
        _ControlButton(
          key: const Key('call-layout'),
          icon: mode == LayoutMode.grid
              ? CallIcons.speakerLayout
              : CallIcons.grid,
          label: mode == LayoutMode.grid
              ? t.callLayoutSpeaker
              : t.callLayoutGrid,
          onPressed: () => onMode(
            mode == LayoutMode.grid ? LayoutMode.speaker : LayoutMode.grid,
          ),
        ),
      _ControlButton(
        key: const Key('call-leave'),
        icon: CallIcons.leave,
        label: t.callLeave,
        danger: true,
        onPressed: onLeave,
      ),
    ];
    return Container(
      color: c.panel,
      padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 8),
      child: Wrap(
        alignment: WrapAlignment.center,
        spacing: 8,
        runSpacing: 8,
        children: buttons,
      ),
    );
  }
}

class _ControlButton extends StatelessWidget {
  const _ControlButton({
    super.key,
    required this.icon,
    required this.label,
    required this.onPressed,
    this.off = false,
    this.on = false,
    this.danger = false,
  });
  final ChurrGlyph icon;
  final String label;
  final VoidCallback onPressed;

  /// Drawn on a pink fill: the microphone or camera is off.
  final bool off;

  /// Drawn on a cyan fill: sharing.
  final bool on;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final (bg, fg) = danger || off
        ? (c.pink, c.onPink)
        : on
        ? (c.cyan, c.onCyan)
        : (c.raised, c.ink);
    return Tooltip(
      message: label,
      child: Semantics(
        button: true,
        label: label,
        excludeSemantics: true,
        child: Material(
          color: bg,
          child: InkWell(
            onTap: onPressed,
            child: SizedBox(
              width: 52,
              height: 48,
              child: Center(child: Glyph(icon, color: fg)),
            ),
          ),
        ),
      ),
    );
  }
}

class _NoticeLine extends StatelessWidget {
  const _NoticeLine({required this.notice, required this.server});
  final CallNotice notice;
  final ServerState server;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final who = notice.accountId == null
        ? t.callSomeone
        : (server.accounts[notice.accountId!]?.name ?? t.callSomeone);
    final text = switch (notice.code) {
      'cameras_full' => t.callCamerasFull,
      'screen_in_use' => t.callScreenInUse(who),
      'declined' => t.callDeclined(who),
      'cannot_decrypt' => t.callCannotDecrypt,
      _ => t.callProblem(notice.code),
    };
    return _InfoLine(text, key: const Key('call-notice'));
  }
}

class _InfoLine extends StatelessWidget {
  const _InfoLine(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      width: double.infinity,
      color: c.yellow,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Text(text, style: TextStyle(color: c.onYellow, fontSize: 13)),
    );
  }
}

class _RingingCard extends StatelessWidget {
  const _RingingCard({required this.server, required this.accounts});
  final ServerState server;
  final List<String> accounts;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    return Container(
      color: c.panel,
      padding: const EdgeInsets.all(8),
      constraints: const BoxConstraints(maxWidth: 260),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final id in accounts.take(6))
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Row(
                children: [
                  Avatar(
                    account: server.accounts[id],
                    server: server,
                    size: 20,
                  ),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      server.accounts[id]?.name ?? t.callSomeone,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: c.ink, fontSize: 13),
                    ),
                  ),
                  if (server.accounts[id]?.dnd ?? false) ...[
                    Glyph(ChurrIcons.dnd, size: 14, color: c.dim),
                    const SizedBox(width: 4),
                    Text(t.callDnd, style: monoStyle(context, size: 10)),
                  ] else ...[
                    Glyph(ChurrIcons.notifyActive, size: 14, color: c.dim),
                    const SizedBox(width: 4),
                    Text(t.callRinging, style: monoStyle(context, size: 10)),
                  ],
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _PeopleList extends StatelessWidget {
  const _PeopleList({
    required this.active,
    required this.mutedForMe,
    required this.onMuteForMe,
  });
  final ActiveCall active;
  final Set<String> mutedForMe;
  final void Function(ActiveCall, String) onMuteForMe;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final server = active.server;
    final me = server.me?.id;
    final ps = active.session.call?.participants ?? const <CallParticipant>[];
    return ColoredBox(
      color: c.panel,
      child: ListView(
        children: [
          for (final p in ps)
            ListTile(
              dense: true,
              leading: Avatar(
                account: server.accounts[p.accountId],
                server: server,
                size: 28,
              ),
              title: Text(
                p.accountId == me
                    ? t.callYou
                    : server.accounts[p.accountId]?.name ?? t.callSomeone,
              ),
              trailing: p.accountId == me
                  ? null
                  : IconButton(
                      tooltip: mutedForMe.contains(p.accountId)
                          ? t.callUnmuteForMe
                          : t.callMuteForMe,
                      icon: Glyph(
                        mutedForMe.contains(p.accountId)
                            ? CallIcons.microphoneOff
                            : CallIcons.microphone,
                        color: c.dim,
                        size: 18,
                      ),
                      onPressed: () => onMuteForMe(active, p.accountId),
                    ),
            ),
        ],
      ),
    );
  }
}

/// Why the call ended, with a way to close.
class _Ended extends StatelessWidget {
  const _Ended({
    required this.session,
    required this.server,
    required this.onClose,
  });
  final CallSession session;
  final ServerState server;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final code = session.endCode ?? '';
    final text = switch (session.ended!) {
      CallEnd.joinedElsewhere => t.callEndedElsewhere,
      CallEnd.removed => t.callEndedRemoved,
      CallEnd.signedOut => t.callEndedSignedOut,
      CallEnd.timeout => t.callEndedTimeout,
      CallEnd.callEnded => t.callEndedEnded,
      CallEnd.serverRestarted => t.callEndedServer,
      CallEnd.networkBlocks => t.callEndedNetwork,
      CallEnd.sealedUnavailable => t.callSealedUnavailable,
      CallEnd.refused => switch (code) {
        'call_full' || 'conflict' => t.callEndedFull,
        'feature_off' => t.callEndedOff,
        _ => t.callEndedRefused(code),
      },
      CallEnd.failed || CallEnd.left => t.callEndedFailed(code),
    };
    return Material(
      color: c.bg,
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                text,
                key: const Key('call-ended'),
                textAlign: TextAlign.center,
                style: ChurrType.title(c.ink, size: 18),
              ),
              const SizedBox(height: 16),
              FilledButton(
                key: const Key('call-close'),
                onPressed: onClose,
                child: Text(t.close),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
