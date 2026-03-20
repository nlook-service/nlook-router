package agentproxy

import (
	"reflect"
	"testing"
)

func TestSanitizeArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"safe args", []string{"--print", "--model", "opus", "fix bug"}, []string{"--print", "--model", "opus", "fix bug"}},
		{"removes semicolon injection", []string{"--task", "fix; rm -rf /"}, []string{"--task"}},
		{"removes pipe", []string{"hello | cat /etc/passwd"}, []string{}},
		{"removes backtick", []string{"`whoami`"}, []string{}},
		{"removes dollar sign", []string{"${HOME}"}, []string{}},
		{"empty input", []string{}, []string{}},
		{"allows dashes and equals", []string{"--max-budget-usd=5.0"}, []string{"--max-budget-usd=5.0"}},
		{"allows hash for issue refs", []string{"Fix #123"}, []string{"Fix #123"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SanitizeArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestIsCommandAllowed(t *testing.T) {
	allowed := []string{"claude", "git", "npm"}

	tests := []struct {
		cmd  string
		want bool
	}{
		{"claude", true},
		{"git", true},
		{"/usr/local/bin/claude", true},
		{"rm", false},
		{"bash", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := IsCommandAllowed(tt.cmd, allowed); got != tt.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
