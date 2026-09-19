# Errors and faults

A BlueCode function fails in one of two ways, and the language keeps them apart on purpose.
An error is a value the program declared and returns instead of its results, which a caller
must handle and which the host receives by name. A fault is what the program was not allowed
to do at all: divide by zero, overflow, recurse past the limit, exhaust its memory. A fault
is not caught. The call ends where it happened and the host is told which fault, and where.
Both leave a trace, so that the host, and the person reading the host's log, know the line
and the chain of calls without a debugger and without the module having any way to print.

The design follows Zig more than anything else. There is no exception, no unwinding and no
hidden control flow: a failing call is written differently from one that cannot fail, and the
three ways of handling a failure are three keywords at the call site. Errors carry no message
and no payload, only their identity, which is what a program deciding what to do next needs
and what a host turns into a message with the manifest.

## Declaring errors

```bluecode
struct Account:
    uint id
    uint balance
    bool frozen


error Frozen
error Insufficient


def uint! withdraw (ref Account account, uint amount):
    if account.frozen:
        return error.Frozen
    if account.balance < amount:
        return error.Insufficient
    account.balance = account.balance - amount
    return account.balance
```

`error Name` at file level declares an error. Errors are numbered from 1 in the order of
their declarations and the manifest gives the host the names back ([modules.md](modules.md));
zero is never an error, it is what a successful call reports in its error field. Two
declarations of one name are refused. There is no hierarchy of errors and no error with
fields. A program that needs to return a reason and a value returns two values.

A function that may fail says so with a `!` after its results: `def uint! withdraw`, `def ()!
deposit`, `def (uint, uint)! split`. Inside it, `return error.Name` leaves the function with
that error and no values. A `()!` function may still fall off its
end, which is its success path. A function without `!` cannot return an error, and
`'cannot_fail' cannot fail: mark it with '!' or handle the case` reports the attempt. The choice the message
offers is the real one: a function either declares that it fails or settles its failures
inside.

## Handling a failure

A call to a function marked `!` is never bare. Writing `withdraw(ref a, 10)` as if it could
not fail is refused, with a message naming the options available where the call stands: `the
call can fail: use 'try' or 'catch'` inside a function that can fail, `the call can fail: use
'catch'` inside one that cannot. Three forms apply to the whole call, and each decides what
becomes of the failure.

| Form | On success | On failure | Allowed where |
|---|---|---|---|
| `try f(...)` | the values | the function returns the same error, with one more frame in the trace | in a function marked `!`; the only form allowed inside an expression or after `return` |
| `f(...) catch value` | the value | `value` stands in and the function goes on | on the whole value of a declaration or an assignment; `f` returns exactly one value, of `value`'s type |
| `f(...) catch err:` + block | the values | the block runs with `err` bound and must leave the function | on the whole value of a declaration or an assignment, or on a call statement |

The two `catch` forms are statements in disguise: they follow a call that is the entire
value of a declaration or an assignment, or the entire statement, and nowhere else. A
`catch` inside an expression or after `return` is refused, so a failure that must be settled
before returning is settled in a declaration first. `try` is an expression and goes anywhere
a value goes, `return try f(...)` included.

`try` is the relay. The failure goes to the caller's caller unchanged, with the line of the
`try` recorded on the way. It is only allowed in a function that can fail itself, and only on
a call that can fail: `try` on a plain call is `the call cannot fail`, and `try` in a plain
function is `'try' needs a function marked '!': settle the failure here with 'catch'`.

`catch value` settles the failure on the spot with a substitute. It needs a call that returns
exactly one value, so that the substitute has a place to go. On a `()!` function or one
returning two values it is refused with `'catch' with a value needs a call that returns one
value, this one returns 0`, and the substitute must have the value's type.

`catch err:` opens a block that sees the failure as `err`, a value of type `error`. The
block must leave the function: its last statement is a `return`, or an `if` with an `else`
whose every arm returns, exactly the rule [functions.md](functions.md) gives for a function's
body, and `the catch block must leave the function` reports a block that could fall through.
That constraint is what makes the form worth its keyword. The lines after the block only run
when the call succeeded, so the values the call produced are real values there, and a reader
never has to ask whether a variable holds a result or a leftover. Inside the block, `return
err` passes the same failure on, with a frame added, and needs a `!` function, and whatever
the block itself raised and caught before that line leaves no frame behind; `return
error.Other` replaces the failure with another, and the trace keeps the first as its cause;
`return v` settles it with a value. The block's `err` is compared with `==` and `!=` to
`error.Name`, and to nothing else. `if err:` is refused because an error is not a bool, and
`err < err` because errors are not ordered.

```bluecode
error Frozen
error Insufficient
error PayrollUnfunded


def uint! withdraw (uint balance, uint amount):
    if balance < amount:
        return error.Insufficient
    return balance - amount


def ()! deposit (uint balance, uint amount):
    if balance == 0:
        return error.Frozen


def uint! transfer (uint balance, uint amount):
    uint left = try withdraw(balance, amount)
    try deposit(balance, amount)
    return left


def uint! pay_salary (uint balance, uint salary):
    uint left = transfer(balance, salary) catch err:
        if err == error.Insufficient:
            return error.PayrollUnfunded
        return err
    return left


def uint left_after (uint balance, uint amount):
    uint left = withdraw(balance, amount) catch 0
    return left


def bool credit (uint balance, uint amount):
    deposit(balance, amount) catch err:
        return false
    return true
```

`try` and `catch err:` work on a call returning several values, received by a
multi-declaration such as `uint a, uint b = try split(n)`, and on a call in statement
position, `try deposit(...)`. `catch value` needs one value. A failure cannot be ignored,
stored or inspected after the fact: there is no error variable outside a catch block, and a
call statement that can fail is wrapped like any other.

A function that cannot fail may call one that can, as `left_after` and `credit` do. It
settles every failure itself, with a value or a block. This is what keeps `!` honest: it is
not transitive by default, and a reader who sees a function without `!` knows that nothing
it calls can make it fail.

## Traces

Every failure records a trace as it travels. The frame at the origin holds the function, the
line of the `return error.Name` and the error's code. Every function that passes the failure
on with `try` or `return err` adds a frame with its own line and a zero code, and the trace
ends at the external function the host called. When a catch block returns a different error,
the new frame is appended after the frames of the error it caught, so the trace holds both,
and the host prints the last raised first and the earlier one as its cause:

```
PayrollUnfunded
    payroll.bc:31 pay_salary
caused by Insufficient
    payroll.bc:8 withdraw
    payroll.bc:19 transfer
```

A `catch value` does not touch the trace, so the frames of the failure it swallowed stay
where they are, and a later failure in the same call shows them as its cause. The trace
lives in memory the host provides and is only written on failure paths, so a call that
succeeds never touches it. Its capacity is bounded by the depth limit, and past it the
count keeps growing while the frames stop, so the host knows how many were lost.
[hosting.md](hosting.md) gives the layout.

## Faults

A fault is a condition the language forbids rather than reports. When one happens, the
function that faulted records a one-frame trace with the line, returns zero values and a
negative gas that names the fault, and every caller up to the entry does the same after
adding its own frame, so the host receives the whole chain. No catch sees a fault: `catch`
handles errors, which are values, and a fault is the absence of a value.

| Fault | When |
|---|---|
| `division by zero` | `/` or `%` with a zero divisor |
| `overflow` | `+`, `-`, `*` or `-x` whose result does not fit; the `int` minimum divided by `-1` |
| `too deep` | a call nested past the depth limit the manifest states |
| `out of memory` | `new` when the instance's heap has no room left ([ownership.md](ownership.md)) |
| `out of gas` | reserved for the gas meter; nothing charges gas yet, so no module produces it ([hosting.md](hosting.md)) |

The consequence for the author is that arithmetic on untrusted input is checked before it is
done. `if b != 0:` guards `a / b`, and `if a >= b:` guards `a - b` on a `uint`. Since `and`
evaluates both sides ([syntax.md](syntax.md)), the guard has to be an `if` and not a
condition joined with `and`. The corpus checks every subtraction on a balance before it
performs it, which is the pattern for any function that must refuse rather than fault.

A `too deep` fault happens on entry, before any line of the function runs, and is reported on
the line of the function's declaration. The depth limit is a constant of the language written
into the manifest, not a choice of the host, so the same program faults at the same depth
everywhere.

## Refused

| Message | Cause |
|---|---|
| `'cannot_fail' cannot fail: mark it with '!' or handle the case` | `return error.X` in a function without `!` |
| `unknown error 'Empty'` | |
| `error 'Frozen' is already declared` | |
| `the call can fail: use 'try' or 'catch'` | a bare call to a `!` function from a `!` function |
| `the call can fail: use 'catch'` | a bare call to a `!` function from a plain function |
| `'try' needs a function marked '!': settle the failure here with 'catch'` | |
| `the call cannot fail` | `try` or `catch` on a function without `!` |
| `the catch block must leave the function` | |
| `the fallback should be uint, got bool` | |
| `'catch' with a value needs a call that returns one value, this one returns 0` | `catch value` on a `()!` function or one with several results |
| `condition must be bool, got error` | `if err:` |
| `an error can only be compared with '==' or '!='` | `err < err` |
| `'try' needs a call` | `try x` |
| `'catch' needs a call` | `x catch 0` |
