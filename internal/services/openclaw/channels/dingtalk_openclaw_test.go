package openclawchannels

import "testing"

func TestContainsDingTalkPluginMarker(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{
			name: "matches package marker",
			out:  "installed plugin: @dingtalk-real-ai/dingtalk-connector",
			want: true,
		},
		{
			name: "matches channel id",
			out:  "channel dingtalk-connector registered",
			want: true,
		},
		{
			name: "rejects unrelated output",
			out:  "installed plugin: @example/other-plugin",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsDingTalkPluginMarker(tt.out); got != tt.want {
				t.Fatalf("containsDingTalkPluginMarker(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}

func TestIsDingTalkPluginSecurityScanBlocked(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "matches dangerous code detected",
			msg:  `Plugin "dingtalk-connector" installation blocked: dangerous code patterns detected`,
			want: true,
		},
		{
			name: "matches warning phrasing",
			msg:  `WARNING: Plugin "dingtalk-connector" contains dangerous code patterns`,
			want: true,
		},
		{
			name: "rejects unrelated install failure",
			msg:  "npm err! 404 not found",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDingTalkPluginSecurityScanBlocked(tt.msg); got != tt.want {
				t.Fatalf("isDingTalkPluginSecurityScanBlocked(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestIsDingTalkPluginInstallRateLimited(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "matches plain rate limit error",
			msg:  "rate limit exceeded, retry later",
			want: true,
		},
		{
			name: "matches 429 response",
			msg:  "request failed with status 429",
			want: true,
		},
		{
			name: "rejects unrelated install failure",
			msg:  "npm err! 404 not found",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDingTalkPluginInstallRateLimited(tt.msg); got != tt.want {
				t.Fatalf("isDingTalkPluginInstallRateLimited(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}
