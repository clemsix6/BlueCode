package tests

import (
	"testing"
	"unsafe"

	"bluecode/runtime"
	"bluecode/tests/harness"
)

// syntaxGas is the budget every call of this file is given.
const syntaxGas = 1000

// syntaxPair is the results block of a function returning two integers.
type syntaxPair struct {
	Low  int64
	High int64
}

// syntaxValueAndError is the results block of a function marked '!' that returns one value:
// the value first, the error last, zero when the call succeeded.
type syntaxValueAndError struct {
	Value uint64
	Err   int64
}

// syntaxInt calls a function whose parameters and single result are 64-bit integers.
func syntaxInt(t *testing.T, pod *bluecode.Pod, name string, args ...uint64) uint64 {
	t.Helper()

	var result uint64
	entry := harness.Function(t, pod, name)

	var block unsafe.Pointer
	if len(args) > 0 {
		block = unsafe.Pointer(&args[0])
	}

	if _, err := entry.Call(block, unsafe.Pointer(&result), syntaxGas); err != nil {
		t.Fatalf("%s%v: %v", name, args, err)
	}

	return result
}

// syntaxSize checks a Go mirror against the block the manifest describes, which is the whole
// of what makes passing a Go value by address safe.
func syntaxSize(t *testing.T, block bluecode.Block, size uintptr, what string) {
	t.Helper()

	if int(size) != block.Size {
		t.Errorf("%s mirrors a block of %d bytes but is %d", what, block.Size, size)
	}
}

// The three ways of writing what a function returns, plus the function that returns nothing
// and is called only for what it does.
func TestSyntaxReturnTypeForms(t *testing.T) {
	pod := harness.Build(t, "programs/syntax.bc")

	if got := syntaxInt(t, pod, "bare_type"); got != 7 {
		t.Errorf("bare_type = %d", got)
	}

	entry := harness.Function(t, pod, "parenthesised_types")
	syntaxSize(t, entry.Results, unsafe.Sizeof(syntaxPair{}), "syntaxPair")

	var pair syntaxPair
	if _, err := entry.Call(nil, unsafe.Pointer(&pair), syntaxGas); err != nil {
		t.Fatalf("parenthesised_types: %v", err)
	}
	if pair != (syntaxPair{1, 2}) {
		t.Errorf("parenthesised_types = %v", pair)
	}

	if got := syntaxInt(t, pod, "relays_a_call_returning_nothing"); got != 1 {
		t.Errorf("relays_a_call_returning_nothing = %d", got)
	}
}

// A function with no parameters and no results has two empty blocks, so the host passes no
// memory at all and still gets its gas back.
func TestSyntaxNoArgumentsAndNoResults(t *testing.T) {
	pod := harness.Build(t, "programs/syntax.bc")

	entry := harness.Function(t, pod, "no_results")
	if entry.Args.Size != 0 || entry.Results.Size != 0 {
		t.Fatalf("no_results: args %d bytes, results %d bytes", entry.Args.Size, entry.Results.Size)
	}

	gasLeft, err := entry.Call(nil, nil, syntaxGas)
	if err != nil || gasLeft != syntaxGas {
		t.Errorf("no_results: gas %d, %v", gasLeft, err)
	}
}

// '!' may sit anywhere between the return types and the name. It changes the results block and
// nothing else: a function that never fails leaves the error field zero.
func TestSyntaxBangSpacings(t *testing.T) {
	pod := harness.Build(t, "programs/syntax.bc")

	withValue := map[string]uint64{"bang_tight": 1, "bang_loose": 2, "bang_after_one_type": 3}
	for name, want := range withValue {
		entry := harness.Function(t, pod, name)
		syntaxSize(t, entry.Results, unsafe.Sizeof(syntaxValueAndError{}), "syntaxValueAndError")

		var results syntaxValueAndError
		if _, err := entry.Call(nil, unsafe.Pointer(&results), syntaxGas); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if results != (syntaxValueAndError{want, 0}) {
			t.Errorf("%s = %v", name, results)
		}
	}

	for _, name := range []string{"bang_after_empty_types", "bang_alone"} {
		var raised int64
		if _, err := harness.Function(t, pod, name).Call(nil, unsafe.Pointer(&raised), syntaxGas); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if raised != 0 {
			t.Errorf("%s raised %d", name, raised)
		}
	}
}

// Declarations, assignment and what a block does to a name.
func TestSyntaxDeclarationsAndBlocks(t *testing.T) {
	pod := harness.Build(t, "programs/syntax.bc")

	if got := syntaxInt(t, pod, "multi_value_declaration"); got != 12 {
		t.Errorf("multi_value_declaration = %d", got)
	}
	if got := syntaxInt(t, pod, "reassignment", 5); got != 12 {
		t.Errorf("reassignment(5) = %d", got)
	}
	if got := syntaxInt(t, pod, "inner_block_shadows_outer", 3); got != 300 {
		t.Errorf("inner_block_shadows_outer(3) = %d, the inner name should win inside the block", got)
	}
	if got := syntaxInt(t, pod, "inner_block_shadows_outer", ^uint64(2)); got != ^uint64(2) {
		t.Errorf("inner_block_shadows_outer(-3) = %d, the outer name should survive the block", int64(got))
	}
	if got := syntaxInt(t, pod, "field_chain_assignment", 4); got != 12 {
		t.Errorf("field_chain_assignment(4) = %d", got)
	}
}

// Parentheses, the postfix chain, chained comparison, casts and how a literal picks its type.
func TestSyntaxExpressions(t *testing.T) {
	pod := harness.Build(t, "programs/syntax.bc")

	if got := syntaxInt(t, pod, "parentheses_regroup", 1, 2, 3); got != 9 {
		t.Errorf("parentheses_regroup(1, 2, 3) = %d", got)
	}
	if got := syntaxInt(t, pod, "field_of_a_fresh_value", 5); got != 15 {
		t.Errorf("field_of_a_fresh_value(5) = %d", got)
	}
	if got := syntaxInt(t, pod, "cast_round_trip", ^uint64(0)); got != ^uint64(0) {
		t.Errorf("cast_round_trip(-1) = %d", got)
	}
	if got := syntaxInt(t, pod, "cast_back", ^uint64(0)); got != ^uint64(0) {
		t.Errorf("cast_back(MaxUint64) = %d", int64(got))
	}
	if got := syntaxInt(t, pod, "literal_takes_its_hint_from_the_other_operand", 5); got != 10 {
		t.Errorf("2 * 5 = %d", got)
	}
	if got := syntaxInt(t, pod, "largest_literal"); got != ^uint64(0) {
		t.Errorf("largest_literal = %d", got)
	}

	syntaxChainedComparison(t, pod)
}

// syntaxChainedComparison checks that comparisons chain to the left: the first one produces
// the bool the second one compares.
func syntaxChainedComparison(t *testing.T, pod *bluecode.Pod) {
	t.Helper()

	entry := harness.Function(t, pod, "chained_comparison")

	checks := []struct {
		a, b int64
		c    bool
		want bool
	}{
		{3, 3, true, true},
		{3, 3, false, false},
		{3, 4, true, false},
		{3, 4, false, true},
	}

	for _, check := range checks {
		args := struct {
			A, B int64
			C    bool
		}{check.a, check.b, check.c}

		var answer bool
		if _, err := entry.Call(unsafe.Pointer(&args), unsafe.Pointer(&answer), syntaxGas); err != nil {
			t.Fatalf("chained_comparison%v: %v", check, err)
		}
		if answer != check.want {
			t.Errorf("%d == %d == %t is %t", check.a, check.b, check.c, answer)
		}
	}
}

// Only 22 words are reserved, and the names of the built-in types are not among them: they are
// ordinary global names an inner scope may shadow like any other.
func TestSyntaxUnreservedWords(t *testing.T) {
	pod := harness.Build(t, "programs/syntax.bc")

	if got := syntaxInt(t, pod, "unreserved_words"); got != 1023 {
		t.Errorf("unreserved_words = %d", got)
	}
	if got := syntaxInt(t, pod, "type_name_as_a_local"); got != 567 {
		t.Errorf("type_name_as_a_local = %d", got)
	}
	if got := syntaxInt(t, pod, "underscores_are_letters", 9); got != 9 {
		t.Errorf("underscores_are_letters(9) = %d", got)
	}
	if got := syntaxInt(t, pod, "last_line_of_the_file", 1); got != 2 {
		t.Errorf("last_line_of_the_file(1) = %d", got)
	}
}
