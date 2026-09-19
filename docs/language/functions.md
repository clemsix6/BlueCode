# Functions and refs

A BlueCode program is a flat set of functions over a flat set of structs. There are no
methods, no closures and no nested functions. A function is declared at file level, has a
name, parameters, results and a body, and may call any function of the file, itself
included. This chapter covers how functions are declared and called, how values and places
are passed, and the scoping and return rules the compiler enforces. Failure is the subject of
[errors.md](errors.md); moving owned values into and out of functions is covered in
[ownership.md](ownership.md).

## Declaration

```
def RESULTS[!] name (PARAMETERS):
    body

external def RESULTS[!] name (PARAMETERS):
    body
```

The result type comes first, then the name, then the parameter list in parentheses, a colon,
and the body as a block. `RESULTS` is one type, `(T1, T2)` for several, or `()` for none;
`def name ():` with no result type at all is accepted and means `()`, though the corpus
writes the parentheses. A `!` between the results and the name marks a function that can fail; it is written
against the results. With no parentheses around the results, the parser reads a name
followed by `(` as the function's name and anything before it as the result type.
`PARAMETERS` is a comma-separated list of `T name` or `ref T name`, possibly empty.

Every function of a file has a distinct name. There is no overloading, a second `def` with a
name already taken is refused, and so is a parameter named twice. A function may be called
before its declaration in the file and may call itself, directly or through others.
Recursion is bounded at run time by the depth limit, past which the call faults
([errors.md](errors.md)), so a function that recurses without a base case is a fault, not a
hang.

`external` puts the function on the module's boundary. It receives a symbol and an entry in
the manifest, and it is the only kind of function a host can call ([modules.md](modules.md)).
That position carries a restriction: its parameters and results must be plain values,
scalars and structs of scalars, because the host cannot hold or interpret an owned value, and
`'crossing' is external: it cannot take or return an own` says so. `ref` parameters are
allowed on an external function, and the host sees the argument written back in place. A
function without `external` is reachable from the program only and gets no symbol, which
lets the optimizer inline it and keeps the module's surface to what was meant to be public.

## Parameters and arguments

A parameter is a copy of its argument. The function receives the value, may change it, and
the change is gone when it returns. The copy is a place of the function's own, so a `ref`
may be taken on it, which is what `moved_x` below does. A `ref` parameter is the opposite: it is the caller's
place, a variable or a field of one, and what the function writes through it is written
there. The distinction is visible on both sides. The declaration says `ref Point point` and
the call says `translate(ref point, dx, 0)`. A call that omits the keyword on a ref parameter
is refused with `argument 1 must be passed with 'ref'`, and a `ref` argument to a value
parameter with `argument 2 is not a ref parameter`. A reader of a call site therefore knows
which of the caller's variables the call may change without opening the callee.

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

What follows `ref` must be a place: a variable, or a field reached from a variable through
fields, as in `ref line.start`. A call, a literal or an arithmetic result has no address to
lend, and `ref f()` is refused with `'ref' needs a variable or a field, not a value`. Two
ref arguments of one call may name the same place, `swap(ref a, ref a)`, with the ordinary
sequential effect; the compiler assumes nothing about aliasing, so this is deterministic if
pointless. Argument count and types must match the declaration exactly. There is no default
argument and no variadic function. A literal argument takes the parameter's type. An owned
value passed as an argument is moved into the callee, which then owns it
([ownership.md](ownership.md)).

A ref is a flag on a binding, not a type. There is no "ref to int" that a struct could hold,
no ref returned from a function and no ref in `state`; `a struct field cannot be a ref`,
`a function cannot return a ref` and `a state variable cannot be a ref` are the three
diagnostics. Together they guarantee that a ref only ever points up the stack, at a variable
of a caller or at memory the program owns and that outlives the call, which is what spares
the language a lifetime system.

## Ref variables

A ref may also be a local. `ref T x = ref place` binds `x` to a place for the rest of its
block, and every use of `x` is a use of that place: reading `x.field` reads it, `x.field =
v` writes it, `x = v` writes the whole place. Outside the bindings of an optional own
([ownership.md](ownership.md)), this declaration is the only form. It stands
alone on its line, refused when mixed into a multi-declaration, and is bound with `ref` on
the right, refused when bound to a value. There is no rebinding, since `x = ref other` is not
a statement. After the declaration, `x = other` copies `other` into the place `x` names, and
`'ref' is only allowed on a call argument or in a ref declaration` is what a `ref` anywhere
else reports.

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

A ref local exists to name a deep place once instead of repeating the chain, and to bind the
content of an optional own ([ownership.md](ownership.md)). While it exists, the place it
points into is frozen: no assignment to the place or to anything containing it, no move out
of it, no `take`, and no passing it by ref, until the ref's block ends. The reason is memory
rather than aliasing. The place could hold an owned value, and assigning it would free what
the ref points at. The rule is stated in full in [ownership.md](ownership.md); it applies to
every ref local, whether the place holds anything owned or not, because the check is made on
the shape of the code and not on the values.

## Calls and statements

A call inside an expression yields exactly one value. A call on a line of its own is a
statement and must yield none: a statement `mixed(x, uint(x))`, with `mixed` returning an
`int`, is refused with `the call returns 1 value(s) that are not used`. A language for code
that moves value cannot let a result vanish because nobody wrote a name for it. The author
who does not want the value writes it into a variable and leaves the variable unused. A call
to a function that can fail is never bare, whatever its results ([errors.md](errors.md)).

## Return

`return` leaves the function. In a `()` function it stands alone. In a function with results
it is followed by as many values as the declaration lists, each of the declared type, and
`'count' returns 2 value(s), got 1` or `cannot return int where bool is expected` report the
mismatches. `return f(...)` may pass on the values of a call whose results match the
function's own in number and types, which is how a tail call on several results is written
without naming them.

A function with results must end every path on a `return`. The compiler decides this by
shape, not by values. The last statement of the body is a `return`, or it is an `if` that
has an `else` and whose every arm, `if`, `elif` and `else`, ends the same way, recursively.
Nothing else counts. In particular a `while` never counts as a path that returns, even `while
true:`, so a function whose result comes out of a loop has a `return` after the loop,
reachable or not. The alternative, proving that a loop cannot fall through, would make the
compiler's acceptance depend on an analysis a reader cannot run in their head. A `()`
function may fall off its end.

## Scope

The scope rules are those of [syntax.md](syntax.md): a name lives to the end of its block,
inner blocks may hide outer names, one block declares a name once. Parameters are names of
the body. Functions, structs and state variables live in the file's scope, are visible in
every body, and may be hidden by a local, which the compiler allows and the corpus avoids. A
name that is not visible, because it was never declared or because its block has ended, is
`undefined name`; a call to a name that is not a function is `undefined function`. Using a
function's name without calling it, using a type's name as a value, or calling a variable
are refused with messages that say which of the three happened.

## Refused

| Message | Cause |
|---|---|
| `'twice' is already declared` | two functions, structs or states with one name, or one named `int`, `uint` or `bool` |
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
