package postgres

import "testing"

// The pool size goes in the address as pool_max_conns (docs/OPERATIONS.md
// section 9) and must not reach PostgreSQL, which refuses a connection that
// names a setting it does not know.
func TestPoolSizeComesOutOfTheAddress(t *testing.T) {
	for _, c := range []struct {
		in, out string
		n       int
		bad     bool
	}{
		{"postgres://u:p@db/churr", "postgres://u:p@db/churr", 20, false},
		{"postgres://u:p@db/churr?pool_max_conns=60", "postgres://u:p@db/churr", 60, false},
		{"postgres://u:p@db/churr?sslmode=verify-full&pool_max_conns=5", "postgres://u:p@db/churr?sslmode=verify-full", 5, false},
		{"postgres://u:p@db/churr?pool_max_conns=many", "", 0, true},
		{"postgres://u:p@db/churr?pool_max_conns=0", "", 0, true},
	} {
		out, n, err := poolSize(c.in)
		if (err != nil) != c.bad || out != c.out || n != c.n {
			t.Errorf("poolSize(%q) = %q, %d, %v; want %q, %d, failing %v", c.in, out, n, err, c.out, c.n, c.bad)
		}
	}
}
