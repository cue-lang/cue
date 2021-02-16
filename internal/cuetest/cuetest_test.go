package cuetest

import (
	"os"
	"testing"
)

func TestCondition(t *testing.T) {
	cases := []struct {
		name string
		env  string
		con  string
		want bool
		err  string
	}{
		// issue cases covered by TestCheckIssueCondition
		{
			name: "long",
			con:  "long",
			want: Long, // not really testing much here
		},
		{
			name: "bad condition",
			env:  ".",
			con:  "golang.org/Issue/1234", // note typo Issue
			want: false,
			err:  "unknown condition golang.org/Issue/1234",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Setenv(envNonIssues, c.env)
			got, err := Condition(c.con)
			if got != c.want {
				t.Errorf("expected %v; got %v", c.want, got)
			}
			if c.err != "" {
				if err == nil {
					t.Errorf("expected error %q; got nil", c.err)
				} else if e := err.Error(); e != c.err {
					t.Errorf("expected error %q; got %q", c.err, e)
				}
			} else if err != nil {
				t.Errorf("expected no error; got %v", err)
			}
		})
	}
}

func TestCheckIssueCondition(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		con      string
		isIssue  bool
		nonIssue bool
		err      string
	}{
		{
			name:     "empty env",
			con:      "golang.org/issue/1234",
			isIssue:  true,
			nonIssue: false,
		},
		{
			name:     "match all issues",
			env:      ".",
			con:      "golang.org/issue/1234",
			isIssue:  true,
			nonIssue: true,
		},
		{
			name:    "bad issue URL",
			con:     "golang.org/Issue/1234", // note typo
			isIssue: false,
		},
		{
			name:    "bad env",
			env:     `\`,
			con:     "golang.org/issue/1234",
			isIssue: false,
			err:     "failed to compile regexp \"\\\\\" specified via CUE_NON_ISSUES: error parsing regexp: trailing backslash at end of expression: ``",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Setenv(envNonIssues, c.env)
			isIssue, nonIssue, err := checkIssueCondition(c.con)
			if isIssue != c.isIssue {
				t.Errorf("expected isIssue %v; got %v", c.isIssue, isIssue)
			}
			if nonIssue != c.nonIssue {
				t.Errorf("expected nonIssue %v; got %v", c.nonIssue, nonIssue)
			}
			if c.err != "" {
				if err == nil {
					t.Errorf("expected error %q; got nil", c.err)
				} else if e := err.Error(); e != c.err {
					t.Errorf("expected error %q; got %q", c.err, e)
				}
			} else if err != nil {
				t.Errorf("expected no error; got %q", err)
			}
		})
	}

}
