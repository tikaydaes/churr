import 'package:flutter/material.dart';

import '../../core/themes/theme.dart';
import 'churr_icons.dart';
import 'motion.dart';
import 'shapes.dart';
import 'tokens.dart';

/// Builds the Material theme for one brightness, palette and contrast
/// setting, from the approved style guide (docs/style-guide.html).
///
/// Flat colour, no rounded corner, no blur, no gradient. Areas are told
/// apart by fill steps (ground, panel, raised), not by lines. Anything
/// that floats wears the hard offset block. Every Material widget the app
/// uses is squared and flattened here, once, so screens do not restyle
/// them.
///
/// [theme], when given, is the theme to draw with (a saved theme, a
/// server's default, one being edited or tried); otherwise the built-in
/// theme for [dark] and [palette].
ThemeData buildTheme({
  required bool dark,
  Palette palette = Palette.standard,
  bool highContrast = false,
  ChurrTheme? theme,
}) {
  final c = theme != null
      ? ChurrColors.forTheme(theme, highContrast: highContrast)
      : ChurrColors.resolve(
          dark: dark,
          palette: palette,
          highContrast: highContrast,
        );
  dark = c.dark;
  const square = RoundedRectangleBorder();
  final edge = BorderSide(color: c.edge, width: 2);
  final floating = HardShadowBorder(
    side: BorderSide(color: c.ink, width: 2),
    block: c.lavender,
  );
  final base = ThemeData(
    useMaterial3: true,
    brightness: dark ? Brightness.dark : Brightness.light,
    fontFamily: fontText,
    splashFactory: NoSplash.splashFactory,
    materialTapTargetSize: MaterialTapTargetSize.padded,
  );
  final text = _textTheme(base.textTheme, c);
  bool on(Set<WidgetState> s, WidgetState x) => s.contains(x);

  return base.copyWith(
    extensions: [c],
    scaffoldBackgroundColor: c.bg,
    // Dropdown menus draw on the canvas colour; one fill step up from
    // the panel they open over.
    canvasColor: c.raised,
    cardColor: c.panel,
    dividerColor: c.line,
    hoverColor: c.raised,
    highlightColor: Colors.transparent,
    splashColor: Colors.transparent,
    focusColor: c.raised,
    shadowColor: Colors.transparent,
    colorScheme: ColorScheme(
      brightness: dark ? Brightness.dark : Brightness.light,
      primary: c.cyan,
      onPrimary: c.onCyan,
      secondary: c.lavender,
      onSecondary: c.onLavender,
      tertiary: c.yellow,
      onTertiary: c.onYellow,
      error: c.pink,
      onError: c.onPink,
      surface: c.panel,
      onSurface: c.ink,
      onSurfaceVariant: c.dim,
      surfaceContainerLowest: c.bg,
      surfaceContainerLow: c.panel,
      surfaceContainer: c.panel,
      surfaceContainerHigh: c.raised,
      surfaceContainerHighest: c.raised,
      outline: c.edge,
      outlineVariant: c.line,
      surfaceTint: Colors.transparent,
      shadow: Colors.transparent,
      scrim: Colors.black54,
    ),
    textTheme: text,
    primaryTextTheme: text,
    iconTheme: IconThemeData(color: c.dim, size: 18),
    dividerTheme: DividerThemeData(color: c.line, thickness: 2, space: 2),
    textSelectionTheme: TextSelectionThemeData(
      selectionColor: c.pink.withValues(alpha: 0.45),
      cursorColor: c.cyan,
      selectionHandleColor: c.cyan,
    ),
    pageTransitionsTheme: PageTransitionsTheme(
      builders: {
        for (final p in TargetPlatform.values)
          p: const CutPageTransitionsBuilder(),
      },
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: c.panel,
      foregroundColor: c.ink,
      elevation: 0,
      scrolledUnderElevation: 0,
      shape: const Border(),
      iconTheme: IconThemeData(color: c.dim, size: 20),
      titleTextStyle: ChurrType.title(c.ink),
    ),
    actionIconTheme: ActionIconThemeData(
      backButtonIconBuilder: (_) => const Glyph(ChurrIcons.arrowLeft, size: 20),
      closeButtonIconBuilder: (_) => const Glyph(ChurrIcons.close, size: 20),
      drawerButtonIconBuilder: (_) => const Glyph(ChurrIcons.moreV, size: 20),
      endDrawerButtonIconBuilder: (_) =>
          const Glyph(ChurrIcons.moreV, size: 20),
    ),
    // The main action: a cyan block with the notch. It turns pink on
    // hover and nudges 1px, as if pressed. Drawn in [backgroundBuilder]
    // so the block itself moves.
    filledButtonTheme: FilledButtonThemeData(style: primaryButton(c)),
    // Secondary actions: a quiet raised block with a line under it; the
    // line turns cyan on hover and yellow with the keyboard's focus.
    outlinedButtonTheme: OutlinedButtonThemeData(style: _quiet(c, text)),
    textButtonTheme: TextButtonThemeData(style: _quiet(c, text)),
    elevatedButtonTheme: ElevatedButtonThemeData(style: _quiet(c, text)),
    iconButtonTheme: IconButtonThemeData(
      style: ButtonStyle(
        foregroundColor: WidgetStateProperty.resolveWith(
          (s) => on(s, WidgetState.disabled)
              ? c.faint
              : on(s, WidgetState.selected)
              ? c.cyan
              : on(s, WidgetState.hovered)
              ? c.ink
              : c.dim,
        ),
        backgroundColor: WidgetStateProperty.resolveWith(
          (s) => on(s, WidgetState.hovered) ? c.raised : Colors.transparent,
        ),
        overlayColor: const WidgetStatePropertyAll(Colors.transparent),
        shape: const WidgetStatePropertyAll(square),
        side: WidgetStateProperty.resolveWith(
          (s) => on(s, WidgetState.focused)
              ? BorderSide(color: c.focus, width: 3)
              : BorderSide.none,
        ),
        iconSize: const WidgetStatePropertyAll(20),
        animationDuration: Duration.zero,
      ),
    ),
    floatingActionButtonTheme: FloatingActionButtonThemeData(
      backgroundColor: c.cyan,
      foregroundColor: c.onCyan,
      hoverColor: c.pink,
      elevation: 0,
      focusElevation: 0,
      hoverElevation: 0,
      highlightElevation: 0,
      shape: const CutCornerBorder(cut: 8),
    ),
    // Fields: ground inside, a 2px edge, a thicker yellow line with focus.
    inputDecorationTheme: InputDecorationTheme(
      isDense: true,
      filled: true,
      fillColor: c.bg,
      hoverColor: Colors.transparent,
      hintStyle: TextStyle(color: c.faint),
      labelStyle: ChurrType.label(c.dim, size: 12),
      floatingLabelStyle: ChurrType.label(c.dim, size: 12),
      helperStyle: TextStyle(color: c.dim, fontSize: 12),
      errorStyle: TextStyle(
        color: c.pink,
        fontSize: 12,
        fontWeight: FontWeight.w600,
      ),
      prefixIconColor: c.dim,
      suffixIconColor: c.dim,
      contentPadding: const EdgeInsets.symmetric(horizontal: 10, vertical: 12),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.zero,
        borderSide: edge,
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.zero,
        borderSide: edge,
      ),
      disabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.zero,
        borderSide: BorderSide(color: c.line, width: 2),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.zero,
        borderSide: BorderSide(color: c.focus, width: 3),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.zero,
        borderSide: BorderSide(color: c.pink, width: 2),
      ),
      focusedErrorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.zero,
        borderSide: BorderSide(color: c.pink, width: 3),
      ),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: c.panel,
      elevation: 0,
      shape: floating,
      titleTextStyle: ChurrType.title(c.ink),
      contentTextStyle: TextStyle(color: c.ink, fontSize: 15, height: 1.45),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: c.panel,
      elevation: 0,
      shape: floating,
      menuPadding: const EdgeInsets.symmetric(vertical: 4),
      textStyle: TextStyle(color: c.ink, fontFamily: fontText, fontSize: 14),
      labelTextStyle: WidgetStatePropertyAll(
        TextStyle(color: c.ink, fontFamily: fontText, fontSize: 14),
      ),
    ),
    menuTheme: MenuThemeData(
      style: MenuStyle(
        backgroundColor: WidgetStatePropertyAll(c.panel),
        elevation: const WidgetStatePropertyAll(0),
        shape: WidgetStatePropertyAll(floating),
        shadowColor: const WidgetStatePropertyAll(Colors.transparent),
      ),
    ),
    dropdownMenuTheme: DropdownMenuThemeData(
      menuStyle: MenuStyle(
        backgroundColor: WidgetStatePropertyAll(c.panel),
        elevation: const WidgetStatePropertyAll(0),
        shape: WidgetStatePropertyAll(floating),
      ),
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: c.panel,
      modalBackgroundColor: c.panel,
      elevation: 0,
      modalElevation: 0,
      shape: Border(top: BorderSide(color: c.ink, width: 2)),
      showDragHandle: false,
    ),
    drawerTheme: DrawerThemeData(
      backgroundColor: c.panel,
      elevation: 0,
      shape: square,
      endShape: square,
    ),
    cardTheme: CardThemeData(
      color: c.panel,
      elevation: 0,
      shape: square,
      margin: EdgeInsets.zero,
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(color: c.ink),
      textStyle: TextStyle(
        color: c.bg,
        fontFamily: fontText,
        fontSize: 12,
        fontWeight: FontWeight.w600,
      ),
      waitDuration: const Duration(milliseconds: 500),
      exitDuration: Duration.zero,
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: c.panel,
      contentTextStyle: TextStyle(
        color: c.ink,
        fontFamily: fontText,
        fontSize: 14,
      ),
      actionTextColor: c.cyan,
      shape: floating,
      elevation: 0,
      behavior: SnackBarBehavior.floating,
    ),
    bannerTheme: MaterialBannerThemeData(
      backgroundColor: c.raised,
      elevation: 0,
    ),
    listTileTheme: ListTileThemeData(
      shape: square,
      iconColor: c.dim,
      textColor: c.ink,
      selectedColor: c.ink,
      selectedTileColor: c.raised,
      subtitleTextStyle: TextStyle(color: c.dim, fontSize: 13),
    ),
    tabBarTheme: TabBarThemeData(
      indicator: UnderlineTabIndicator(
        borderSide: BorderSide(color: c.cyan, width: 4),
        borderRadius: BorderRadius.zero,
      ),
      indicatorSize: TabBarIndicatorSize.tab,
      dividerColor: Colors.transparent,
      dividerHeight: 0,
      labelColor: c.ink,
      unselectedLabelColor: c.dim,
      labelStyle: ChurrType.label(c.ink, size: 12),
      unselectedLabelStyle: ChurrType.label(c.dim, size: 12),
      overlayColor: WidgetStateProperty.resolveWith(
        (s) => on(s, WidgetState.hovered) ? c.raised : Colors.transparent,
      ),
      splashFactory: NoSplash.splashFactory,
    ),
    segmentedButtonTheme: SegmentedButtonThemeData(
      selectedIcon: const Glyph(ChurrIcons.check, size: 16),
      style: ButtonStyle(
        shape: const WidgetStatePropertyAll(square),
        side: WidgetStatePropertyAll(BorderSide(color: c.edge, width: 2)),
        backgroundColor: WidgetStateProperty.resolveWith(
          (s) => on(s, WidgetState.selected) ? c.cyan : c.raised,
        ),
        foregroundColor: WidgetStateProperty.resolveWith(
          (s) => on(s, WidgetState.selected) ? c.onCyan : c.ink,
        ),
        overlayColor: const WidgetStatePropertyAll(Colors.transparent),
        textStyle: WidgetStatePropertyAll(
          archivo(size: 13, weight: 700, width: 85),
        ),
        animationDuration: Duration.zero,
      ),
    ),
    // Material's switch is a pill; the app draws its own (ChurrSwitch).
    // These colours are for any that slip through.
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith(
        (s) => on(s, WidgetState.selected) ? c.cyan : c.faint,
      ),
      trackColor: WidgetStatePropertyAll(c.bg),
      trackOutlineColor: WidgetStateProperty.resolveWith(
        (s) => on(s, WidgetState.selected) ? c.cyan : c.edge,
      ),
      thumbIcon: const WidgetStatePropertyAll(null),
    ),
    checkboxTheme: CheckboxThemeData(
      shape: square,
      side: BorderSide(color: c.edge, width: 2),
      fillColor: WidgetStateProperty.resolveWith(
        (s) => on(s, WidgetState.selected) ? c.cyan : Colors.transparent,
      ),
      checkColor: WidgetStatePropertyAll(c.onCyan),
      overlayColor: const WidgetStatePropertyAll(Colors.transparent),
      splashRadius: 0,
    ),
    radioTheme: RadioThemeData(
      fillColor: WidgetStateProperty.resolveWith(
        (s) => on(s, WidgetState.selected) ? c.cyan : c.edge,
      ),
      overlayColor: const WidgetStatePropertyAll(Colors.transparent),
    ),
    chipTheme: ChipThemeData(
      shape: square,
      side: BorderSide(color: c.edge, width: 2),
      backgroundColor: c.raised,
      selectedColor: c.cyan,
      disabledColor: c.panel,
      checkmarkColor: c.onCyan,
      deleteIconColor: c.dim,
      labelStyle: TextStyle(
        color: c.ink,
        fontSize: 13,
        fontWeight: FontWeight.w600,
      ),
      secondaryLabelStyle: TextStyle(
        color: c.onCyan,
        fontSize: 13,
        fontWeight: FontWeight.w700,
      ),
      elevation: 0,
      pressElevation: 0,
      showCheckmark: true,
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
    ),
    sliderTheme: SliderThemeData(
      activeTrackColor: c.cyan,
      inactiveTrackColor: c.line,
      thumbColor: c.cyan,
      overlayColor: Colors.transparent,
      trackHeight: 4,
      trackShape: const RectangularSliderTrackShape(),
      thumbShape: const SquareThumbShape(),
      overlayShape: SliderComponentShape.noOverlay,
      valueIndicatorShape: const RectangularSliderValueIndicatorShape(),
      valueIndicatorColor: c.ink,
      valueIndicatorTextStyle: TextStyle(
        color: c.bg,
        fontFamily: fontMono,
        fontSize: 11,
      ),
      tickMarkShape: SliderTickMarkShape.noTickMark,
    ),
    scrollbarTheme: ScrollbarThemeData(
      radius: Radius.zero,
      thickness: const WidgetStatePropertyAll(6),
      thumbColor: WidgetStatePropertyAll(c.edge),
      trackColor: const WidgetStatePropertyAll(Colors.transparent),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(
      color: c.cyan,
      linearTrackColor: c.raised,
      linearMinHeight: 4,
      borderRadius: BorderRadius.zero,
      stopIndicatorColor: Colors.transparent,
      trackGap: 0,
    ),
    badgeTheme: BadgeThemeData(
      backgroundColor: c.yellow,
      textColor: c.onYellow,
    ),
  );
}

/// The main action: a block with the notch, cyan unless [fill] says
/// otherwise (lavender for "join", pink for destructive actions). It turns
/// pink on hover and nudges 1px, as if pressed. The block is drawn in
/// `backgroundBuilder` so that it moves with the nudge.
ButtonStyle primaryButton(ChurrColors c, {Color? fill}) {
  bool on(Set<WidgetState> s, WidgetState x) => s.contains(x);
  final base = fill ?? c.cyan;
  bool hot(Set<WidgetState> s) =>
      !on(s, WidgetState.disabled) &&
      (on(s, WidgetState.hovered) || on(s, WidgetState.pressed));
  return ButtonStyle(
    backgroundColor: const WidgetStatePropertyAll(Colors.transparent),
    foregroundColor: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.disabled)
          ? c.faint
          : (hot(s) ? c.onPink : c.on(base)),
    ),
    iconColor: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.disabled)
          ? c.faint
          : (hot(s) ? c.onPink : c.on(base)),
    ),
    overlayColor: const WidgetStatePropertyAll(Colors.transparent),
    elevation: const WidgetStatePropertyAll(0),
    shape: const WidgetStatePropertyAll(CutCornerBorder(cut: 7)),
    padding: const WidgetStatePropertyAll(
      EdgeInsets.symmetric(horizontal: 16, vertical: 12),
    ),
    minimumSize: const WidgetStatePropertyAll(Size(48, 44)),
    textStyle: WidgetStatePropertyAll(ChurrType.button(c.on(base))),
    animationDuration: Duration.zero,
    backgroundBuilder: (context, states, child) {
      final color = on(states, WidgetState.disabled)
          ? c.raised
          : (hot(states) ? c.pink : base);
      Widget block = DecoratedBox(
        decoration: ShapeDecoration(
          color: color,
          shape: const CutCornerBorder(cut: 7),
        ),
        child: child,
      );
      if (on(states, WidgetState.focused)) {
        block = DecoratedBox(
          decoration: ShapeDecoration(
            shape: CutCornerBorder(
              cut: 7,
              side: BorderSide(color: c.focus, width: 3),
            ),
          ),
          position: DecorationPosition.foreground,
          child: block,
        );
      }
      return Transform.translate(
        offset: hot(states) && !Motion.reduced(context)
            ? const Offset(1, 1)
            : Offset.zero,
        child: block,
      );
    },
  );
}

/// The destructive main action: the pink block.
ButtonStyle dangerButton(ChurrColors c) => primaryButton(c, fill: c.pink);

/// The quiet secondary block.
ButtonStyle _quiet(ChurrColors c, TextTheme text) {
  bool on(Set<WidgetState> s, WidgetState x) => s.contains(x);
  return ButtonStyle(
    backgroundColor: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.disabled) ? c.panel : c.raised,
    ),
    foregroundColor: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.disabled) ? c.faint : c.ink,
    ),
    iconColor: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.disabled) ? c.faint : c.ink,
    ),
    overlayColor: const WidgetStatePropertyAll(Colors.transparent),
    elevation: const WidgetStatePropertyAll(0),
    shape: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.focused)
          ? const UnderEdgeBorder(allRound: true)
          : const UnderEdgeBorder(),
    ),
    side: WidgetStateProperty.resolveWith(
      (s) => on(s, WidgetState.focused)
          ? BorderSide(color: c.focus, width: 3)
          : on(s, WidgetState.hovered)
          ? BorderSide(color: c.cyan, width: 2)
          : BorderSide(color: c.edge, width: 2),
    ),
    padding: const WidgetStatePropertyAll(
      EdgeInsets.symmetric(horizontal: 14, vertical: 11),
    ),
    minimumSize: const WidgetStatePropertyAll(Size(44, 40)),
    textStyle: WidgetStatePropertyAll(archivo(size: 14, weight: 700)),
    animationDuration: Duration.zero,
  );
}

/// A square block whose side is drawn only along its bottom edge (the
/// quiet button's underline), or all the way round for the keyboard's
/// focus outline.
class UnderEdgeBorder extends OutlinedBorder {
  const UnderEdgeBorder({super.side, this.allRound = false});
  final bool allRound;

  @override
  EdgeInsetsGeometry get dimensions => EdgeInsets.only(bottom: side.width);

  @override
  Path getInnerPath(Rect rect, {TextDirection? textDirection}) =>
      Path()..addRect(rect);

  @override
  Path getOuterPath(Rect rect, {TextDirection? textDirection}) =>
      Path()..addRect(rect);

  @override
  void paint(Canvas canvas, Rect rect, {TextDirection? textDirection}) {
    if (side.style == BorderStyle.none || side.width == 0) return;
    final p = Paint()..color = side.color;
    if (allRound) {
      // Focus: an outline all round.
      canvas.drawRect(
        rect.deflate(side.width / 2),
        p
          ..style = PaintingStyle.stroke
          ..strokeWidth = side.width,
      );
    } else {
      canvas.drawRect(
        Rect.fromLTRB(
          rect.left,
          rect.bottom - side.width,
          rect.right,
          rect.bottom,
        ),
        p,
      );
    }
  }

  @override
  ShapeBorder scale(double t) =>
      UnderEdgeBorder(side: side.scale(t), allRound: allRound);

  @override
  UnderEdgeBorder copyWith({BorderSide? side}) =>
      UnderEdgeBorder(side: side ?? this.side, allRound: allRound);

  @override
  bool operator ==(Object other) =>
      other is UnderEdgeBorder &&
      other.side == side &&
      other.allRound == allRound;

  @override
  int get hashCode => Object.hash(side, allRound);
}

/// A square slider thumb.
class SquareThumbShape extends SliderComponentShape {
  const SquareThumbShape();

  @override
  Size getPreferredSize(bool isEnabled, bool isDiscrete) => const Size(16, 16);

  @override
  void paint(
    PaintingContext context,
    Offset center, {
    required Animation<double> activationAnimation,
    required Animation<double> enableAnimation,
    required bool isDiscrete,
    required TextPainter labelPainter,
    required RenderBox parentBox,
    required SliderThemeData sliderTheme,
    required TextDirection textDirection,
    required double value,
    required double textScaleFactor,
    required Size sizeWithOverflow,
  }) {
    context.canvas.drawRect(
      Rect.fromCenter(center: center, width: 16, height: 16),
      Paint()..color = sliderTheme.thumbColor!,
    );
  }
}

TextTheme _textTheme(TextTheme base, ChurrColors c) {
  final t = base.apply(
    bodyColor: c.ink,
    displayColor: c.ink,
    fontFamily: fontText,
  );
  TextStyle? heavy(TextStyle? s, double weight, double width) =>
      s?.merge(archivo(weight: weight, width: width));
  return t.copyWith(
    displayLarge: heavy(t.displayLarge, 900, 70),
    displayMedium: heavy(t.displayMedium, 900, 70),
    displaySmall: heavy(t.displaySmall, 900, 70),
    headlineLarge: heavy(t.headlineLarge, 900, 75),
    headlineMedium: heavy(t.headlineMedium, 900, 75),
    headlineSmall: heavy(t.headlineSmall, 800, 85),
    titleLarge: heavy(t.titleLarge, 800, 85),
    titleMedium: heavy(t.titleMedium, 800, 85),
    titleSmall: heavy(t.titleSmall, 700, 85),
    labelLarge: heavy(t.labelLarge, 800, 85),
    bodyLarge: t.bodyLarge?.copyWith(fontSize: 16, height: 1.5),
    bodyMedium: t.bodyMedium?.copyWith(fontSize: 14, height: 1.4),
    bodySmall: t.bodySmall?.copyWith(color: c.dim),
  );
}
