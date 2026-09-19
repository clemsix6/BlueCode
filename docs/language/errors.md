# Errors and faults

## Declaring and raising

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

`error Name` at file level declares an error. Errors are numbered from 1 in declaration
order and their names go into the manifest; an error carries no data. A function whose
results are followed by `!` can fail: `return error.Name` leaves it with that error instead
of its values. A function without `!` cannot return an error.

## Handling

A call to a function that can fail is never bare. The caller picks one of three forms, each
applied to the whole call:

| Form | Effect | Allowed in |
|---|---|---|
| `try f(...)` | the values on success; on failure, leaves the function with the same error and one more frame in the trace | a function marked `!` |
| `f(...) catch value` | the value on success, `value` on failure; the function goes on | any function; `f` returns exactly one value and `value` is of its type |
| `f(...) catch err:` + block | the values on success; on failure the block runs with `err`, of type `error`, and must leave the function | any function |

The block of `catch err:` always leaves: it ends on a `return`, or on an `if` with an `else`
whose every arm does. So the lines after it only run with the call's values in hand. Inside
the block, `return err` passes the failure on unchanged (the function must be `!`),
`return error.Other` replaces it, and `return v` settles it with a value. `err` compares with
`==` and `!=` to `error.Name` and to nothing else, and cannot be a condition by itself.

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

`try` and `catch err:` also apply to a call returning several values, received by a
multi-declaration; `catch value` needs a call returning exactly one.

## Traces

Every failure records a trace: the function and line where the error was raised, then one
frame per function that passed it on with `try` or `return err`, up to the external function
the host called. A `catch err:` block that returns another error appends to the trace of the
one it caught, so the host sees both, the last raised first and the earlier one as its cause.
The host reads function names and line numbers through the manifest ([host.md](host.md)).

## Faults

A fault is what the code must never do. It is not an error: nothing catches it, the call
ends at once, every result is zero, and the host gets a negative gas naming the fault, with a
trace from the faulting line.

| Fault | When |
|---|---|
| `division by zero` | `/` or `%` by zero |
| `overflow` | `+`, `-`, `*` or `-x` whose result does not fit; the `int` minimum divided by `-1` |
| `too deep` | calls nested past the depth limit of the manifest |
| `out of memory` | `new` when the instance's heap has no room left ([ownership.md](ownership.md)) |
| `out of gas` | the gas meter ([host.md](host.md)) |

Check before computing: `if b != 0:` before `a / b`, `if a >= b:` before `a - b` on uints.
`and` does not short-circuit, so the check is an `if`, not a condition joined with `and`.

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
