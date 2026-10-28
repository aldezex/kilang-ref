package sema

// VarOrigin returns the origin that the table recorded for the variable v: where
// it was born and why. The record of a variable (TypeTable.vars) is not
// exported, so this file, which is part of package sema, hands it to the tests
// of package sema_test, which cannot see it. It is compiled only for tests.
func VarOrigin(t *TypeTable, v TypeVarID) Origin {
	return t.vars.Get(v).Origin
}
