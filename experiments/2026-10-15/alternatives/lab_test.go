package lab

import "testing"

func TestBackends(t *testing.T) {
	if e := Check(); e != nil {
		t.Fatal(e)
	}
}
