package sema

// VarOrigin returns the origin that the table recorded for the variable v: where
// it was born and why. The record of a variable (TypeTable.vars) is not
// exported, so this file, which is part of package sema, hands it to the tests
// of package sema_test, which cannot see it. It is compiled only for tests.
func VarOrigin(t *TypeTable, v TypeVarID) Origin {
	return t.vars.Get(v).Origin
}

// SetPatternBudget sets how many steps the pass "patterns" may take for one `match`
// or `let` (patternBudget) and returns the function that puts the old value back.
// The budget is a variable of the package so that the tests of package sema_test
// can lower it, to reach E0707 with a few arms and to find where it begins; it is
// compiled only for tests.
func SetPatternBudget(n int) (restore func()) {
	old := patternBudget
	patternBudget = n
	return func() { patternBudget = old }
}
