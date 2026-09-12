package money

import "testing"

func TestFormat(t *testing.T) {
	for n, want := range map[int]string{0: "৳ 0", 650: "৳ 650", 1200: "৳ 1,200", 1234567: "৳ 1,234,567"} {
		if got := Format(n); got != want {
			t.Errorf("Format(%d) = %q, want %q", n, got, want)
		}
	}
}
