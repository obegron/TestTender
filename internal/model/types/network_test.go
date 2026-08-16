package types

import "testing"

func TestNetworkDriverName(t *testing.T) {
	tests := []struct {
		driver string
		want   string
	}{
		{want: "bridge"},
		{driver: "macvlan", want: "macvlan"},
	}
	for _, tt := range tests {
		network := &Network{Driver: tt.driver}
		if got := network.DriverName(); got != tt.want {
			t.Errorf("DriverName() = %q, want %q", got, tt.want)
		}
	}
}
