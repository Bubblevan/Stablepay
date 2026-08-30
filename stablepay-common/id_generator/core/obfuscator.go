package core

import "math/bits"

// Obfuscator provides 1-to-1 integer mapping to hide sequential numbers.
type Obfuscator struct {
	prime  int64
	offset int64
	mask   int64
}

// NewObfuscator creates a new Obfuscator with parameters.
func NewObfuscator(prime, offset, mask int64) *Obfuscator {
	return &Obfuscator{
		prime:  prime,
		offset: offset,
		mask:   mask,
	}
}

// ObfuscateWithLimit maps a sequential ID to a random-looking ID within [0, limit).
func (o *Obfuscator) ObfuscateWithLimit(seq, limit int64) int64 {
	p := uint64(o.prime)
	s := uint64(seq)
	off := uint64(o.offset)
	l := uint64(limit)

	// (seq * prime + offset) % limit
	// We use 128-bit arithmetic to avoid overflow before modulo
	hi, lo := bits.Mul64(s, p)
	lo, carry := bits.Add64(lo, off, 0)
	hi, _ = bits.Add64(hi, 0, carry)
	
	// div returns (hi, lo) / y, and (hi, lo) % y
	// we only need the remainder
	_, rem := bits.Div64(hi, lo, l)

	return int64(rem)
}
