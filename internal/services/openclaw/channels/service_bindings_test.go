package openclawchannels

import (
	"testing"

	"chatclaw/internal/services/channels"
)

func TestOpenClawManagedAccountIDQQUsesPerChannelKey(t *testing.T) {
	if got := openClawManagedAccountID(channels.PlatformQQ, 42, `{}`); got != "channel_42" {
		t.Fatalf("expected channel_42, got %q", got)
	}
	extra := `{"app_id":"x","app_secret":"y","openclaw_channel_id":"custom_qq"}`
	if got := openClawManagedAccountID(channels.PlatformQQ, 42, extra); got != "custom_qq" {
		t.Fatalf("expected custom_qq from extra, got %q", got)
	}
}

func TestConfigBindingsPreservesEmptyArray(t *testing.T) {
	got := configBindings(map[string]any{
		"bindings": []any{},
	})
	if got == nil {
		t.Fatalf("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got len=%d", len(got))
	}
}

func TestRemoveManagedBindingsPreservesEmptyArray(t *testing.T) {
	got := removeManagedBindings(nil, "qqbot", "default")
	if got == nil {
		t.Fatalf("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got len=%d", len(got))
	}
}
