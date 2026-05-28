package chrome

import "testing"

func TestParseFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in        string
		wantName  string
		wantValue any
	}{
		{"--disable-gpu", "disable-gpu", true},
		{"disable-gpu", "disable-gpu", true},
		{"--proxy-server=http://proxy.local:3128", "proxy-server", "http://proxy.local:3128"},
		{"--user-agent=Tales/0.1 (linux)", "user-agent", "Tales/0.1 (linux)"},
		{"-no-sandbox", "no-sandbox", true},
		{"foo=", "foo", ""},
		{"--equal=a=b=c", "equal", "a=b=c"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			gotName, gotValue := parseFlag(tc.in)
			if gotName != tc.wantName {
				t.Errorf("name = %q, want %q", gotName, tc.wantName)
			}

			if gotValue != tc.wantValue {
				t.Errorf("value = %#v, want %#v", gotValue, tc.wantValue)
			}
		})
	}
}
