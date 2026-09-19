# Syntax

## Files and declarations

A program is one `.bc` file. At file level it holds `struct`, `error`, `state` and `def`
declarations, in any order: a body may use a struct, a function or an error declared further
down. Nothing else stands at file level: no import, no constant, no statement.

A name is made of ASCII letters, digits and `_`, starts with a letter or `_`, and is case
sensitive. The keywords are reserved: `struct def external state if elif else while return
true false and or not ref own new none take error try catch`.

A comment runs from `//` to the end of the line. `///` is a comment like any other; the
corpus uses it as the header of a program and above each function.

## Lines and blocks

One statement per line, no separator. A block opens with `:` at the end of a line and holds
the lines below it that are indented deeper, with spaces only. The first line of a block sets
its width and every line of the block uses it; a line that leaves the block returns to the
width of an enclosing block, exactly. A block holds at least one statement. Blank lines and
lines holding only a comment are ignored, so they never affect indentation.

## Literals

| Literal | Type |
|---|---|
| `42` | `int`, or `uint` when the context asks for one (see [types.md](types.md)). Decimal digits only: no sign, no `0x`, no `_`. It must fit the type. |
| `true`, `false` | `bool` |
| `none` | the empty value of an `own T?` |
| `error.Name` | the declared error `Name`, of type `error` |

A negative number is `-42`, the unary minus applied to `42`, which makes it an `int`.

## Operators

From the loosest to the tightest:

| Level | Operators | Notes |
|---|---|---|
| 1 | `a or b` | bools; both sides are always evaluated |
| 2 | `a and b` | bools; both sides are always evaluated |
| 3 | `not a` | bool |
| 4 | `== != < <= > >=` | left to right, so `a < b < c` compares a bool with a number and is refused |
| 5 | `+ -` | left to right |
| 6 | `* / %` | left to right |
| 7 | `-a`, `ref place`, `try call`, `take place` | prefix |
| 8 | `f(a, b)`, `T(a, b)`, `x.field` | postfix, chained freely |
| 9 | literals, names, `new T(a, b)`, `(expression)` | |

`and` and `or` do not short-circuit: `b != 0 and a / b > 1` divides even when `b` is zero,
and faults. Guard with an `if`. Where `ref`, `try` and `take` may appear is a typing rule:
[functions.md](functions.md), [errors.md](errors.md), [ownership.md](ownership.md).

## Statements

| Statement | Meaning |
|---|---|
| `T x = expression` | declares `x`, initialised; the type is written, never inferred |
| `T a, U b = f(...)` | declares one name per value of a call that returns several |
| `ref T x = ref place` | binds `x` to a place, once and for all ([functions.md](functions.md)) |
| `T x = expression or:` + block | binds an optional own, or leaves ([ownership.md](ownership.md)) |
| `x = expression`, `x.f.g = expression` | assigns a variable or a field; nothing else can be assigned |
| `f(...)`, `try f(...)`, `f(...) catch err:` + block | a call for its effects; it must return nothing |
| `if c:` / `elif c:` / `else:` | arms in that order; `elif` arms and `else` are optional |
| `if ref T x = ref place:`, `if own T x = expression:` | an arm that binds an optional own ([ownership.md](ownership.md)) |
| `while c:` | the only loop |
| `return`, `return a, b`, `return error.Name`, `return err` | leaves the function ([functions.md](functions.md), [errors.md](errors.md)) |

A condition is a `bool`. A declaration lives until the end of its block, and a name may be
declared again in an inner block, where it hides the outer one.

## Refused

| Message | Cause |
|---|---|
| `tabs are not allowed, use spaces` | a tab anywhere in a line |
| `unindent does not match any outer indentation level` | a line dedented to a width no open block has |
| `unexpected indentation` | a line indented deeper than its block without a `:` opening one |
| `expected an indented block` | a `:` with no indented line under it |
| `unexpected token at the end of the line` | anything after a complete statement, including `0x10`, `1_000` or a second statement |
| `unexpected character '@'` | a character that is no token |
| `expected 'struct', 'error', 'state' or 'def'` | a statement at file level |
| `expected an expression` | an operator or a `(` with nothing after it |
| `only a variable or a field can be assigned` | `f() = 1`, `Point(1, 2) = p` |
| `expected '='` | a line that is neither a call, an assignment nor a declaration, such as a bare `x + 1` |
| `expected ':'` | a header line without its colon |

Any other missing or misplaced token is reported as `expected ...` at the token where the
parser stopped, and the rest of the line is skipped so that one mistake costs one message.
