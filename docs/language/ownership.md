# Ownership and state

Most of BlueCode works on values that live in variables and copy freely. Two things need
more: data whose size is not known at compile time, a list or a tree, and data that outlives
a call. Both are provided by owned structs, allocated in memory the host lends to the module,
and by `state`, the variables a module keeps from one call to the next. The design is single
ownership with moves and compile-time drops, the shape of Rust without borrows that can be
stored. Every allocated struct has exactly one owner at every moment, a variable, a field or
a parameter. Giving it to someone else moves it. When the owner lets go, the struct is freed,
along with everything it owned in turn. There is no garbage collector, no reference count and
no arena, and freed memory returns to the instance's heap at once. The compiler places every
free and refuses every program in which a value could be used after it was given away.

## Owned structs

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

`own T` names a struct `T` that lives in a block of the instance's heap and belongs to
whatever holds the `own T` value: a variable, a field, a parameter. Only a struct can be
owned; `own int` is refused, since a scalar has no reason to live anywhere but where it is
used. `new T(v1, ...)` allocates the block and builds the value in it with one argument per
field in declaration order, as `T(...)` does for a plain struct, and yields an `own T` for
whoever receives it. `new T` always names the declared struct `T`, whatever a local of that
name holds. If the heap has no block to give, `new` faults with `out of memory`
([errors.md](errors.md)). The size of the heap is the host's decision, and a module that must
not fault sizes its data to it.

An `own T` is not a `T`. The fields of the owned struct are read and written through the
value, `s.end.x`, and a ref may be taken into it, but the struct cannot be copied out into a
`T` variable: `Point q = p` with `p` an `own Point` is refused with `cannot assign own Point
to 'q' of type Point`. A program that wants a copy builds one field by field. The asymmetry
keeps the two kinds of value apart in the reader's mind. A plain struct is bytes in a
variable; an owned struct is a block with an owner.

A field of a struct may be an `own`, which is what makes a linked structure: a node owns its
children and the root owns the tree. A struct with an owned field is itself an owning value,
and the rules below apply to it as they apply to an `own`.

## When memory is freed

The owner frees what it owns when it lets go, and it lets go in four ways. A variable goes
out of scope at the end of the block that declared it, and whatever it still owns is freed
there. A variable or a field is assigned a new value, and the old value is freed before the
new one is stored, so a field assigned in a loop never holds more than one value. A function
that received an owned value as a parameter returns without having moved it anywhere, and
the value is freed on return. And a struct that owns fields is freed, which frees its
fields, recursively, so dropping the root of a tree drops the tree.

Every function has a single exit path that frees what its locals still own, and every early
return, every relayed failure and every fault goes through it, so a failure never leaks what
the failing call had allocated. The compiler tracks, for each owning local, whether it
currently holds a value, and frees only what is held. There is nothing to write for any of
this, and there is no destructor either: freeing returns the block to the heap and runs no
code of the program's.

## Moves

An owning value is never copied, because two owners would free the block twice. Using a
variable that holds one as a value moves it: into a parameter when passed, into a field or a
variable when assigned or declared from, into the caller's hands when returned. After the
move the variable is dead. Reading it is refused with `'node' was moved` until an assignment
gives it a value again, after which it is alive as before. The check is flow-sensitive within
a function: a variable moved in one arm of an `if` is dead after the `if`, unless that arm
always returns, since a path that leaves the function leads nowhere.

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

Loops get a stricter rule. A variable declared outside a `while` and moved inside its body
would be moved a second time on the next iteration, so the compiler refuses the move with
`'node' is moved inside a loop`, unless the body assigns the variable again before its end,
in which case every iteration starts with a live value. A variable declared inside the body
is a fresh variable each time and may be moved freely.

Three kinds of place are never moved out of. A field, because the struct holding it would be
left with a dead field and no way to tell; `keep(node.next)` is refused with `cannot move out
of a field: take a ref into it, or take it with 'take'`. A `ref`, because the place belongs
to someone else. A `state` variable, because the instance would be left with a hole. The
message names the two alternatives, which are the two forms below: reach into the place with
`ref`, or, when the place is an `own T?`, take the value out with `take`, which leaves
something well defined behind. A field or a state of type `own T`, without the question
mark, is only ever reached with `ref`. A fresh
value, the result of `new`, of a call or of a `take`, belongs to nobody yet and may be given
anywhere. A fresh owning value whose fields are read without being stored first, as in
`leaf(1).value`, is refused, because nothing would own it afterwards.

## Optional owns

`own T?` is an `own T` that may hold nothing. It is the only optional type in the language,
and `none` is its empty value. An `own T` may be used where an `own T?` is expected, and so
may `none`; the reverse is refused, an `own T?` passed to an `own T` parameter reporting
`argument 1 should be own Node, got own Node?`. Nothing is reachable through an `own T?`
directly. `node.next.value` is refused with `own Node? may be none: bind it with 'if ref' or
'or:' first`, and so is comparing an own with `==`, since the question "is it empty" is
answered by binding it, never by a test.

Binding is the operation that turns an `own T?` into something usable, and it has four forms.
The two `if` forms run an arm with the name bound when the optional holds something and skip
it otherwise; `elif` and `else` follow as usual. The two `or:` forms bind the name for the
rest of the block and run the `or:` block when the optional is empty. That block must leave
the function, for the same reason a catch block must ([errors.md](errors.md)): so that the
name is bound on every line after it. A block that could fall through is refused with `the
'or:' block must leave the function`.

| Form | Binds |
|---|---|
| `if ref T x = ref place:` | a ref to the content, for the arm |
| `if own T x = expression:` | the content, moved out, for the arm |
| `ref T x = ref place or:` + block | a ref to the content, for the rest of the block |
| `own T x = expression or:` + block | the content, moved out, for the rest of the block |

The `ref` forms bind into the place without moving anything. A tree is walked with `if ref
Node child = ref tree.left:` and stays whole. The `own` forms move the content out of an
expression, which is a `take`, a call returning an `own T?`, or a variable of that type,
which is then moved. The type written on the binding is the plain struct for a ref and the
plain own for an own; `cannot bind Node to 'child' of type Point` reports a mismatch, and a
binding from a value that is not an `own T?` is `expected an own that may be none, got Node`.

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

## Taking

`take place` moves the content out of an `own T?` place, a variable, a field or a state, and
stores `none` in its stead. It is the only way to remove something from a structure, and it
is what makes a queue or a stack writable: the head is taken out of the state, its successor
is taken out of it and put back in the state, and the head itself is returned or dropped.
`take` yields an `own T?`, to bind with `or:` or `if own`, or to give wherever an `own T?`
is expected: another optional place, a parameter, a field of a `new`. It applies only to a place, `'take' needs a variable or a field`, and only to an
optional own, `'take' needs an optional own, got own Node`, since a plain `own T` has no
empty value to leave behind.

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

`enqueue` shows the idiom for pushing. The new node takes the old head as its successor in
the same expression that builds it, and the assignment to `head` stores the node. Nothing is
freed on the way, since the old head was taken and not overwritten. In `dequeue`, the node
bound to `first` is freed when the function returns, after its fields were read; its
successor was taken back into the state before that, so nothing goes with it.

## The frozen rule

A ref local names a place, and while it does, the place must stay what it was. Assigning it
would free an owned value the ref points into. Moving it or taking it would empty it. Passing
it by ref to a function would let that function do either. So for as long as a ref variable
is in scope, the place it was bound to is frozen, together with every place containing it
and every place inside it: none of them may be assigned, moved, taken or passed by ref. The
diagnostic names the action, `cannot assign 'line.start' while 'p' refers into it`, and
reads `move`, `take` or `pass by ref` for the others.

The rule is lexical. It is decided from the text of the block, by the path of the ref and
the path of the place, and not from what the values are, which is why it also applies when
the place holds no owned value at all: a ref to a plain `Point` field freezes that field the
same way. The cost is a refusal that a value-level analysis would have allowed. The benefit
is a rule a reader can check by looking at the block. A ref parameter is not a borrow in this
sense; the caller's place is protected by the fact that the caller is suspended.

## State

`state T name` at file level declares a variable that lives in the instance rather than on
the stack and keeps its value from one call to the next. Any type is allowed: a scalar, a
struct, an `own`, an `own T?`. A state starts out zero, or `none`, when the instance is
created, and is never uninitialised. It is read like a local, assigned, in which case the old
value is freed like any other assignment, reached into with `ref`, and taken from with
`take`. It is never moved out of and never a ref. Nothing else frees what a state owns: the
value stays in the instance's heap until the state place is assigned again or the host
discards the instance, which is why the bytes an instance holds do not fall back to zero
between calls. Together with owned structs, state is what
lets a module hold a data structure of unbounded size across calls: the tree of the corpus
is built by one call, searched by another and pruned by a third, all on the same instance.

The host decides how much heap an instance has and which calls share one. A call made
without an instance runs on a fresh one that is discarded afterwards
([hosting.md](hosting.md)). Nothing in an instance ever crosses to the host as a pointer,
which is what lets the host treat it as opaque memory, copy it, or throw it away.

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
