package beanfun

// Zero overwrites every byte of b with 0. Exposed as a package-level
// helper so callers in other packages (e.g. internal/launcher) can
// zero an OTP / token slice after use without re-implementing the
// loop. Inlined by the compiler.
//
// Strings can't be zeroed (they're immutable and may live in
// .rodata). Keep secrets in []byte for the entire lifecycle, then
// call Zero before the slice falls out of reach.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
