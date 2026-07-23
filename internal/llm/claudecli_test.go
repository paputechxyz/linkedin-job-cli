package llm

import (
	"testing"
)

func TestParseClaudeResult_Success(t *testing.T) {
	out, err := parseClaudeResult("result", "success", "  hello  ", false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != "hello" {
		t.Errorf("got %q want hello", out)
	}
}

func TestParseClaudeResult_EmptySubtypeOK(t *testing.T) {
	// Some versions emit no subtype on success; accept that.
	if _, err := parseClaudeResult("result", "", "ok", false); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestParseClaudeResult_ErrorSubtype(t *testing.T) {
	_, err := parseClaudeResult("result", "error_during_execution", "boom", false)
	if err == nil {
		t.Fatal("want error for non-success subtype")
	}
}

func TestParseClaudeResult_IsError(t *testing.T) {
	_, err := parseClaudeResult("result", "success", "boom", true)
	if err == nil {
		t.Fatal("want error when is_error=true")
	}
}

func TestChat_ClaudeCLI(t *testing.T) {
	prev := claudeRun
	t.Cleanup(func() { claudeRun = prev })
	var gotSystem, gotUser string
	claudeRun = func(execPath, model, system, user string, maxTokens int, temperature float64) (string, error) {
		if execPath != "/fake/claude" {
			t.Errorf("execPath=%q", execPath)
		}
		gotSystem, gotUser = system, user
		return "hello from claude", nil
	}
	p := &Provider{Kind: backendClaudeCLI, Model: "sonnet", cliPath: "/fake/claude"}
	out, err := Chat(p, "sys", "ask", 100, 0)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "hello from claude" {
		t.Errorf("out=%q", out)
	}
	if gotSystem != "sys" || gotUser != "ask" {
		t.Errorf("args lost: system=%q user=%q", gotSystem, gotUser)
	}
}

// TestChat_HTTPStillDefault confirms a zero-Kind provider routes through the
// HTTP path (existing call sites and test fakes rely on this default).
func TestChat_HTTPStillDefault(t *testing.T) {
	calls := 0
	srv, p := fakeCompletions(t, "ok", 200, &calls)
	defer srv.Close()
	out, err := Chat(p, "s", "u", 10, 0)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "ok" || calls != 1 {
		t.Errorf("HTTP path not used: out=%q calls=%d", out, calls)
	}
}
