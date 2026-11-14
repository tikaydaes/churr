// A highlight.js grammar for Zig, written for Churr.
//
// highlight.js ships no Zig grammar, and docs/MESSAGE-FORMAT.md section 2.3
// lists `zig` among the languages every client highlights. This is a small
// one in highlight.js's own grammar format: keywords, types, literals,
// builtins (`@name`), strings, numbers and comments. It is Churr's own
// code, under the project's licence, and not part of the vendored files.

export default function zig(hljs) {
  const keyword = [
    'addrspace', 'align', 'allowzero', 'and', 'anyframe', 'anytype', 'asm', 'async', 'await', 'break', 'callconv', 'catch',
    'comptime', 'const', 'continue', 'defer', 'else', 'enum', 'errdefer', 'error', 'export', 'extern', 'fn', 'for', 'if',
    'inline', 'linksection', 'noalias', 'noinline', 'nosuspend', 'opaque', 'or', 'orelse', 'packed', 'pub', 'resume',
    'return', 'struct', 'suspend', 'switch', 'test', 'threadlocal', 'try', 'union', 'unreachable', 'usingnamespace', 'var',
    'volatile', 'while',
  ];
  const type = [
    'i8', 'u8', 'i16', 'u16', 'i32', 'u32', 'i64', 'u64', 'i128', 'u128', 'isize', 'usize', 'c_char', 'c_short', 'c_ushort',
    'c_int', 'c_uint', 'c_long', 'c_ulong', 'c_longlong', 'c_ulonglong', 'c_longdouble', 'f16', 'f32', 'f64', 'f80', 'f128',
    'bool', 'anyopaque', 'void', 'noreturn', 'type', 'anyerror', 'comptime_int', 'comptime_float',
  ];
  const literal = ['true', 'false', 'null', 'undefined'];
  return {
    name: 'Zig',
    keywords: { keyword, type, literal },
    contains: [
      hljs.COMMENT('//', '$'),
      { scope: 'string', begin: '"', end: '"', illegal: '\\n', contains: [hljs.BACKSLASH_ESCAPE] },
      { scope: 'string', begin: "'", end: "'", illegal: '\\n', contains: [hljs.BACKSLASH_ESCAPE] },
      // A multi-line string is a run of lines that each start with `\\`.
      { scope: 'string', begin: /\\\\/, end: /$/ },
      { scope: 'built_in', begin: /@[A-Za-z_][A-Za-z0-9_]*/ },
      { scope: 'number', begin: /\b(0x[0-9a-fA-F_]+(\.[0-9a-fA-F_]+)?([pP][+-]?[0-9_]+)?|0o[0-7_]+|0b[01_]+|[0-9][0-9_]*(\.[0-9_]+)?([eE][+-]?[0-9_]+)?)\b/, relevance: 0 },
      { begin: [/\bfn/, /\s+/, /[A-Za-z_][A-Za-z0-9_]*/], beginScope: { 1: 'keyword', 3: 'title.function' } },
      // By convention a name that starts with a capital is a type.
      { scope: 'type', begin: /\b[A-Z][A-Za-z0-9_]*\b/, relevance: 0 },
    ],
  };
}
