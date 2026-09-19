# Ownership and state

## own and new

```bluecode
struct Point:
    int x
    int y


struct Segment:
    own Point start
    own Point end


def own Segment segment (int a, int b):
    return new Segment(new Point(a, a), new Point(b, b))


def int length (int a, int b):
    own Segment s = segment(a, b)
    return s.end.x - s.start.x
```

`own T` is a struct `T` kept in a block of the instance's heap and owned by the variable,
field or parameter that holds it. `new T(v1, ...)` allocates that block and builds the value
in it, one argument per field like `T(...)`, and hands it to whoever receives it. Only a
struct can be owned. An `own T` is not a `T`: it cannot be copied into a `T` variable, but
its fields are read and written through it, `s.end.x`, and a ref can be taken into it.

The value is freed when its owner lets go: at the end of the block that declared the
variable, when the variable or field holding it is assigned something else, when a function
that received it as a parameter returns without passing it on, or when the struct or
instance holding the field is freed. Freeing an owned struct frees what it owns in turn.
There is no `free`, no garbage collector and no reference count: the compiler places every
drop.

## Moves

A value that owns memory, an `own` or a struct with an `own` field, is never copied. Using a
variable holding one as a value moves it: into a parameter, a field, a new variable, a
result. The variable is then dead until it is assigned again, and the compiler refuses to
read it. Moving, in the body of a loop, a variable declared around the loop is refused too,
unless the body assigns it again before its end.

A field, a `ref` and a `state` variable are never moved out of: reach in with `ref`, or take
the value out with `take`. A fresh value, a `new`, a call's result or a `take`, is free to
give.

```bluecode
struct Node:
    int value
    own Node? next


def () keep (own Node node):
    return


def () hand_over ():
    own Node node = new Node(1, none)
    keep(node)
    node = new Node(2, none)
    keep(node)
```

## Optional owns

`own T?` may hold nothing, written `none`; it is the only optional type. An `own T` goes
where an `own T?` is expected, and so does `none`, never the reverse. An `own T?` is used by
binding it, and the binding is the only way to reach what it holds:

| Form | Binds | When empty |
|---|---|---|
| `if ref T x = ref place:` | `x`, a ref to the content, for the arm | the arm is skipped; `elif` and `else` follow as usual |
| `if own T x = expression:` | `x`, the content moved out, for the arm | the arm is skipped |
| `ref T x = ref place or:` + block | `x`, a ref to the content, for the rest of the block | the block runs and must leave the function |
| `own T x = expression or:` + block | `x`, the content moved out, for the rest of the block | the block runs and must leave the function |

The `expression` of an `own` binding is a `take`, a call, or a variable of type `own T?`,
which moves. Reading `place.field` through an unbound `own T?` is refused, and so is `==` on
any own: bind it to know whether it holds something.

```bluecode
struct Node:
    uint key
    own Node? left
    own Node? right


def uint size (ref Node tree):
    uint total = 1
    if ref Node left = ref tree.left:
        total = total + size(ref left)
    if ref Node right = ref tree.right:
        total = total + size(ref right)
    return total


def (bool, uint) find (ref Node tree, uint key):
    if key == tree.key:
        return true, key
    if key < tree.key:
        ref Node child = ref tree.left or:
            return false, 0
        return find(ref child, key)
    ref Node child = ref tree.right or:
        return false, 0
    return find(ref child, key)
```

## take

`take place` moves the content out of an `own T?` place, a variable, a field or a state, and
leaves `none` in it. It is the only way to remove something from a structure. It yields an
`own T?`, to bind with `or:` or `if own`, or to assign to another `own T?` place.

```bluecode
struct Transfer:
    uint amount
    own Transfer? next


state own Transfer? head


def () enqueue (uint amount):
    head = new Transfer(amount, take head)


def (bool, uint) dequeue ():
    own Transfer first = take head or:
        return false, 0
    head = take first.next
    return true, first.amount
```

## The frozen rule

While a ref variable points into a place, that place, with everything containing it and
everything it contains, is frozen: it cannot be assigned, moved, taken or passed by ref until
the ref's block ends. Otherwise the ref could name freed memory. The rule is lexical: it
holds for the block the ref was declared in, whatever the values are.

## State

`state T name` at file level declares a variable that lives in the instance from one call to
the next, zero or `none` when the instance is created. Any type goes: a scalar, a struct, an
`own`, an `own T?`. A state is read, assigned (the old value is freed), reached with `ref` and
taken with `take`; it is never moved out of and never a ref. The host creates the instance
and sizes its heap ([host.md](host.md)); a call made without an instance runs on a throwaway
one.

## Refused

| Message | Cause |
|---|---|
| `only a struct can be owned, not int` | `own int` |
| `'int' is not a struct` | `new int(1)` |
| `only an own can be optional: write 'own T?'` | `Node?` |
| `'node' was moved` | reading a variable after moving it |
| `'node' is moved inside a loop` | moving a variable of an enclosing block in a loop body that does not assign it again |
| `cannot move out of a field: take a ref into it, or take it with 'take'` | `keep(node.next)` |
| `cannot move out of ref 'node'` | `keep(node)` with `ref Node node` |
| `cannot move out of state 'root': take a ref into it, or take it with 'take'` | |
| `cannot assign own Point to 'q' of type Point` | copying out of an own |
| `cannot assign none to 'node' of type own Node` | `none` for an own that is not optional |
| `own Node? may be none: bind it with 'if ref' or 'or:' first` | `node.next.value`, `ref node.next` as an argument |
| `an own cannot be compared: bind it with 'if ref' to see whether it holds something` | |
| `a value that owns memory has to be kept in a variable before its fields are read` | `leaf(1).value` |
| `'take' needs an optional own, got own Node` | |
| `'take' needs a variable or a field` | `take leaf(1)` |
| `expected an own that may be none, got Node` | binding from something that is not an `own T?` |
| `cannot bind Node to 'child' of type Point` | a binding declared with the wrong type |
| `the 'or:' block must leave the function` | |
| `a declaration with 'or:' binds one name` | |
| `cannot assign 'line.start' while 'p' refers into it` | the frozen rule; the same message says `move`, `take` or `pass by ref` |
| `argument 1 should be own Node, got own Node?` | an optional own where a plain one is expected: bind it first |
| `'crossing' is external: it cannot take or return an own` | |
