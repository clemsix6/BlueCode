# Types

BlueCode has three scalar types, structs built from them, and two kinds of value that only
exist in specific positions: errors and owned structs. There is no floating point, no
string, no array and no integer narrower than 64 bits. The set is small on purpose. Every
value has one representation, one size and one meaning on every machine, which is what lets
two hosts compute the same result from the same input without a specification of rounding,
encoding or promotion.

## Integers

`int` is a signed 64-bit integer in two's complement and `uint` an unsigned one. They are
distinct types and never convert into each other on their own. An operator takes two
operands of one type and produces that type; `a + b` with `a` an `int` and `b` a `uint` is
refused with `operator '+' cannot mix int and uint`. There is no promotion rule to remember,
which is the point: what a program mixes, it mixes visibly.

Conversion is explicit and looks like a call, `uint(x)` and `int(x)`. It reinterprets the 64
bits and never checks the range, so `uint(-1)` is the largest `uint` and `int(uint(-1))` is
`-1` again. This is the one place where a value silently changes meaning, and the cast is
written out for that reason. A cast takes exactly one argument and only goes between `int`
and `uint`; `uint(b)` with `b` a `bool` is refused.

A literal takes the type its context expects and is an `int` otherwise. The context is the
declared type of a variable, as in `uint x = 5`; the other operand of a binary operator, on
whichever side the literal sits, as in `2 * x` with `x` a `uint`; the parameter it is passed
to; the field it initialises in `Point(1, 2)`; the result type of the function in `return 0`;
and the type of the call in `f() catch 0`. The literal must fit: `9223372036854775808`
cannot be an `int` and is refused as `integer literal is too large for int`, while it is a
valid `uint` where a `uint` is expected. A negative literal does not exist. `-5` is the unary
minus applied to `5`, and since `-` only applies to `int`, `-5` is always an `int`; `uint x =
-5` is therefore refused, not because `-5` is negative but because it is an `int`.

Arithmetic is checked. `+`, `-` and `*` compute the mathematical result and fault with
`overflow` when it does not fit the type, for `int` and `uint` alike, so a `uint`
subtraction that would go below zero faults rather than wrapping. Unary `-` is an `int`
operation only and faults on the minimum `int`, whose negation does not fit. Division and
remainder follow C: `/` truncates toward zero for `int` and is the ordinary unsigned division
for `uint`, `%` keeps the sign of the dividend for `int`. Both fault with `division by zero`
on a zero divisor, and both fault with `overflow` on the `int` minimum by `-1`, where C
would return 0 for the remainder. A fault ends
the call; [errors.md](errors.md) describes what the host sees.

| Operation | `int` | `uint` | Faults when |
|---|---|---|---|
| `a + b`, `a - b`, `a * b` | signed, checked | unsigned, checked | the result does not fit (`overflow`) |
| `a / b` | truncates toward zero | unsigned | `b` is zero (`division by zero`); the `int` minimum divided by `-1` (`overflow`) |
| `a % b` | the sign of `a` | unsigned | `b` is zero (`division by zero`); the `int` minimum by `-1` (`overflow`) |
| `-a` | checked | refused | `a` is the `int` minimum (`overflow`) |

There is no wrapping form of these operators, and no bitwise operator or shift. A program
that wants modular arithmetic on 64 bits has no way to write it today, which is a known gap
rather than a position.

Comparison is the usual six operators. `==` and `!=` apply to `int`, `uint`, `bool` and
`error`; `<`, `<=`, `>` and `>=` apply to `int` and `uint` only, with signed and unsigned
order respectively. Structs and owned values are never compared: there is no structural
equality and no identity to compare. Ordering bools is refused with `operator '<' needs
numbers, got bool`.

## Booleans

`bool` holds `true` or `false` and is one byte wide, holding 0 or 1. `and`, `or` and `not`
combine bools and give a bool; `and` or `or` on integers is refused with `operator 'and'
needs bools, got int`. Both operands of `and` and `or` are always evaluated
([syntax.md](syntax.md)). A `bool` is not a number and a number is not a `bool`: `if x:` with
an integer is refused with `condition must be bool, got int`, and there is no cast between
the two.

## Structs

```bluecode
struct Point:
    int x
    int y


struct Rect:
    Point origin
    int width
    int height


def int area (Rect r):
    return r.width * r.height


def Point far_corner (Rect r):
    Point corner = r.origin
    corner.x = corner.x + r.width
    corner.y = corner.y + r.height
    return corner
```

A struct is declared at file level: the keyword, a name, a colon, then one field per line in
an indented block, each a type followed by a name. A struct holds at least one field, since
an empty block is not a block; `expected an indented block of fields` reports the omission,
on the line of whatever declaration follows. A struct, a function or a state may not be
named `int`, `uint` or `bool`, which are declared before the file is read. A field may be a scalar, another struct
or an owned struct ([ownership.md](ownership.md)). It may not be a ref, because a ref is a
name for a place that must outlive it, and a struct can be copied anywhere
([functions.md](functions.md)). A struct may hold a struct declared further down the file;
the compiler declares every struct before it resolves any field. It may not hold itself by
value, directly or through another struct, since its size would be infinite, and `struct
'Loop' contains itself: own the field instead` says what to do about it: a tree of nodes is
exactly the `own Node?` field of [ownership.md](ownership.md). Two fields of one struct
cannot share a name.

A struct value is built by calling its name with one value per field, in the order of the
declaration: `Point(1, 2)`. There is no default value, no named argument and no partial
construction. Every field is given every time, and a construction with too few values or a
value of the wrong type is refused, so that adding a field to a struct breaks every
construction of it, which is the safe way to find them all. A field is read with a dot,
`r.origin.x`, and written the same way, `r.origin.x = 0`, through as many levels as the
structs nest.

A struct is a value. Declaring a variable from another, passing a struct to a function,
returning one, all copy the whole struct, fields included. A function that assigns to a field
of a value parameter changes its own copy and the caller's variable stays what it was. To
change the caller's struct the parameter is a `ref` ([functions.md](functions.md)). The
exception is a struct that holds an `own`: such a struct cannot be copied, only moved, and
[ownership.md](ownership.md) explains why.

Structs have no methods, no visibility and no inheritance. A function that works on a
`Point` is a function taking a `Point`, declared anywhere in the file.

## Layout

Every type has a size and an alignment that the compiler and the host agree on, because the
host reads and writes struct values directly ([modules.md](modules.md)). The rule is the
natural alignment rule of C on 64-bit platforms. `int`, `uint` and `error` are 8 bytes
aligned on 8. `bool` is 1 byte aligned on 1. An owned field is a pointer, 8 bytes aligned on
8, and only ever exists inside the module. A struct lays its fields out in declaration order,
each at the next offset that is a multiple of its alignment; the struct's alignment is the
largest of its fields' and its size is rounded up to it. A struct of two `uint` fields and a
`bool` is therefore 24 bytes, with the `bool` at offset 16 and seven bytes of padding after
it, and a Go struct `struct{ ID, Balance uint64; Frozen bool }` has exactly that layout
without any annotation. The manifest lists every offset, so a host never computes them.

## Several results

```bluecode
def (uint, uint) divmod (uint a, uint b):
    return a / b, a % b


def uint use_divmod (uint a, uint b):
    uint quotient, uint remainder = divmod(a, b)
    return quotient * 10 + remainder
```

A function returns any number of values. Its result type is written `(T1, T2)`, and `()` for
none. The values of such a call go to one of two places. A declaration with one name per
value receives them in order, each name with its own written type. A `return` may pass them
on whole, `return divmod(a, b)`, from a function whose results have the same count and
types. Anywhere else a call must yield exactly one value: `pair(a) + 1` is refused with `the
call returns 2 values, only one can be used here`, and `return nothing()` with a `()`
function is `the call returns nothing`. There is no tuple type and no way to hold several
results in one variable. They are received and named at once, which is the intent.

## The error type

`error` is the type of `error.Name` and of the `err` a catch block binds
([errors.md](errors.md)). It is only ever returned, passed on with `return err`, or compared
with `==` and `!=` to another error. It cannot name the type of a variable, a field, a
parameter or a result, since `error` is a keyword and not a type name; a function that can
fail says so with `!` and its error rides along with its results. An error carries no
payload, only its identity, and the trace of the failure is what carries the context.

## Refused

| Message | Cause |
|---|---|
| `integer literal is too large for int` | a literal past the range of its type |
| `operator '+' cannot mix int and uint` | two operand types on one operator; cast one side |
| `operator '<' needs numbers, got bool` | ordering anything but `int` or `uint` |
| `operator 'and' needs bools, got int` | `and` or `or` on numbers |
| `'not' needs a bool, got int` | |
| `unary '-' needs an int, got uint` | negating a `uint` |
| `cannot cast bool to uint` | a cast from or to anything but `int` and `uint` |
| `a cast to uint takes exactly one argument` | |
| `structs cannot be compared with '=='` | |
| `condition must be bool, got int` | an `if` or `while` on anything but a bool |
| `cannot assign int to 'y' of type uint` | a declaration whose value has another type |
| `cannot assign uint to a target of type int` | an assignment whose value has another type |
| `'Point' has 2 field(s), got 1 value(s)` | a construction with the wrong number of values |
| `field 'y' of 'Point' is int, got bool` | a construction value of the wrong type |
| `struct 'Point' has no field 'z'` | |
| `int has no fields` | `.field` on a scalar |
| `struct 'Loop' contains itself: own the field instead` | a struct holding itself by value |
| `struct 'Point' already has a field 'x'` | |
| `expected 2 value(s), got 1` | a multi-declaration and a call that disagree on the count |
| `the call returns 2 values, only one can be used here` | a call with several results inside an expression |
| `the call returns nothing` | a `()` call used as a value |
| `only an own can be optional: write 'own T?'` | `Point?` |
| `unknown type 'Foo'` | |
| `'count' is not a type` | a variable or a function name in a type position |
