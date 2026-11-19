package format

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limits from quizroom.md ("Checks before anything is stored").
const (
	SingleOptions = 4
	MaxCodeLines  = 25
	MaxStem       = 1500
	MaxOption     = 400
	MaxExpl       = 1200
	MaxMistake    = 300
)

// Reasons shown in the chat when something is dropped.
const (
	ReasonFormat      = "doesn't match the format"
	ReasonSingle      = "a single-answer question needs exactly 4 options and 1 correct"
	ReasonMulti       = "a pick-all question needs 4–5 options with 2–3 correct"
	ReasonSameOptions = "two options were the same"
	ReasonBlank       = "an option was blank"
	ReasonNoExpl      = "an option had no explanation"
	ReasonDifficulty  = "difficulty was outside 1–10"
	ReasonCode        = "code snippet over 25 lines"
	ReasonCodeLang    = "unknown code language"
	ReasonTooLong     = "too long"
	ReasonUnknown     = "unknown topic or subtopic"
	ReasonDuplicate   = "duplicate of an existing question"
	ReasonOneTopic    = "only one new topic per reply"
	ReasonTopicExists = "topic already exists"
)

var (
	slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	keyRe  = regexp.MustCompile(`^n[0-9]{1,3}$`)
)

// Known is what already exists, for checking references.
type Known struct {
	Subtopics map[string]map[string]bool // topic slug → subtopic slugs
	Mistakes  map[string]bool            // refs like "m12"
	StemKeys  map[string]map[string]bool // topic slug → StemKey of existing stems
}

// Dropped records something Claude sent that wasn't stored, and why.
type Dropped struct {
	What   string // "question: <stem>", "mistake: <belief>" or "topic: <slug>"
	Reason string
}

// Clean is a reply after the checks: only what may be stored.
type Clean struct {
	Reply     string
	Topic     *NewTopic
	Mistakes  []NewMistake
	Questions []Question
	Dropped   []Dropped
}

// StemKey normalizes a stem for duplicate checks.
func StemKey(stem string) string {
	return strings.Join(strings.Fields(strings.ToLower(stem)), " ")
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func tooLong(s string, n int) bool { return utf8.RuneCountInString(s) > n }

func head(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Validate checks a reply against what's known. It never modifies k.
func Validate(r Reply, k Known) Clean {
	c := Clean{Reply: r.Reply}

	subtopics := make(map[string]map[string]bool, len(k.Subtopics)+1)
	for t, subs := range k.Subtopics {
		subtopics[t] = subs
	}
	for i, t := range r.NewTopics {
		if i > 0 {
			c.Dropped = append(c.Dropped, Dropped{"topic: " + t.Slug, ReasonOneTopic})
			continue
		}
		if reason := checkTopic(t, k); reason != "" {
			c.Dropped = append(c.Dropped, Dropped{"topic: " + t.Slug, reason})
			continue
		}
		topic := t
		c.Topic = &topic
		subs := map[string]bool{}
		for _, s := range t.Subtopics {
			subs[s.Slug] = true
		}
		subtopics[t.Slug] = subs
	}

	newKeys := map[string]bool{}
	for _, m := range r.NewMistakes {
		if !keyRe.MatchString(m.Key) || newKeys[m.Key] || !subtopics[m.Topic][m.Subtopic] ||
			blank(m.Belief) || blank(m.Correction) || tooLong(m.Belief, MaxMistake) || tooLong(m.Correction, MaxMistake) {
			c.Dropped = append(c.Dropped, Dropped{"mistake: " + head(m.Belief, 60), ReasonFormat})
			continue
		}
		newKeys[m.Key] = true
		c.Mistakes = append(c.Mistakes, m)
	}

	seen := map[string]map[string]bool{}
	for _, q := range r.Questions {
		reason := checkQuestion(q, subtopics)
		key := StemKey(q.Stem)
		if reason == "" && (k.StemKeys[q.Topic][key] || seen[q.Topic][key]) {
			reason = ReasonDuplicate
		}
		if reason != "" {
			c.Dropped = append(c.Dropped, Dropped{"question: " + head(q.Stem, 60), reason})
			continue
		}
		if seen[q.Topic] == nil {
			seen[q.Topic] = map[string]bool{}
		}
		seen[q.Topic][key] = true

		q.Options = append([]Option(nil), q.Options...)
		for i, o := range q.Options {
			switch {
			case o.Mistake == "":
			case o.Correct:
				q.Options[i].Mistake = ""
			case strings.HasPrefix(o.Mistake, "m") && k.Mistakes[o.Mistake]:
			case newKeys[o.Mistake]:
			default:
				q.Options[i].Mistake = ""
			}
		}
		c.Questions = append(c.Questions, q)
	}
	return c
}

func checkTopic(t NewTopic, k Known) string {
	if !slugRe.MatchString(t.Slug) || blank(t.Name) || len(t.Subtopics) < 2 || len(t.Subtopics) > 15 {
		return ReasonFormat
	}
	if _, exists := k.Subtopics[t.Slug]; exists {
		return ReasonTopicExists
	}
	slugs := map[string]bool{}
	for _, s := range t.Subtopics {
		if !slugRe.MatchString(s.Slug) || slugs[s.Slug] || blank(s.Name) {
			return ReasonFormat
		}
		slugs[s.Slug] = true
	}
	return ""
}

func checkQuestion(q Question, subtopics map[string]map[string]bool) string {
	if q.Kind != Single && q.Kind != Multi {
		return ReasonFormat
	}
	if !subtopics[q.Topic][q.Subtopic] {
		return ReasonUnknown
	}
	if blank(q.Stem) {
		return ReasonFormat
	}
	if tooLong(q.Stem, MaxStem) {
		return ReasonTooLong
	}
	for _, o := range q.Options {
		if tooLong(o.Text, MaxOption) || tooLong(o.Explanation, MaxExpl) {
			return ReasonTooLong
		}
	}
	if q.Difficulty < 1 || q.Difficulty > 10 {
		return ReasonDifficulty
	}
	langOK := false
	for _, l := range CodeLangs {
		if q.CodeLang == l {
			langOK = true
		}
	}
	if !langOK {
		return ReasonCodeLang
	}
	if lines := strings.Split(strings.TrimRight(q.Code, "\n"), "\n"); q.Code != "" && len(lines) > MaxCodeLines {
		return ReasonCode
	}

	correct := 0
	for _, o := range q.Options {
		if o.Correct {
			correct++
		}
	}
	switch q.Kind {
	case Single:
		if len(q.Options) != SingleOptions || correct != 1 {
			return ReasonSingle
		}
	case Multi:
		if len(q.Options) < 4 || len(q.Options) > 5 || correct < 2 || correct > 3 {
			return ReasonMulti
		}
	}

	texts := map[string]bool{}
	for _, o := range q.Options {
		if blank(o.Text) {
			return ReasonBlank
		}
		if blank(o.Explanation) {
			return ReasonNoExpl
		}
		t := StemKey(o.Text)
		if texts[t] {
			return ReasonSameOptions
		}
		texts[t] = true
	}
	return ""
}
