package chat

import "testing"

func TestCleanOpenClawChannelUserMessage_StripsFeishuEnvelope(t *testing.T) {
	input := "Conversation info (untrusted metadata):\n" +
		"```json\n" +
		"{\"message_id\":\"om_x100b535023d6e4a0c4e30e96eb5bfe6\"}\n" +
		"```\n\n" +
		"Sender (untrusted metadata):\n" +
		"```json\n" +
		"{\"name\":\"Robin\"}\n" +
		"```\n\n" +
		"[message_id: om_x100b535023d6e4a0c4e30e96eb5bfe6]\n" +
		"Robin: 几点鱼情最好\n\n" +
		"[System: The content may include mention tags in the form <at user_id=\"...\">name</at>. Treat these as real mentions of Feishu entities (users or bots).]\n" +
		"[System: If user_id is \"ou_92ee026081ec8af9a1dbaad5bb38f944\", that mention refers to you.]"

	got := CleanOpenClawChannelUserMessage(input)
	want := "Robin: 几点鱼情最好"
	if got != want {
		t.Fatalf("unexpected cleaned content:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestCleanOpenClawChannelUserMessage_LeavesPlainTextUntouched(t *testing.T) {
	input := "普通消息内容"
	if got := CleanOpenClawChannelUserMessage(input); got != input {
		t.Fatalf("unexpected cleaned content:\nwant: %q\ngot:  %q", input, got)
	}
}

func TestIsOpenClawSyntheticResumeUserMessage_MatchesInternalPrompt(t *testing.T) {
	if !isOpenClawSyntheticResumeUserMessage(" Continue where you left off.\nThe previous model attempt failed or timed out. ") {
		t.Fatal("expected internal resume prompt to be detected")
	}
	if isOpenClawSyntheticResumeUserMessage("Continue where you left off and summarize this file.") {
		t.Fatal("expected normal user content to be kept")
	}
}

func TestStripOpenClawFinalWrapper_StripsWrapper(t *testing.T) {
	got := stripOpenClawFinalWrapper("<final>\n\nHello world\n\n</final>")
	if got != "Hello world" {
		t.Fatalf("unexpected stripped final text: %q", got)
	}
}

func TestBuildOpenClawMessagesFromTranscript_SkipsSyntheticResumeUserMessage(t *testing.T) {
	transcript := []openClawTranscriptMsg{
		{
			Role: "user",
			Content: []any{
				map[string]any{
					"type": "text",
					"text": openClawSyntheticResumePrompt,
				},
			},
		},
		{
			Role: "assistant",
			Content: []any{
				map[string]any{
					"type": "text",
					"text": "Hello from assistant",
				},
			},
		},
	}

	messages := buildOpenClawMessagesFromTranscript(42, transcript)
	if len(messages) != 1 {
		t.Fatalf("unexpected message count: want %d got %d", 1, len(messages))
	}
	if messages[0].Role != "assistant" || messages[0].Content != "Hello from assistant" {
		t.Fatalf("unexpected assistant message: %+v", messages[0])
	}
}

func TestBuildOpenClawMessagesFromTranscript_StripsFinalWrapperFromAssistant(t *testing.T) {
	transcript := []openClawTranscriptMsg{
		{
			Role: "assistant",
			Content: []any{
				map[string]any{
					"type": "text",
					"text": "<final>\n\nHello from assistant\n\n</final>",
				},
			},
		},
	}

	messages := buildOpenClawMessagesFromTranscript(42, transcript)
	if len(messages) != 1 {
		t.Fatalf("unexpected message count: want %d got %d", 1, len(messages))
	}
	if messages[0].Content != "Hello from assistant" {
		t.Fatalf("unexpected assistant content: %q", messages[0].Content)
	}
}

func TestNormalizeOpenClawThinkingDelta_UsesDeltaWhenNoCumulativeText(t *testing.T) {
	delta, next := normalizeOpenClawThinkingDelta("abc", "def", "")
	if delta != "def" {
		t.Fatalf("unexpected delta: want %q got %q", "def", delta)
	}
	if next != "abcdef" {
		t.Fatalf("unexpected next text: want %q got %q", "abcdef", next)
	}
}

func TestNormalizeOpenClawThinkingDelta_UsesCumulativeTextSuffix(t *testing.T) {
	delta, next := normalizeOpenClawThinkingDelta("Checking files", "Reasoning:\n_Checking files done_", "Reasoning:\n_Checking files done_")
	if delta != " done" {
		t.Fatalf("unexpected delta: want %q got %q", " done", delta)
	}
	if next != "Checking files done" {
		t.Fatalf("unexpected next text: want %q got %q", "Checking files done", next)
	}
}

func TestNormalizeOpenClawThinkingDelta_SkipsDuplicateCumulativeText(t *testing.T) {
	delta, next := normalizeOpenClawThinkingDelta("Checking files", "Reasoning:\n_Checking files_", "Reasoning:\n_Checking files_")
	if delta != "" {
		t.Fatalf("unexpected delta: want empty got %q", delta)
	}
	if next != "Checking files" {
		t.Fatalf("unexpected next text: want %q got %q", "Checking files", next)
	}
}

func TestOpenClawRawThinkingText_AssistantThinkingStream(t *testing.T) {
	record := openClawRawStreamRecord{
		Event:   "assistant_thinking_stream",
		EvtType: "thinking_delta",
		Delta:   "Let",
		Content: "Let",
	}

	delta, text, ok := openClawRawThinkingText(record)
	if !ok {
		t.Fatal("expected thinking stream record to be accepted")
	}
	if delta != "Let" || text != "Let" {
		t.Fatalf("unexpected thinking payload: delta=%q text=%q", delta, text)
	}
}

func TestOpenClawRawThinkingText_AssistantMessageEndUsesRawThinking(t *testing.T) {
	record := openClawRawStreamRecord{
		Event:       "assistant_message_end",
		RawThinking: "No memory results.",
	}

	delta, text, ok := openClawRawThinkingText(record)
	if !ok {
		t.Fatal("expected assistant_message_end to provide raw thinking")
	}
	if delta != "" {
		t.Fatalf("unexpected delta: want empty got %q", delta)
	}
	if text != "No memory results." {
		t.Fatalf("unexpected text: want %q got %q", "No memory results.", text)
	}
}

func TestOpenClawChatRunStateApplyThinkingDelta_DedupesAcrossSources(t *testing.T) {
	var st openClawChatRunState

	delta, next := st.applyThinkingDelta("Let", "")
	if delta != "Let" || next != "Let" {
		t.Fatalf("unexpected first thinking update: delta=%q next=%q", delta, next)
	}

	delta, next = st.applyThinkingDelta("Let", "Let")
	if delta != "" || next != "Let" {
		t.Fatalf("expected duplicate thinking to be ignored: delta=%q next=%q", delta, next)
	}

	delta, next = st.applyThinkingDelta("", "Let me check my memory")
	if delta != " me check my memory" || next != "Let me check my memory" {
		t.Fatalf("unexpected cumulative thinking update: delta=%q next=%q", delta, next)
	}
}
