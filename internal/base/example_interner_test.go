package base_test

import (
	"fmt"

	"github.com/aldezex/kilang/internal/base"
)

func ExampleInterner() {
	in := base.NewInterner()

	fib := in.InternString("fib")
	main := in.InternString("main")
	again := in.InternString("fib")

	fmt.Println(fib, main, again)
	fmt.Println(fib == again, fib == main)
	fmt.Println(in.String(main), in.Len())

	_, ok := in.Lookup([]byte("nope"))
	fmt.Println(ok, in.Len())
	// Output:
	// 1 2 1
	// true false
	// main 2
	// false 2
}
