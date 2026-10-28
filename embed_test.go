package kilang

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbed(t *testing.T) {
	t.Run("prelude_is_embedded", func(t *testing.T) {
		// The prelude is read from the embedded file system and not from the
		// disk, so ki finds it wherever the binary runs.
		data, err := fs.ReadFile(Stdlib, "stdlib/prelude.ki")
		if err != nil {
			t.Fatalf("cannot read stdlib/prelude.ki from Stdlib: %v", err)
		}
		if !strings.Contains(string(data), "ki_print_int") {
			t.Errorf("stdlib/prelude.ki is %q, want it to declare ki_print_int", data)
		}
	})
}
