//go:build xgrammar

package xgrammar

import "testing"

// BenchmarkErrBufPoolGet measures the round-trip cost of borrowing
// and returning an error scratch buffer. This drives the decision of
// whether to keep zero-filling buffers on Get; if the cost is
// negligible the simpler "always-zero on Get" policy wins.
func BenchmarkErrBufPoolGet(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bp := getErrBuf()
		putErrBuf(bp)
	}
}
