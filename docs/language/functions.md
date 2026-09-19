# Functions

## Declaration

```
def RESULTS[!] name (PARAMETERS):
    body

external def RESULTS[!] name (PARAMETERS):
    body
```

`RESULTS` is one type, `(T1, T2)` for several, `()` for none; `def name ():` with no result
type at all means `()` as well. A `!` right after the results marks a function that can fail
([errors.md](errors.md)). `PARAMETERS` is a comma-separated list of `T name` or `ref T name`,
possibly empty. The body is a block.

Functions are declared at file level only: no nested function, no closure, no overloading,
one name each. A function may call any function of the file, including itself and the ones
declared below it. Recursion is bounded by the depth limit ([errors.md](errors.md)).

`external` makes the function callable from the host, with a symbol and an entry in the
manifest ([host.md](host.md)). Its parameters and results are plain values: no `own`, no
struct holding one; `ref` parameters are allowed and are written back into the host's block.
A function without `external` is reachable from the pod only.

## Parameters and arguments

A parameter is a copy of the argument: what the function writes to it is gone when it
returns. A `ref` parameter is the caller's place itself, a variable or a field: the function
reads and writes the place, and what it writes stays. The call site says which is which.

```bluecode
struct Point:
    int x
    int y


def () translate (ref Point point, int dx, int dy):
    point.x = point.x + dx
    point.y = point.y + dy


def int moved_x (Point point, int dx):
    translate(ref point, dx, 0)
    return point.x
```

A `ref` argument is `ref` followed by a place: a variable, or a field reached through
variables and fields. A value, a call or a literal cannot be passed by ref. A `ref` parameter
takes a `ref` argument and nothing else, and a value parameter never takes one. Arity and
types must match; a literal takes the parameter's type. Two ref arguments may name the same
place. An `own` argument moves the value into the callee ([ownership.md](ownership.md)).

## Ref variables

A ref variable names a place for the rest of its block:

```bluecode
struct Point:
    int x
    int y


struct Line:
    Point start
    Point end


def () clamp_start (ref Line line):
    ref Point p = ref line.start
    if p.x < 0:
        p.x = 0
    if p.y < 0:
        p.y = 0
```

`ref T x = ref place` is the only way to declare one: alone on its line, bound with `ref`,
once. `x = v` afterwards writes `v` into the place; there is no rebinding. A ref is never
stored in a struct, kept in `state` or returned, so it always points at something that
outlives it, the caller's variable or a place in the pod's own memory, and the compiler needs
no lifetime analysis. While a ref variable exists, the place it points into is frozen: it
cannot be assigned, moved, taken or passed by ref until the ref's block ends
([ownership.md](ownership.md)).

## Calls

In an expression, `f(args)` yields exactly one value. On a line of its own, a call is a
statement and must yield none: a value that is not used is an error, because a pod cannot
afford to drop a result silently. A call to a function that can fail is never bare
([errors.md](errors.md)).

## Return

`return a, b` returns the values, as many as the declaration says and of its types; `return`
alone leaves a `()` function. `return f(...)` passes on every value of a call whose results
match the function's. A function with results ends every path on a `return`: the last
statement of its body is a `return`, or an `if` with an `else` whose every arm ends so. A
`while` never counts, even `while true:`. A `()` function may fall off its end.

## Scope

A name lives from its declaration to the end of its block. Blocks are the bodies of
functions, `if` arms, `else`, `while`, `catch err:` and `or:`. A name declared in an inner
block hides the same name of an outer one until the block ends; a name cannot be declared
twice in one block. Parameters are names of the function's body. Function, struct and state
names are visible everywhere, and a local may hide them.

## Refused

| Message | Cause |
|---|---|
| `'twice' is already declared` | two functions, structs or states with one name |
| `duplicate parameter 'a'` | |
| `expected 2 argument(s), got 1` | |
| `argument 1 must be passed with 'ref'` | a value where a ref parameter is |
| `argument 2 is not a ref parameter` | `ref` where a value parameter is |
| `argument 1 should be own Node, got Node` | an argument of another type |
| `'ref' needs a variable or a field, not a value` | `ref f()`, `ref 1` |
| `'ref' is only allowed on a call argument or in a ref declaration` | `ref` in an expression, `x = ref y`, `return ref x` |
| `a ref variable must be bound with 'ref'` | `ref int m = n` |
| `a ref variable must be declared on its own` | a ref among the names of a multi-declaration |
| `a function cannot return a ref` | `def ref T f` |
| `a struct field cannot be a ref` | |
| `a state variable cannot be a ref` | |
| `the call returns 1 value(s) that are not used` | a call with results on a line of its own |
| `the call returns nothing` | a `()` call used as a value |
| `not all paths of 'falls_through' return a value` | |
| `'count' returns 2 value(s), got 1` | a `return` with the wrong count |
| `cannot return int where bool is expected` | |
| `'passes_on_wrong_count' returns 2 value(s), the call gives 1` | `return g()` passing on the wrong count |
| `value 1 of the call is int, uint is expected` | `return g()` passing on the wrong types |
| `undefined function 'twice'` | |
| `undefined name 'y'` | a name never declared, or declared in a block that has ended |
| `'y' is already declared in this block` | |
| `'twice' is a function, not a value` | a function name used without a call |
| `'Point' is a type, not a value` | |
| `'x' is not a function` | calling a variable |
| `only a function or a type can be called` | `p.x(1)`, `f()()` |
| `'crossing' is external: it cannot take or return an own` | |
