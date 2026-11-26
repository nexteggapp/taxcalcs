package golden

import (
	_ "unsafe" // for go:linkname
)

// Go's protobuf library picks, per build, from a hash of the binary,
// whether to swap two adjacent fields in the order a generated message's
// Range visits them (and whether its errors put U+00A0 after "proto:"), so
// that no one relies on either. taxyear's workings and problems come out
// in Range order, so the vectors would change with every build of this
// test. Zeroing the seed before any message is reflected over keeps Range
// in declaration order, the order the Rust port reproduces.
//
//go:linkname detrandSeed google.golang.org/protobuf/internal/detrand.randSeed
var detrandSeed uint64

func init() { detrandSeed = 0 }
