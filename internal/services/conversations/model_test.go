package conversations

import "testing"

func TestNormalizeConversationSource_DefaultsToManual(t *testing.T) {
	got := NormalizeConversationSource("")
	if got != ConversationSourceManual {
		t.Fatalf("expected default conversation source %q, got %q", ConversationSourceManual, got)
	}
}

func TestNormalizeConversationSource_RecognizesOpenClawCron(t *testing.T) {
	got := NormalizeConversationSource(" openclaw_cron ")
	if got != ConversationSourceOpenClawCron {
		t.Fatalf("expected %q, got %q", ConversationSourceOpenClawCron, got)
	}
}
