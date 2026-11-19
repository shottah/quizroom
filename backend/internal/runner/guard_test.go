package runner

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuthMode(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"ANTHROPIC_API_KEY": "sk-key", "CLAUDE_CODE_OAUTH_TOKEN": "tok"}, "api_key"},
		{map[string]string{"ANTHROPIC_API_KEY": "sk-key"}, "api_key"},
		{map[string]string{"ANTHROPIC_API_KEY": "", "CLAUDE_CODE_OAUTH_TOKEN": "tok"}, "subscription"},
		{map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "tok"}, "subscription"},
		{map[string]string{"ANTHROPIC_API_KEY": "", "CLAUDE_CODE_OAUTH_TOKEN": ""}, ""},
	}
	for _, c := range cases {
		if got := AuthMode(c.env); got != c.want {
			t.Fatalf("AuthMode(%v) = %q, want %q", c.env, got, c.want)
		}
	}
}

func envMap(kv []string) map[string]string {
	m := map[string]string{}
	for _, e := range kv {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func TestChildEnvPassesExactlyOneCredential(t *testing.T) {
	parent := map[string]string{"ANTHROPIC_API_KEY": "sk-key", "CLAUDE_CODE_OAUTH_TOKEN": "tok", "SECRET": "x", "PATH": "/usr/bin"}
	env := envMap(ChildEnv(parent, "/tmp/cfg", "/tmp/home"))
	if env["ANTHROPIC_API_KEY"] != "sk-key" {
		t.Fatal("the API key wins when set")
	}
	if _, ok := env["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Fatal("only one credential may be passed")
	}
	if _, ok := env["SECRET"]; ok {
		t.Fatal("unlisted variables must not leak into claude's environment")
	}
	if env["PATH"] != "/usr/bin" || env["CLAUDE_CONFIG_DIR"] != "/tmp/cfg" || env["HOME"] != "/tmp/home" {
		t.Fatalf("env = %v", env)
	}

	env = envMap(ChildEnv(map[string]string{"ANTHROPIC_API_KEY": "", "CLAUDE_CODE_OAUTH_TOKEN": "tok"}, "/c", "/h"))
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok" {
		t.Fatal("the subscription token is passed when there's no key")
	}
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Fatal("an empty key counts as unset")
	}
}

func TestArgv(t *testing.T) {
	r := Request{System: "SYS", Prompt: "P", Schema: []byte(`{"type":"object"}`), Model: "sonnet"}
	want := []string{"claude", "-p", "--tools", "", "--model", "sonnet", "--output-format", "json",
		"--json-schema", `{"type":"object"}`, "--system-prompt", "SYS", "--strict-mcp-config",
		"--disable-slash-commands", "--setting-sources", "", "--no-session-persistence"}
	if got := Argv(r, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("Argv =\n%q\nwant\n%q", got, want)
	}
	withKey := Argv(r, true)
	if !reflect.DeepEqual(withKey[len(withKey)-2:], []string{"--max-budget-usd", "1"}) {
		t.Fatalf("with an API key, Argv must cap spending: %q", withKey)
	}
	for _, a := range withKey {
		if a == "--bare" {
			t.Fatal("never --bare: it ignores CLAUDE_CODE_OAUTH_TOKEN")
		}
	}
}

func TestParseRequest(t *testing.T) {
	bad := []string{
		`{"prompt":"","schema":{},"model":"sonnet"}`,
		`{"prompt":"hi","schema":{},"model":"gpt"}`,
		`{"prompt":"hi","schema":[],"model":"sonnet"}`,
		`{"prompt":"hi","schema":{},"model":"sonnet","timeout_s":-1}`,
		`not json`,
	}
	for _, b := range bad {
		if _, err := ParseRequest([]byte(b)); err == nil {
			t.Fatalf("ParseRequest(%s) should fail", b)
		}
	}
	r, err := ParseRequest([]byte(`{"system":"s","prompt":"hi","schema":{"type":"object"},"model":"opus","timeout_s":0}`))
	if err != nil || r.TimeoutS != DefaultTimeoutS || r.Model != "opus" {
		t.Fatalf("got %+v, %v", r, err)
	}
	r, _ = ParseRequest([]byte(`{"prompt":"hi","schema":{},"model":"sonnet","timeout_s":5000}`))
	if r.TimeoutS != MaxTimeoutS {
		t.Fatalf("timeout %d, want clamp to %d", r.TimeoutS, MaxTimeoutS)
	}
}
