// Package format is the single definition of what Claude sends back: the reply
// types, the JSON schema generated from them for `claude --json-schema`, and
// the checks every reply passes before anything is stored.
package format

// Kind is a question's answer style.
type Kind string

const (
	Single Kind = "single" // four options, one correct
	Multi  Kind = "multi"  // "pick all that apply": 4–5 options, 2–3 correct
)

// CodeLangs are the languages a question's code snippet may be in.
var CodeLangs = []string{"", "yaml", "dockerfile", "shell", "json", "text"}

// Reply is everything Claude returns for one chat message. Every part is
// always present; the lists may be empty.
type Reply struct {
	Reply       string       `json:"reply" jsonschema:"what to show the learner in the chat: short, plain text, markdown code allowed"`
	Questions   []Question   `json:"questions" jsonschema:"new questions; empty when the message didn't ask for any"`
	NewMistakes []NewMistake `json:"new_mistakes" jsonschema:"mistakes not on the list yet: ones you tag wrong options with, or ones the learner says they make"`
	NewTopics   []NewTopic   `json:"new_topics" jsonschema:"at most one new topic, only when the learner asks for a topic that doesn't exist"`
}

// Question is one multiple-choice question as Claude writes it.
type Question struct {
	Topic      string   `json:"topic" jsonschema:"topic slug from the context, or the slug of the new topic in new_topics"`
	Subtopic   string   `json:"subtopic" jsonschema:"subtopic slug from that topic"`
	Kind       Kind     `json:"kind" jsonschema:"single: exactly 4 options, 1 correct; multi (pick all that apply): 4-5 options, 2-3 correct"`
	Difficulty int      `json:"difficulty" jsonschema:"1-10 on the difficulty rubric"`
	Stem       string   `json:"stem" jsonschema:"the question itself"`
	Code       string   `json:"code" jsonschema:"optional code snippet of at most 25 lines; empty string when none"`
	CodeLang   string   `json:"code_lang" jsonschema:"language of the code snippet; empty string when there is no code"`
	Options    []Option `json:"options" jsonschema:"the answer options, in any order"`
}

// Option is one answer option with its explanation.
type Option struct {
	Text        string `json:"text" jsonschema:"the option text"`
	Correct     bool   `json:"correct" jsonschema:"true when this option is correct"`
	Explanation string `json:"explanation" jsonschema:"why this option is right or wrong, in 1-3 sentences"`
	Mistake     string `json:"mistake" jsonschema:"empty, an existing mistake ID like m12, or the key of a new mistake in new_mistakes like n1; only on wrong options"`
}

// NewMistake is a wrong belief to add to the learner's mistake list.
type NewMistake struct {
	Key        string `json:"key" jsonschema:"n1, n2, ...: what wrong options reference in their mistake field"`
	Topic      string `json:"topic" jsonschema:"topic slug"`
	Subtopic   string `json:"subtopic" jsonschema:"subtopic slug"`
	Belief     string `json:"belief" jsonschema:"the wrong belief, in a short sentence"`
	Correction string `json:"correction" jsonschema:"the correct understanding, in a short sentence"`
	FromUser   bool   `json:"from_user" jsonschema:"true only when the learner said they make this mistake"`
}

// NewTopic is a topic Claude creates when the learner asks for one.
type NewTopic struct {
	Slug            string        `json:"slug" jsonschema:"lowercase slug, e.g. terraform"`
	Name            string        `json:"name" jsonschema:"display name, e.g. Terraform"`
	Description     string        `json:"description" jsonschema:"one short sentence"`
	Subtopics       []NewSubtopic `json:"subtopics" jsonschema:"5-10 subtopics"`
	ConfusionGroups []string      `json:"confusion_groups" jsonschema:"groups of easily confused ideas, one string per group"`
}

// NewSubtopic is one subtopic of a new topic.
type NewSubtopic struct {
	Slug string `json:"slug" jsonschema:"lowercase slug"`
	Name string `json:"name" jsonschema:"display name"`
}
