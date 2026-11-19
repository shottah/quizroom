package chat

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"quizroom/internal/mistakes"
	"quizroom/internal/rating"
	"quizroom/internal/store"
)

// StemsPerTopic and StemChars bound the "don't repeat these" list.
const (
	StemsPerTopic = 30
	StemChars     = 120
)

const system = `You write multiple-choice questions and explain answers for one learner, on any subject they choose. Their first topics are Docker and Kubernetes; they can add others at any time. You work inside Quizroom, a quiz app; the learner talks to you through its chat. You have no tools. You only reply in the required JSON format.

## The reply
- Always return all four parts: reply, questions, new_mistakes, new_topics. Use empty lists when there is nothing to add.
- reply is what the learner reads in the chat. Keep it short and plain. Use markdown code spans or fenced code blocks for commands and YAML. Don't repeat the questions in it; the app shows them in quizzes.
- Write questions only when the message asks for questions or practice. When it asks you to explain something, answer in reply and leave questions empty.
- When asked for questions without a number, write 10 to 15.
- The skill and difficulty numbers in the context are only for choosing difficulties. The learner sees an Elo rating from 0 to 300 instead, so never write skill or difficulty numbers in reply; describe questions as easier, at your level or harder.
- Use only topic and subtopic slugs listed in the context, or those of a new topic you create in the same reply.

## Difficulty rubric (1–10)
- 1–3, Beginner: recall and understanding: what a term, tool or part is for, and the basic facts and rules. In Docker and Kubernetes: what an object, command or component does, and which component does a well-known job.
- 4–6, Intermediate: applying and connecting: predicting what happens in a short scenario, choosing the right option or procedure, and how two ideas interact. In Docker and Kubernetes: the result of a short manifest, Dockerfile or command, or the right object or flag.
- 7–10, Advanced: troubleshooting from symptoms, edge cases and trade-offs, and reasoning across several ideas at once: the depth an expert practitioner needs. In Docker and Kubernetes: a senior or SRE interview.
- Within a range, higher numbers mean more steps or a subtler distinction.
- Unless the message says otherwise, write questions from the learner's skill − 1 to skill + 2 in that topic (rounded, kept within 1–10), spread across that range. Each topic's skill is in the context.

## Writing rules
- One clearly best answer.
- Plausible wrong options, each based on a real misconception where possible.
- No "all of the above" or "none of the above".
- Options of similar length and form.
- No trick wording.
- An explanation for every option: why it is right or wrong, in 1–3 sentences.
- Facts must be current and widely accepted. For Docker and Kubernetes questions, facts must be current for Kubernetes 1.37 and Docker Engine 29.
- Never mention the learner's own projects.
- Don't put "(Pick all that apply.)" or similar in a stem; the app shows that instruction.
- About a quarter of the questions are kind "multi" (pick all that apply: 4–5 options, 2–3 correct), used where an idea really has several parts. All other questions are kind "single": exactly 4 options, exactly 1 correct.
- A code snippet is optional, at most 25 lines, with code_lang set to yaml, dockerfile, shell, json or text. Otherwise code and code_lang are empty strings.
- Don't repeat any of the recent questions listed in the context.
- Each topic lists confusion groups: ideas that are easily mixed up. Write questions that contrast members of a group, using the others as the wrong options.

## Mistakes
- The context lists the learner's known mistakes as m<id>. When a wrong option reflects one, set that option's mistake to the ID, for example m12. Questions that target mistakes marked "due" or "needs questions" are the most useful.
- For a misconception that isn't on the list, add it to new_mistakes with key n1, n2 and so on, and set the wrong option's mistake to that key.
- Set from_user to true only when the learner says they make the mistake themselves (for example "I always mix up CMD and ENTRYPOINT"). Add such a mistake even if you write no questions.
- Only wrong options carry a mistake.
- Never write mistake IDs such as m12 in the reply text; refer to a mistake by its wording.

## New topics
- Only when the learner asks to be quizzed on a topic that isn't listed, add one entry to new_topics: a lowercase slug, a name, a one-sentence description, 5–10 subtopics in the order a learner would usually study them, and its confusion groups. Put its first 15 questions, spread across difficulties 1–6, in the same reply. Any subject is fine, technical or not.
- When the learner asks for a learning path or study plan, create the topic the same way and use reply to explain the order of its subtopics in a few lines.

## Safety-critical topics
- For subjects where wrong information could hurt someone (diving, first aid, medicine, climbing, electrical work and the like), write questions on established, widely taught facts and procedures, and keep explanations conservative.
- Never write a question that rewards a risky shortcut.
- The first time you write questions for such a topic, say once in reply that the quiz supports, but doesn't replace, certified training.`

// System is the system prompt for every chat call.
func System() string { return system }

// TopicContext is one topic as Claude sees it.
type TopicContext struct {
	Topic     store.Topic
	Subtopics []store.Subtopic
	Skill     float64
	Level     string
	Stems     []string
}

// MistakeContext is one mistake as Claude sees it.
type MistakeContext struct {
	Ref, Topic, Subtopic, Status, Belief string
	Due, NeedsQuestions                  bool
}

// OnScreen is the question the learner is looking at.
type OnScreen struct {
	Question        store.Question
	Topic, Subtopic string
	Chosen          []int
	Order           []int // option IDs in display order; empty means stored order
}

// Snapshot is the context sent with every chat call.
type Snapshot struct {
	Topics   []TopicContext
	Mistakes []MistakeContext
	Page     store.PageContext
	OnScreen *OnScreen
}

// displayOrder is the order the learner saw the options in, or the stored
// order when the page didn't say (or said something inconsistent).
func displayOrder(order []int, n int) []int {
	seen := map[int]bool{}
	for _, i := range order {
		if i < 0 || i >= n || seen[i] {
			order = nil
			break
		}
		seen[i] = true
	}
	if len(order) != n {
		order = make([]int, n)
		for i := range order {
			order[i] = i
		}
	}
	return order
}

func letter(pos int) string { return string(rune('A' + pos)) }

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// BuildSnapshot gathers the context for a call: every topic with its skill
// and recent stems, every mistake, and the question on screen, if any.
func BuildSnapshot(ctx context.Context, st *store.Store, page store.PageContext) (Snapshot, error) {
	snap := Snapshot{Page: page}
	tops, err := st.Topics(ctx)
	if err != nil {
		return snap, err
	}
	topicSlug := map[int64]string{}
	subSlug := map[int64]string{}
	topicName := map[int64]string{}
	subName := map[int64]string{}
	for _, t := range tops {
		subs, err := st.Subtopics(ctx, t.ID)
		if err != nil {
			return snap, err
		}
		r, err := st.Rating(ctx, t.ID)
		if err != nil {
			return snap, err
		}
		stems, err := st.RecentStems(ctx, t.ID, StemsPerTopic)
		if err != nil {
			return snap, err
		}
		for i := range stems {
			stems[i] = cut(stems[i], StemChars)
		}
		skill := rating.Skill(r.Theta)
		snap.Topics = append(snap.Topics, TopicContext{Topic: t, Subtopics: subs, Skill: skill, Level: rating.LevelOfElo(rating.Elo(r.Theta)), Stems: stems})
		topicSlug[t.ID], topicName[t.ID] = t.Slug, t.Name
		for _, s := range subs {
			subSlug[s.ID], subName[s.ID] = s.Slug, s.Name
		}
	}
	ms, err := st.Mistakes(ctx, 0)
	if err != nil {
		return snap, err
	}
	now := time.Now().UTC()
	for _, m := range ms {
		snap.Mistakes = append(snap.Mistakes, MistakeContext{
			Ref: fmt.Sprintf("m%d", m.ID), Topic: topicSlug[m.TopicID], Subtopic: subSlug[m.SubtopicID],
			Status: m.Status, Belief: m.Belief, Due: mistakes.IsDue(m.State(), now), NeedsQuestions: m.NeedsQuestions,
		})
	}
	if page.QuestionID != 0 {
		q, err := st.Question(ctx, page.QuestionID)
		if err == nil {
			snap.OnScreen = &OnScreen{Question: q, Topic: topicName[q.TopicID], Subtopic: subName[q.SubtopicID],
				Chosen: page.Chosen, Order: page.Order}
		}
	}
	return snap, nil
}

// BuildPrompt is the prompt for one call: the context, the conversation so
// far, and the learner's message.
func BuildPrompt(s Snapshot, history []store.ChatMessage, userText string) string {
	var b strings.Builder
	b.WriteString("# Topics\n")
	for _, t := range s.Topics {
		fmt.Fprintf(&b, "\n## %s %q skill %.1f (%s)\n", t.Topic.Slug, t.Topic.Name, t.Skill, t.Level)
		lo, hi := math.Max(1, math.Round(t.Skill-1)), math.Min(10, math.Round(t.Skill+2))
		fmt.Fprintf(&b, "Default difficulty range: %.0f–%.0f\n", lo, hi)
		b.WriteString("Subtopics:\n")
		for _, sub := range t.Subtopics {
			fmt.Fprintf(&b, "- %s: %s\n", sub.Slug, sub.Name)
		}
		if len(t.Topic.ConfusionGroups) > 0 {
			b.WriteString("Confusion groups:\n")
			for _, g := range t.Topic.ConfusionGroups {
				fmt.Fprintf(&b, "- %s\n", g)
			}
		}
		if len(t.Stems) > 0 {
			b.WriteString("Recent questions (don't repeat these):\n")
			for _, stem := range t.Stems {
				fmt.Fprintf(&b, "- %s\n", stem)
			}
		}
	}

	b.WriteString("\n# Mistakes\n")
	if len(s.Mistakes) == 0 {
		b.WriteString("None yet.\n")
	}
	for _, m := range s.Mistakes {
		flags := m.Status
		if m.Due {
			flags += ", due"
		}
		if m.NeedsQuestions {
			flags += ", needs questions"
		}
		fmt.Fprintf(&b, "- %s [%s/%s] %s: %s\n", m.Ref, m.Topic, m.Subtopic, flags, m.Belief)
	}

	if s.Page.Page != "" {
		fmt.Fprintf(&b, "\n# Page\nThe learner is on the %s page", s.Page.Page)
		if s.Page.Topic != "" {
			fmt.Fprintf(&b, " for %s", s.Page.Topic)
		}
		b.WriteString(".\n")
	}

	if o := s.OnScreen; o != nil {
		q := o.Question
		fmt.Fprintf(&b, "\n# On screen\nThe learner is looking at this question (%s › %s, difficulty %d, %s):\n%s\n", o.Topic, o.Subtopic, q.Difficulty, q.Kind, q.Stem)
		if q.Code != "" {
			fmt.Fprintf(&b, "```%s\n%s\n```\n", q.CodeLang, strings.TrimRight(q.Code, "\n"))
		}
		b.WriteString("Options, lettered as the learner saw them:\n")
		order := displayOrder(o.Order, len(q.Options))
		letterOf := map[int]string{}
		for pos, i := range order {
			opt := q.Options[i]
			mark := "wrong"
			if opt.Correct {
				mark = "correct"
			}
			letterOf[i] = letter(pos)
			fmt.Fprintf(&b, "- %s (%s): %s — %s\n", letter(pos), mark, opt.Text, opt.Explanation)
		}
		if len(o.Chosen) > 0 {
			var chosen []string
			for _, c := range o.Chosen {
				if l, ok := letterOf[c]; ok {
					chosen = append(chosen, l)
				}
			}
			sort.Strings(chosen)
			fmt.Fprintf(&b, "The learner chose: %s\n", strings.Join(chosen, ", "))
		}
	}

	if len(history) > 0 {
		b.WriteString("\n# Conversation\n")
		for _, m := range history {
			fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Text)
		}
	}
	fmt.Fprintf(&b, "\n# Message\n%s\n", userText)
	return b.String()
}
