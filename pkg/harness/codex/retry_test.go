package codex

import "testing"

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{429, true},
		{500, true},
		{503, true},
		{400, false},
		{401, false},
		{200, false},
	}
	for _, c := range cases {
		if got := isRetryable(c.status); got != c.want {
			t.Fatalf("status %d: expected %v, got %v", c.status, c.want, got)
		}
	}
}
