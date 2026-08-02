package event

import (
	"strings"
	"testing"
)

func TestLoadPullRequestOpened(t *testing.T) {
	ev, err := Load("testdata/pull_request_opened.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := &Event{
		Action:  "opened",
		Number:  7,
		Owner:   "coolamit",
		Repo:    "themis",
		BaseRef: "master",
		HeadSHA: "1111111111111111111111111111111111111111",
		IsFork:  false,
		Sender:  "coolamit",
	}
	if *ev != *want {
		t.Errorf("Event = %+v, want %+v", ev, want)
	}
}

func TestLoadPullRequestTargetLabeled(t *testing.T) {
	ev, err := Load("testdata/pull_request_target_labeled.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ev.Action != "labeled" || ev.LabelName != "themis-review" || ev.Sender != "label-applier" {
		t.Errorf("labeled fields = %+v", ev)
	}
	if !ev.IsFork {
		t.Error("IsFork = false for a fork head repo")
	}
	if ev.Number != 12 || ev.HeadSHA != "2222222222222222222222222222222222222222" {
		t.Errorf("PR fields = %+v", ev)
	}
}

func TestLoadSynchronize(t *testing.T) {
	ev, err := Load("testdata/pull_request_synchronize.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ev.Action != "synchronize" || ev.BaseRef != "develop" || ev.IsFork {
		t.Errorf("Event = %+v", ev)
	}
	if ev.HeadSHA != "3333333333333333333333333333333333333333" {
		t.Errorf("HeadSHA = %q, want the post-push head", ev.HeadSHA)
	}
}

func TestLoadDeletedForkHeadRepo(t *testing.T) {
	ev, err := Load("testdata/fork_deleted_head_repo.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ev.IsFork {
		t.Error("a null head repo must be treated as a fork")
	}
}

func TestParseRejectsIncompletePayloads(t *testing.T) {
	cases := []struct {
		name, payload, wantErr string
	}{
		{"not JSON", "nope", "parsing event payload"},
		{"empty object", "{}", "no pull request number"},
		{"missing head sha", `{"number": 5, "pull_request": {"number": 5, "base": {"ref": "main"}}}`, "no head SHA"},
		{"missing base ref", `{"number": 5, "pull_request": {"number": 5, "head": {"sha": "abc"}}}`, "no base ref"},
		{"missing repository", `{"number": 5, "pull_request": {"number": 5, "base": {"ref": "main"}, "head": {"sha": "abc"}}}`, "repository name"},
	}
	for _, tc := range cases {
		_, err := Parse(strings.NewReader(tc.payload))
		if err == nil {
			t.Errorf("%s: Parse accepted invalid payload", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error = %q, want mention of %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestParseSameRepoCaseInsensitive(t *testing.T) {
	payload := `{"number": 5, "pull_request": {"number": 5, "base": {"ref": "main", "repo": {"full_name": "o/r"}}, "head": {"sha": "abc1234", "repo": {"full_name": "O/R"}}}, "repository": {"full_name": "o/r"}}`
	ev, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ev.IsFork {
		t.Error("IsFork = true for a same-repo head differing only in case")
	}
}

func TestParseFallsBackToTopLevelNumber(t *testing.T) {
	payload := `{"number": 42, "pull_request": {"base": {"ref": "main", "repo": {"full_name": "o/r"}}, "head": {"sha": "abc", "repo": {"full_name": "o/r"}}}, "repository": {"full_name": "o/r"}}`
	ev, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ev.Number != 42 {
		t.Errorf("Number = %d, want 42 from top-level field", ev.Number)
	}
}
