# Syntax

BlueCode borrows its surface from the scripting languages most people already read: blocks
by indentation, one statement per line, words for the logical operators. The choice is
deliberate and the rest of this reference assumes it, so this chapter states the lexical and
grammatical rules exactly, including the ones a reader would guess and the few they would
not.

## A program is one file

A program is a single `.bc` file. There is no import, no module system and no preprocessor:
everything the program uses is declared in the file, and the file compiles on its own into
one module ([modules.md](modules.md)). The language is unlikely to lift this soon, because it
keeps the unit of compilation, of deployment and of review the same thing, one text a reader
can go through from top to bottom.

At file level the program holds four kinds of declaration, in any order: `struct`, `error`,
`state` and `def`, the last one optionally preceded by `external`. Nothing else stands at
file level. A statement outside a function, a constant or an expression is refused with
`expected 'struct', 'error', 'state' or 'def'`. Order does not matter for resolution: a
function may call a function declared below it, a struct may hold a struct declared below
it, and a body may name an error declared at the bottom of the file. The compiler collects
every declaration before it checks any body.

## Names and keywords

A name is a run of ASCII letters, digits and underscores that does not start with a digit.
Names are case sensitive, so `Point` and `point` are two names. Letters outside ASCII are not
letters to the lexer, and a source written with them is refused one character at a time. No
case convention is enforced; the corpus writes types in `CamelCase` and everything else in
`snake_case`, and the diagnostics quote names as written.

The keywords are reserved and cannot name anything: `struct`, `def`, `external`, `state`,
`if`, `elif`, `else`, `while`, `return`, `true`, `false`, `and`, `or`, `not`, `ref`, `own`,
`new`, `none`, `take`, `error`, `try`, `catch`. The word `error` being a keyword is why an
error is written `error.Name`, and why no variable can be declared with `error` as its type
([types.md](types.md)). `int`, `uint` and `bool` are not keywords but predeclared type
names.

## Comments

A comment runs from `//` to the end of the line, anywhere a line can end. There is no block
comment, and `/* */` is not one. A line holding only a comment, or nothing but spaces, is dropped by the lexer before
indentation is considered, so it never opens or closes a block and may sit at any width. The
corpus opens every program and every function with a `///` header; the third slash means
nothing to the compiler and marks documentation as opposed to a note on a line.

## Lines and blocks

A statement takes exactly one line. There is no separator and no continuation character,
and parentheses do not join two lines: a newline inside an open parenthesis ends the
statement with `expected ')'`. Long argument lists are where this is felt, and the answer
is a local variable. A statement written after a `return` in the same block is never reached
and is dropped without a diagnostic.

A block is opened by a `:` at the end of a line, its header, and consists of the lines that
follow at a deeper indentation. Indentation is measured in spaces. A tab anywhere in a line
is refused with `tabs are not allowed, use spaces`, because two editors do not agree on what
a tab is worth and the language would rather refuse than guess. The first line after the
header sets the width of the block, and every following line of the block has that width. A
line with less indentation closes the block, and possibly several enclosing ones, and must
land exactly on the width of a block still open, or it is `unindent does not match any outer
indentation level`. A line indented deeper than its block without a header opening a new one
is `unexpected indentation`, and a header with nothing indented below it is `expected an
indented block`. A block therefore holds at least one statement. There is no `pass`; a body
that does nothing is written `return`.

The width itself is free. The corpus uses four spaces. Two would work, and nothing requires
every block of a file to share a width, though nothing recommends mixing them either.

## Literals

An integer literal is a run of decimal digits and nothing else: no sign, no `0x` prefix, no
`_` separator, no exponent. `0x10` lexes as the literal `0` followed by the name `x10`, and
the parser reports `unexpected token at the end of the line`; `1_000` fails the same way.
The type of a literal is decided by its context, not by its digits: `uint x = 5` makes the
`5` a `uint`, `x + 1` with `x` an `int` makes the `1` an `int`, and a literal with nothing to
tell it otherwise is an `int`. [types.md](types.md) gives the full rule. The literal must fit
the type it ends up with, or it is `integer literal is too large for int`. A negative number
is not a literal: `-42` is the unary minus applied to `42`, which makes it an `int` in every
context, and `uint x = -1` is refused for that reason, a cast being the way to spell the
largest `uint`. The same rule has a corner: `-9223372036854775808` is refused, because the
literal is checked as an `int` before the minus applies, so the smallest `int` cannot be
written in source and has to be computed or passed in.

`true` and `false` are the two `bool` literals. `none` is the empty value of an optional own
([ownership.md](ownership.md)). `error.Name` names a declared error ([errors.md](errors.md)).
There is no string, character or floating-point literal, because the language has none of
those types.

## Expressions

Operators bind from the loosest to the tightest as the table below says. Every binary
operator associates to the left, the comparisons included: `a < b < c` parses as
`(a < b) < c`, which then compares a `bool` with a number and is refused with
`operator '<' cannot mix bool and int`. The language has no chained comparison.

| Level | Operators | Meaning |
|---|---|---|
| 1 | `a or b` | logical or, on bools |
| 2 | `a and b` | logical and, on bools |
| 3 | `not a` | logical not |
| 4 | `== != < <= > >=` | comparison, giving a `bool` |
| 5 | `+ -` | addition, subtraction |
| 6 | `* / %` | multiplication, division, remainder |
| 7 | `-a`, `ref place`, `try call`, `take place` | prefix forms |
| 8 | `f(a, b)`, `T(a, b)`, `x.field` | call, construction, field access, chained freely |
| 9 | literals, names, `new T(a, b)`, `(expression)` | primary forms |

`and` and `or` deserve a warning that other languages have made unnecessary: they do not
short-circuit. Both sides are always evaluated and the results are combined afterwards.
`b != 0 and a / b > 1` divides even when `b` is zero, and faults. The guard has to be an
`if`. The compiler emits the two operators as plain bitwise operations on one-bit values,
without a branch, which is also why they cost nothing; whether the language should gain
short-circuiting forms is an open question, and until it does the rule stands.

There is no bitwise operator, no shift, no increment and no compound assignment. `&&` and
`||` are not spellings of `and` and `or`; every one of those characters is an `unexpected
character`, and `a << 2` parses as two `<` comparisons.

The prefix forms of level 7 are not ordinary operators. `ref` may only appear on a call
argument or on the right of a ref declaration, `try` only on a call inside a function that
can fail, `take` only on a place holding an optional own. The parser accepts them wherever
an expression is accepted and the checker refuses the misplaced ones; [functions.md](functions.md),
[errors.md](errors.md) and [ownership.md](ownership.md) give the rules. Parentheses group and
do nothing else: there is no tuple.

## Statements

The statement forms are few. Each takes one line, except those that end with a header and
own a block.

| Statement | Reads as |
|---|---|
| `T x = expression` | declare `x` with the written type and that initial value |
| `T a, U b = f(...)` | declare one name per value of a call returning several |
| `ref T x = ref place` | bind `x` to a place, once and for all |
| `T x = expression or:` + block | bind an optional own, or run the block, which leaves |
| `x = expression`, `x.f.g = expression` | assign a variable or a field; the value may carry a `catch`, as a declaration's may |
| `f(...)`, `try f(...)`, `f(...) catch err:` + block | call for the effects, keeping no value |
| `if c:` + block, `elif c:` + block, `else:` + block | conditional, in that order |
| `if ref T x = ref place:` + block, `if own T x = expression:` + block | an arm that binds an optional own |
| `while c:` + block | the only loop |
| `return`, `return a, b`, `return error.Name`, `return err` | leave the function |

A declaration always carries its type. There is no inference and no `var`, and a variable is
declared with a value, never uninitialised. Two of the forms above are tied to their
position. `catch` follows a call that is the whole value of a declaration, of an assignment
or of a call statement, and nowhere else: `return f() catch 0` is refused where
`return try f()` is fine, and a failure to settle before returning is settled in a
declaration first. `or:` follows a declaration only; after an assignment, a `return` or an
`if` condition it is not recognised and the line fails on its `or`. The target of an assignment is a variable or a
chain of fields rooted in one, and nothing else, so `f() = 1` and `Point(1, 2) = p` are
refused as `only a variable or a field can be assigned`. A line that is neither a
declaration, an assignment nor a call, a bare `x + 1` for instance, is refused with
`expected '='`, because a value computed for nothing is more likely a mistake than an intent.

The conditions of `if`, `elif` and `while` are `bool` expressions and only those; an integer
is not a condition. There is no `for`, no `break` and no `continue`. A loop is a `while` and
leaves through its condition or through a `return`, and the absence of `break` is felt on
searches, which the corpus writes as a function returning from inside its loop.

## Scope

A declared name lives from its line to the end of the block it was declared in. The blocks
that scope names are the bodies of functions, of `if`, `elif` and `else` arms, of `while`,
of `catch err:` and of `or:`. A name declared in an inner block hides a name of an outer
block until the inner block ends, and the outer name is back afterwards, untouched. Two
declarations of one name in the same block are refused. A local may hide a function, a
struct or a state variable, since those live in the file's scope and a body's scope sits
inside it; the corpus never does, and a reader would not thank the author who did.

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
parser stopped, and the rest of the line is skipped. A token the parser expected and did not
find is not consumed, so one missing token can be reported several times at the same column,
and the checker may add a message of its own on the empty name it leaves behind.
