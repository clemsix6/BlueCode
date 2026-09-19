# Types

## Scalars

| Type | Values | Size |
|---|---|---|
| `int` | signed 64-bit | 8 bytes |
| `uint` | unsigned 64-bit | 8 bytes |
| `bool` | `true`, `false` | 1 byte |

There is no other scalar: no float, no string, no char, no narrower integer.

An integer literal takes the type its context expects and is `int` otherwise: the declared
type in `uint x = 5`, the other operand in `x + 1` on either side, the parameter in `f(5)`,
the field in `Point(1, 2)`, the result type in `return 0`, the call's type in `f() catch 0`.
It must fit that type.

`int` and `uint` never mix. Both sides of `+ - * / %` and of a comparison have one type; the
result of arithmetic is that type, the result of a comparison is `bool`. Convert explicitly:
`uint(x)` and `int(x)` reinterpret the 64 bits and never check the range, so `uint(-1)` is the
largest `uint`. A cast takes exactly one argument and only goes between `int` and `uint`.

| Operation | `int` | `uint` | Faults when |
|---|---|---|---|
| `a + b`, `a - b`, `a * b` | signed, checked | unsigned, checked | the result does not fit (`overflow`) |
| `a / b` | truncates toward zero | | `b` is zero (`division by zero`); the `int` minimum divided by `-1` (`overflow`) |
| `a % b` | the sign of `a` | | `b` is zero (`division by zero`) |
| `-a` | checked | refused | `a` is the `int` minimum (`overflow`) |

A fault ends the call; [errors.md](errors.md) says what the host sees.

`==` and `!=` compare `int`, `uint`, `bool` and `error` values; `< <= > >=` order `int` and
`uint` only. Structs and owns are never compared. `and`, `or` and `not` take bools and give
a bool, and both sides of `and` and `or` are always evaluated.

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

A struct is declared at file level with one field per line, each `T name`. A field is a
scalar, another struct declared anywhere in the file, or an `own` ([ownership.md](ownership.md));
it cannot be a ref. A struct cannot contain itself by value, only through an `own`.

`T(v1, v2, ...)` builds a value with one argument per field, in declaration order, no more,
no fewer, each of its field's type. `x.field` reads a field, `x.field = v` writes it, and the
chain goes as deep as the structs do: `r.origin.x`.

A struct is a value: assigning, passing or returning it copies it, so a function that changes
a parameter's field changes its own copy. Take a `ref` to change the caller's
([functions.md](functions.md)). A struct holding an `own` is never copied but moved
([ownership.md](ownership.md)).

Fields sit in order at natural alignment, 8 for the integers and owns, 1 for a bool, and the
struct is padded to its widest alignment. The manifest lists every offset ([host.md](host.md)).

## Several results

A function returns any number of values; the type is written `(T1, T2)`, and `()` for none:

```bluecode
def (uint, uint) divmod (uint a, uint b):
    return a / b, a % b


def uint use_divmod (uint a, uint b):
    uint quotient, uint remainder = divmod(a, b)
    return quotient * 10 + remainder
```

The values of such a call are received by a declaration with one name per value, in order,
or passed on whole by `return divmod(a, b)` from a function that returns the same number of
values of the same types. In any other position a call yields exactly one value: a call
returning two cannot be an operand, and a call returning nothing cannot be a value.

## The error type

`error` is the type of `error.Name` and of the `err` a catch block binds. It is only ever
returned, passed on, or compared with `==` and `!=`. It cannot be declared as the type of a
variable, a field, a parameter or a result: a function that can fail is marked `!` instead
([errors.md](errors.md)).

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
