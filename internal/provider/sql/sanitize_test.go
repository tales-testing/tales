package sql

import "testing"

func TestMaskDSN(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                                  "",
		"postgres://user:secret@host/db":    "postgres://user:***@host/db",
		"postgresql://app:s3cret@host/db":   "postgresql://app:***@host/db",
		"mysql://user:secret@host/db":       "mysql://user:***@host/db",
		"user:pa55@tcp(127.0.0.1:3306)/db":  "user:***@tcp(127.0.0.1:3306)/db",
		"host=localhost password=topsecret": "host=localhost password=***",
		"x?token=abc123&debug=1":            "x?token=***&debug=1",
		"http://noport.example/healthz":     "http://noport.example/healthz",
	}

	for in, want := range cases {
		got := MaskDSN(in)
		if got != want {
			t.Errorf("MaskDSN(%q): want %q got %q", in, want, got)
		}
	}
}
