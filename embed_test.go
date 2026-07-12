package prompts

import (
	"strings"
	"testing"
)

// The expected strings below are what v1's Python produces for these blocks
// (verified against orchestrator/fusion/{judge,review}.py). They are TEST
// FIXTURES asserting loader correctness — the frozen source of truth remains
// prompts/*.tmpl, guarded by make prompts-check.

func render(t *testing.T, stem string, n int, vars map[string]string) string {
	t.Helper()
	tpl, err := Block(stem, n)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tpl.Render(vars)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestJudgeSystemPrompt(t *testing.T) {
	got := render(t, "judge", 2, nil)
	want := "You are a senior technical judge evaluating a software implementation. Score objectively on a 1-10 scale."
	if got != want {
		t.Fatalf("judge system prompt:\n got %q\nwant %q", got, want)
	}
}

func TestJudgeUserPrompt(t *testing.T) {
	got := render(t, "judge", 3, map[string]string{
		"brief":                                  "BRIEF",
		"implementation":                         "IMPL",
		"_test_report_prompt_block(test_report)": "TESTBLOCK\n\n",
	})
	if !strings.HasPrefix(got, "IMPLEMENTATION BRIEF (specification):\nBRIEF\n\nIMPLEMENTATION (code to evaluate):\nIMPL\n\nTESTBLOCK\n\nScore this implementation on three dimensions, each on a scale of 1 to 10:\n\n") {
		t.Fatalf("judge user prompt head wrong:\n%q", got[:min(len(got), 220)])
	}
	if !strings.HasSuffix(got, "PASS if the average of the three scores is >= 8.0, FAIL otherwise.") {
		t.Fatalf("judge user prompt tail wrong:\n%q", got[max(0, len(got)-120):])
	}
	if !strings.Contains(got, "req: <score 1-10>\nsec: <score 1-10>\nmaint: <score 1-10>\nverdict: PASS or FAIL\n") {
		t.Fatalf("judge format block missing:\n%q", got)
	}
}

func TestJudgeTestReportBlock(t *testing.T) {
	got := render(t, "judge", 1, map[string]string{"status": "GREEN (all passing)"})
	want := "TEST REPORT (deterministic evidence from the test runner, not a model):\nstatus: GREEN (all passing)\n"
	if got != want {
		t.Fatalf("test-report block:\n got %q\nwant %q", got, want)
	}
}

func TestReviewPrompts(t *testing.T) {
	sys := render(t, "review", 1, nil)
	if sys != "You are a senior code reviewer. Your job is to find defects in an implementation relative to its specification." {
		t.Fatalf("review system prompt: %q", sys)
	}
	user := render(t, "review", 2, map[string]string{"brief": "B", "implementation": "I"})
	if !strings.HasPrefix(user, "IMPLEMENTATION BRIEF (what was supposed to be built):\nB\n\nIMPLEMENTATION (what was actually built):\nI\n\nReview the implementation against the brief.") {
		t.Fatalf("review user prompt head: %q", user[:min(len(user), 200)])
	}
	if !strings.HasSuffix(user, "If no defects found, respond with: NO DEFECTS FOUND") {
		t.Fatalf("review user prompt tail: %q", user[max(0, len(user)-80):])
	}
}

func TestMissingPlaceholderIsError(t *testing.T) {
	tpl, err := Block("review", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tpl.Render(map[string]string{"brief": "B"}); err == nil {
		t.Fatal("missing placeholder must error")
	}
}
