package cmd

import "testing"

func TestValidateLoopbackListenAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:2475", "[::1]:2475"} {
		if err := validateLoopbackListenAddress(address); err != nil {
			t.Errorf("expected %s to be accepted: %v", address, err)
		}
	}
	for _, address := range []string{":2475", "0.0.0.0:2475", "localhost:2475", "10.0.0.1:2475", "invalid"} {
		if err := validateLoopbackListenAddress(address); err == nil {
			t.Errorf("expected %s to be rejected", address)
		}
	}
}
