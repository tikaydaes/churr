/// The width classes of docs/CLIENTS.md section 10. The shell arranges
/// the same screens by available width, not by platform: a resized
/// desktop window and a tablet in split view follow the same rules.
enum WidthClass {
  /// Under 600: one pane at a time.
  compact,

  /// 600 to 1023: sidebar and conversation.
  medium,

  /// 1024 to 1399: sidebar and conversation; a thread is a third pane
  /// and the sidebar narrows to make room.
  expanded,

  /// 1400 and over: everything at full width.
  wide;

  /// The class of a width in logical pixels.
  static WidthClass of(double width) => width < 600
      ? compact
      : width < 1024
      ? medium
      : width < 1400
      ? expanded
      : wide;
}

/// The rail of the server-rail layout is 64 wide beside a 280-wide list.
const railWidth = 64.0;
const railListWidth = 280.0;

/// The one column of the merged and hybrid layouts on a medium width.
const mediumSidebarWidth = 300.0;

/// A conversation's text column stops growing here, so lines stay
/// readable on a wide monitor.
const maxTextColumn = 900.0;
