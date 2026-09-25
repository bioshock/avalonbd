// Package money formats integer BDT amounts.
package money

import "strconv"

// Format renders 1200 as "৳ 1,200" (taka sign, space, comma thousands).
func Format(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-৳ " + string(out)
	}
	return "৳ " + string(out)
}
