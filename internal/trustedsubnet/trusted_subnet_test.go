package trustedsubnet

import "testing"

func TestChecker(t *testing.T) {
	checker, err := NewChecker(" 192.168.1.0/24 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !checker.Enabled() {
		t.Fatal("expected checker to be enabled")
	}

	if !checker.Allows(" 192.168.1.42 ") {
		t.Fatal("expected matching IP to be allowed")
	}
	if checker.Allows("10.0.0.7") {
		t.Fatal("expected IP outside subnet to be rejected")
	}
	if checker.Allows("") {
		t.Fatal("expected empty IP to be rejected")
	}
}

func TestCheckerDisabledForEmptyCIDR(t *testing.T) {
	checker, err := NewChecker("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checker.Enabled() {
		t.Fatal("expected checker to be disabled")
	}
	if !checker.Allows("") {
		t.Fatal("expected disabled checker to allow requests")
	}
}

func TestNewCheckerRejectsInvalidCIDR(t *testing.T) {
	if _, err := NewChecker("192.168.1.0"); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}
