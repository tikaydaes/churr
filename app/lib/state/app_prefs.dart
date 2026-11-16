import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../core/db/app_db.dart';
import '../core/models/conversation.dart';

/// The two sidebar layouts (docs/CLIENTS.md 9.1, ruling R47). How the
/// conversations of each server are grouped and ordered is the person's
/// own and kept by that server (ruling R83).
enum SidebarLayout { rail, merged }

/// The colour palette (docs/style-guide.html): the standard four role
/// colours, or high distinction for colour vision.
enum Palette { standard, highDistinction }

/// Dark is the default; "system" follows the device.
enum ThemeChoice { dark, light, system }

/// The app preferences of docs/CLIENTS.md section 8.2: choices that
/// belong to the app on this device and to no server. Kept in `app.db`;
/// a change is written there and listeners are told at once.
class AppPrefs extends ChangeNotifier {
  AppPrefs(this._db);
  final AppDb _db;
  Map<String, String> _v = {};

  /// Reads everything from `app.db`. Called once at start.
  Future<void> load() async {
    _v = await _db.prefs();
    notifyListeners();
  }

  Future<void> _put(String key, String value) async {
    _v[key] = value;
    notifyListeners();
    await _db.putPref(key, value);
  }

  /// A preference by key, for areas that keep their own (see
  /// `lib/state/account_prefs.dart`). Null when never set.
  String? raw(String key) => _v[key];

  /// Writes a preference by key; listeners are told at once.
  Future<void> putRaw(String key, String value) => _put(key, value);

  T _enum<T extends Enum>(String key, List<T> values, T fallback) =>
      values.firstWhere((e) => e.name == _v[key], orElse: () => fallback);

  /// The rail is the layout out of the box.
  SidebarLayout get layout =>
      _enum('layout', SidebarLayout.values, SidebarLayout.rail);
  set layout(SidebarLayout v) => _put('layout', v.name);

  /// The default for `show`, used by every conversation whose own is not
  /// set: one of always, activity, notification. Never hidden.
  ShowSetting get defaultShow {
    final s = _enum('default_show', ShowSetting.values, ShowSetting.always);
    return s == ShowSetting.hidden ? ShowSetting.always : s;
  }

  set defaultShow(ShowSetting v) => _put('default_show', v.name);

  bool get showHidden => _v['show_hidden'] == '1';
  set showHidden(bool v) => _put('show_hidden', v ? '1' : '0');

  ThemeChoice get theme => _enum('theme', ThemeChoice.values, ThemeChoice.dark);
  set theme(ThemeChoice v) => _put('theme', v.name);

  /// The colour palette: the standard four, or the high-distinction set
  /// (pink to orange, lavender to blue) for colour vision.
  Palette get palette => _enum('palette', Palette.values, Palette.standard);
  set palette(Palette v) => _put('palette', v.name);

  /// A multiplier on top of the system text size.
  double get textScale => double.tryParse(_v['text_scale'] ?? '') ?? 1.0;
  set textScale(double v) => _put('text_scale', '$v');

  /// Whether Enter sends (and Shift+Enter makes a new line). On by
  /// default; phones send with the button either way.
  bool get enterSends => _v['enter_sends'] != '0';
  set enterSends(bool v) => _put('enter_sends', v ? '1' : '0');

  /// The sidebar's width where it can be dragged (280 to 360).
  double get sidebarWidth =>
      (double.tryParse(_v['sidebar_width'] ?? '') ?? 300).clamp(280, 360);
  set sidebarWidth(double v) => _put('sidebar_width', '${v.clamp(280, 360)}');

  /// Things folded away on this device: server headings (merged layout),
  /// groups, and parents whose sub-channels are hidden. Keys are
  /// `<server local id>/<group key or conversation id>`.
  Set<String> get collapsed => AppDb.decodeList(_v['collapsed']).toSet();

  bool isCollapsed(String key) => collapsed.contains(key);

  void setCollapsed(String key, bool value) {
    final next = collapsed;
    value ? next.add(key) : next.remove(key);
    _put('collapsed', jsonEncode(next.toList()));
  }

  /// The system measurements are shown in beside the original (ruling
  /// R73): `metric`, `imperial`, `off`, or '' for the device's region.
  String get measureSystem => _v['measure_system'] ?? '';
  set measureSystem(String v) => _put('measure_system', v);

  /// The time zones added to the time converter (ruling R69), in the
  /// order added. Kept on this device only.
  List<String> get instantZones => AppDb.decodeList(_v['instant_zones']);
  set instantZones(List<String> v) => _put('instant_zones', jsonEncode(v));
}
