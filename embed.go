// Package kilang is the root of the module. It holds what has to sit at the
// top of the tree: the files that ki carries inside its binary.
package kilang

import "embed"

// Stdlib is the standard library as source, embedded in the binary: today only
// stdlib/prelude.ki. ki reads the prelude from here and not from the disk, so it
// finds it wherever the binary runs.
//
// This file lives at the root of the module because an embed pattern cannot
// reach a parent directory: `stdlib` has to be below the directory of the file
// that names it. The pattern grows with the standard library.
//
//go:embed stdlib
var Stdlib embed.FS
