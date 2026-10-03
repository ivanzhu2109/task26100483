package vmplanner

// makeRatio normalizes n/d (d must be > 0, n >= 0).
func makeRatio(n, d int64) Ratio {
	if n == 0 {
		return Ratio{Num: 0, Den: 1}
	}
	g := gcd(n, d)
	return Ratio{Num: n / g, Den: d / g}
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// less reports whether r < s, exactly, via integer cross-multiplication.
func (r Ratio) less(s Ratio) bool {
	return r.Num*s.Den < s.Num*r.Den
}

// geq reports whether r >= s, exactly.
func (r Ratio) geq(s Ratio) bool {
	return !r.less(s)
}

// Float64 returns a floating-point approximation for display only.
func (r Ratio) Float64() float64 {
	return float64(r.Num) / float64(r.Den)
}
