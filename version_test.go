package main

import "testing"

func TestModuleVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tagged release", "v0.2.0", "0.2.0"},
		{"pseudo version", "v0.0.0-20261015052528-872a2cd4ad39", "0.0.0-20261015052528-872a2cd4ad39"},
		{"dirty tagged release", "v0.2.0+dirty", "0.2.0+dirty"},
		{"development build", "(devel)", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moduleVersion(tt.in); got != tt.want {
				t.Errorf("moduleVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
