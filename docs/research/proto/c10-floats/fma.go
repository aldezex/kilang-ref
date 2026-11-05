package c10floats

// Naive computes x*y + z in one expression. The Go spec lets a compiler fuse
// the multiplication and the addition into one instruction with a single
// rounding (a fused multiply-add, FMA), so the result may differ in the last
// bit from the unfused one. Go has no flag to turn that off.
//
//go:noinline
func Naive(x, y, z float64) float64 {
	return x*y + z
}

// Explicit computes the same value but converts the product to float64 before
// the addition. The spec says an explicit conversion rounds, so the compiler
// may not fuse across it: this is the way to write a float operation that
// gives the same bits on every platform.
//
//go:noinline
func Explicit(x, y, z float64) float64 {
	return float64(x*y) + z
}
