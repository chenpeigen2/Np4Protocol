import 'package:flutter/material.dart';

/// NP4 visual language: a deep-space dark surface with a single emerald
/// accent. One accent, quiet neutrals — a privacy tool should feel calm and
/// precise, not decorated.
class Np4Colors {
  static const bg = Color(0xFF0A0F1C);
  static const surface = Color(0xFF121A2C);
  static const surfaceHigh = Color(0xFF1A2338);
  static const border = Color(0xFF202C47);

  static const accent = Color(0xFF34D399);
  static const accentContainer = Color(0xFF12301F);
  static const onAccent = Color(0xFF052018);

  static const textPrimary = Color(0xFFE9EDF5);
  static const textMuted = Color(0xFF8B94AB);
  static const textFaint = Color(0xFF5A6378);

  static const warn = Color(0xFFF3B94D);
  static const warnContainer = Color(0xFF322913);
  static const danger = Color(0xFFF0716E);
}

ThemeData buildNp4Theme() {
  const scheme = ColorScheme.dark(
    primary: Np4Colors.accent,
    onPrimary: Np4Colors.onAccent,
    primaryContainer: Np4Colors.accentContainer,
    onPrimaryContainer: Np4Colors.accent,
    secondary: Np4Colors.accent,
    surface: Np4Colors.surface,
    onSurface: Np4Colors.textPrimary,
    surfaceContainerHighest: Np4Colors.surfaceHigh,
    onSurfaceVariant: Np4Colors.textMuted,
    error: Np4Colors.danger,
  );

  final base = ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: Np4Colors.bg,
    fontFamilyFallback: const ['PingFang SC', 'Noto Sans SC', 'Microsoft YaHei'],
  );

  return base.copyWith(
    appBarTheme: const AppBarTheme(
      backgroundColor: Np4Colors.bg,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      centerTitle: false,
      titleTextStyle: TextStyle(
        color: Np4Colors.textPrimary,
        fontSize: 18,
        fontWeight: FontWeight.w600,
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Np4Colors.surfaceHigh,
      hintStyle: const TextStyle(color: Np4Colors.textFaint),
      labelStyle: const TextStyle(color: Np4Colors.textMuted),
      helperStyle: const TextStyle(color: Np4Colors.textFaint, fontSize: 11.5),
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: const BorderSide(color: Np4Colors.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: const BorderSide(color: Np4Colors.border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: const BorderSide(color: Np4Colors.accent, width: 1.4),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: Np4Colors.accent,
        foregroundColor: Np4Colors.onAccent,
        minimumSize: const Size.fromHeight(52),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
      ),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: Np4Colors.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: const BorderSide(color: Np4Colors.border),
      ),
      titleTextStyle: const TextStyle(
        color: Np4Colors.textPrimary,
        fontSize: 17,
        fontWeight: FontWeight.w600,
      ),
      contentTextStyle: const TextStyle(
        color: Np4Colors.textMuted,
        fontSize: 14.5,
        height: 1.5,
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: Np4Colors.surfaceHigh,
      contentTextStyle: const TextStyle(color: Np4Colors.textPrimary),
      behavior: SnackBarBehavior.floating,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: Np4Colors.surface,
      modalBackgroundColor: Np4Colors.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      showDragHandle: true,
    ),
    dividerTheme: const DividerThemeData(color: Np4Colors.border, thickness: 1),
  );
}
