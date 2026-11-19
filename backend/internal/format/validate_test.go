package format

import (
	"reflect"
	"strings"
	"testing"
)

func known() Known {
	return Known{
		Subtopics: map[string]map[string]bool{
			"docker":     {"compose": true, "images": true},
			"kubernetes": {"workloads": true},
		},
		Mistakes: map[string]bool{"m12": true},
		StemKeys: map[string]map[string]bool{"docker": {"an existing question?": true}},
	}
}

func opt(text string, correct bool) Option {
	return Option{Text: text, Correct: correct, Explanation: "because " + text}
}

func good(stem string) Question {
	return Question{
		Topic: "docker", Subtopic: "compose", Kind: Single, Difficulty: 4, Stem: stem,
		Options: []Option{opt("A", true), opt("B", false), opt("C", false), opt("D", false)},
	}
}

func reasons(c Clean) []string {
	var out []string
	for _, d := range c.Dropped {
		out = append(out, d.Reason)
	}
	return out
}

func dropReason(t *testing.T, q Question) string {
	t.Helper()
	c := Validate(Reply{Questions: []Question{q}}, known())
	if len(c.Questions) != 0 || len(c.Dropped) != 1 {
		t.Fatalf("want the question dropped, got %d kept, drops %v", len(c.Questions), reasons(c))
	}
	return c.Dropped[0].Reason
}

func TestValidateKeepsValidAndDropsInvalid(t *testing.T) {
	dup := good("Which option is duplicated?")
	dup.Options[1].Text = "a"
	unknown := good("Which subtopic is this?")
	unknown.Subtopic = "nope"
	c := Validate(Reply{Reply: "Here you go.", Questions: []Question{good("What does depends_on do?"), dup, unknown}}, known())
	if len(c.Questions) != 1 || c.Questions[0].Stem != "What does depends_on do?" {
		t.Fatalf("kept %+v", c.Questions)
	}
	if got, want := reasons(c), []string{"two options were the same", "unknown topic or subtopic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dropped %v, want %v", got, want)
	}
	if !strings.HasPrefix(c.Dropped[0].What, "question: Which option is duplicated?") {
		t.Fatalf("What = %q", c.Dropped[0].What)
	}
	if c.Reply != "Here you go." {
		t.Fatalf("Reply = %q", c.Reply)
	}
}

func TestValidateKindRules(t *testing.T) {
	const single = "a single-answer question needs exactly 4 options and 1 correct"
	const multi = "a pick-all question needs 4–5 options with 2–3 correct"

	three := good("q")
	three.Options = three.Options[:3]
	twoRight := good("q")
	twoRight.Options[1].Correct = true
	for _, q := range []Question{three, twoRight} {
		if r := dropReason(t, q); r != single {
			t.Fatalf("got %q, want %q", r, single)
		}
	}

	oneRight := good("q")
	oneRight.Kind = Multi
	six := good("q")
	six.Kind = Multi
	six.Options = append(six.Options, opt("E", true), opt("F", false))
	for _, q := range []Question{oneRight, six} {
		if r := dropReason(t, q); r != multi {
			t.Fatalf("got %q, want %q", r, multi)
		}
	}
	okMulti := good("A pick-all question?")
	okMulti.Kind = Multi
	okMulti.Options[1].Correct = true
	if c := Validate(Reply{Questions: []Question{okMulti}}, known()); len(c.Questions) != 1 {
		t.Fatalf("a valid pick-all question was dropped: %v", reasons(c))
	}

	bad := good("q")
	bad.Kind = "x"
	if r := dropReason(t, bad); r != "doesn't match the format" {
		t.Fatalf("got %q", r)
	}
}

func TestValidateFieldRules(t *testing.T) {
	blank := good("q")
	blank.Options[2].Text = "  "
	noExpl := good("q")
	noExpl.Options[3].Explanation = ""
	d0, d11 := good("q"), good("q")
	d0.Difficulty, d11.Difficulty = 0, 11
	longCode := good("q")
	longCode.Code, longCode.CodeLang = strings.Repeat("line\n", 26), "yaml"
	lang := good("q")
	lang.Code, lang.CodeLang = "x := 1", "go"
	long := good(strings.Repeat("x", 1501))

	cases := []struct {
		q    Question
		want string
	}{
		{blank, "an option was blank"},
		{noExpl, "an option had no explanation"},
		{d0, "difficulty was outside 1–10"},
		{d11, "difficulty was outside 1–10"},
		{longCode, "code snippet over 25 lines"},
		{lang, "unknown code language"},
		{long, "too long"},
	}
	for _, tc := range cases {
		if r := dropReason(t, tc.q); r != tc.want {
			t.Fatalf("got %q, want %q", r, tc.want)
		}
	}
	ok := good("Twenty-five lines is fine?")
	ok.Code, ok.CodeLang = strings.Repeat("line\n", 25), "yaml"
	if c := Validate(Reply{Questions: []Question{ok}}, known()); len(c.Questions) != 1 {
		t.Fatalf("25 lines should be kept: %v", reasons(c))
	}
}

func TestValidateMistakeRefs(t *testing.T) {
	q := good("Which component recreates the pod?")
	q.Options[1].Mistake = "m12" // known: kept
	q.Options[2].Mistake = "m99" // unknown: cleared
	q.Options[3].Mistake = "n1"  // new mistake in this reply: kept
	q.Options[0].Mistake = "m12" // on the correct option: cleared
	r := Reply{
		Questions:   []Question{q},
		NewMistakes: []NewMistake{{Key: "n1", Topic: "docker", Subtopic: "compose", Belief: "depends_on waits for readiness", Correction: "only with condition: service_healthy"}},
	}
	c := Validate(r, known())
	if len(c.Questions) != 1 || len(c.Mistakes) != 1 {
		t.Fatalf("kept %d questions and %d mistakes; drops %v", len(c.Questions), len(c.Mistakes), reasons(c))
	}
	got := []string{}
	for _, o := range c.Questions[0].Options {
		got = append(got, o.Mistake)
	}
	if want := []string{"", "m12", "", "n1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mistake refs = %v, want %v", got, want)
	}
}

func TestValidateNewMistakeRules(t *testing.T) {
	r := Reply{NewMistakes: []NewMistake{
		{Key: "n1", Topic: "docker", Subtopic: "compose", Belief: "b", Correction: "c"},
		{Key: "n1", Topic: "docker", Subtopic: "compose", Belief: "duplicate key", Correction: "c"},
		{Key: "x", Topic: "docker", Subtopic: "compose", Belief: "bad key", Correction: "c"},
		{Key: "n3", Topic: "docker", Subtopic: "nope", Belief: "unknown subtopic", Correction: "c"},
		{Key: "n4", Topic: "docker", Subtopic: "compose", Belief: "no correction", Correction: " "},
	}}
	c := Validate(r, known())
	if len(c.Mistakes) != 1 || c.Mistakes[0].Belief != "b" {
		t.Fatalf("kept %+v", c.Mistakes)
	}
	if len(c.Dropped) != 4 || c.Dropped[0].What != "mistake: duplicate key" || c.Dropped[0].Reason != "doesn't match the format" {
		t.Fatalf("dropped %+v", c.Dropped)
	}
}

func TestValidateNewTopic(t *testing.T) {
	tf := NewTopic{Slug: "terraform", Name: "Terraform", Description: "IaC",
		Subtopics: []NewSubtopic{{Slug: "state", Name: "State"}, {Slug: "modules", Name: "Modules"}}}
	q := good("What does terraform plan do?")
	q.Topic, q.Subtopic = "terraform", "state"
	second := NewTopic{Slug: "ansible", Name: "Ansible", Subtopics: tf.Subtopics}
	c := Validate(Reply{NewTopics: []NewTopic{tf, second}, Questions: []Question{q}}, known())
	if c.Topic == nil || c.Topic.Slug != "terraform" || len(c.Questions) != 1 {
		t.Fatalf("topic %+v, %d questions, drops %v", c.Topic, len(c.Questions), reasons(c))
	}
	if got := reasons(c); !reflect.DeepEqual(got, []string{"only one new topic per reply"}) {
		t.Fatalf("drops %v", got)
	}

	exists := NewTopic{Slug: "docker", Name: "Docker", Subtopics: tf.Subtopics}
	c = Validate(Reply{NewTopics: []NewTopic{exists}}, known())
	if c.Topic != nil || !reflect.DeepEqual(reasons(c), []string{"topic already exists"}) {
		t.Fatalf("topic %+v, drops %v", c.Topic, reasons(c))
	}
	oneSub := NewTopic{Slug: "helm", Name: "Helm", Subtopics: tf.Subtopics[:1]}
	if c = Validate(Reply{NewTopics: []NewTopic{oneSub}}, known()); c.Topic != nil {
		t.Fatal("a topic needs 2–15 subtopics")
	}
	if k := known(); k.Subtopics["terraform"] != nil {
		t.Fatal("Validate must not modify Known")
	}
}

func TestValidateDuplicates(t *testing.T) {
	existing := good("  An EXISTING   question? ")
	twice1, twice2 := good("Asked twice?"), good("asked  twice?")
	c := Validate(Reply{Questions: []Question{existing, twice1, twice2}}, known())
	if len(c.Questions) != 1 || c.Questions[0].Stem != "Asked twice?" {
		t.Fatalf("kept %+v", c.Questions)
	}
	if got := reasons(c); !reflect.DeepEqual(got, []string{"duplicate of an existing question", "duplicate of an existing question"}) {
		t.Fatalf("drops %v", got)
	}
}

func TestStemKey(t *testing.T) {
	if got := StemKey("  What  IS\ta Pod? "); got != "what is a pod?" {
		t.Fatalf("StemKey = %q", got)
	}
}
