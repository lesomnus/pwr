package lab

import "testing"

func TestScenarios(t *testing.T) {
	if err := CheckScenarios(); err != nil {
		t.Fatal(err)
	}
}
