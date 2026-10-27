// This program must NOT compile. TestIdVec/distinct_id_types_do_not_mix builds
// it and expects the compiler to reject the call below: an IdVec[FileID, ...]
// accepts a FileID and nothing else, even though otherID has the same
// underlying type.
package main

import "github.com/aldezex/kilang/internal/base"

type otherID uint32

func main() {
	var v base.IdVec[base.FileID, string]
	var i otherID
	_ = v.Get(i)
}
