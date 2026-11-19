// Package quiz runs quizzes: starting them, picking each question, applying
// answers (ratings and the mistake schedule), and the topic views. Its types
// are also the API's response bodies.
package quiz

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"sync"
	"time"

	"quizroom/internal/mistakes"
	"quizroom/internal/picker"
	"quizroom/internal/rating"
	"quizroom/internal/store"
)

// Service is the quiz flow. Now defaults to the UTC wall clock.
type Service struct {
	St   *store.Store
	Now  func() time.Time
	Rand *rand.Rand

	mu sync.Mutex // guards Rand
}

// Quiz kinds: practice aims at the target success rate and retests
// mistakes; a check measures the learner as fast as possible.
const (
	Practice = "practice"
	Check    = "check"
)

// NotEnoughError means too few questions can be shown to start a quiz.
type NotEnoughError struct {
	Available int
	Need      int
	Kind      string
}

func (e NotEnoughError) Error() string {
	if e.Kind == Check {
		return fmt.Sprintf("No skill check right now: it needs %d questions you haven't seen in the last 7 days, and this topic has %d. Write 15 at your level first.", e.Need, e.Available)
	}
	return fmt.Sprintf("No quiz right now: a quiz needs %d questions you haven't seen in the last 7 days, and this topic has %d. Write 15 at your level to add more.", picker.MinLength, e.Available)
}

// ErrBadChoice is an empty, out-of-range or duplicated choice, or more than
// one choice on a single-answer question.
var ErrBadChoice = errors.New("choose one option, or for pick-all questions one or more different options")

type Started struct {
	QuizID int64  `json:"quiz_id"`
	Length int    `json:"length" doc:"the most questions it will ask; a check may stop earlier"`
	Kind   string `json:"kind" enum:"practice,check"`
}

type ShownOption struct {
	ID   int    `json:"id" doc:"the option's original index; send it back when answering"`
	Text string `json:"text"`
}

type ShownQuestion struct {
	ID        int64         `json:"id"`
	Topic     string        `json:"topic" doc:"topic name"`
	TopicSlug string        `json:"topic_slug"`
	Subtopic  string        `json:"subtopic" doc:"subtopic name"`
	Kind      string        `json:"kind" enum:"single,multi"`
	Stem      string        `json:"stem"`
	Code      string        `json:"code"`
	CodeLang  string        `json:"code_lang"`
	Options   []ShownOption `json:"options"`
}

type Shown struct {
	QuizID   int64          `json:"quiz_id"`
	Position int            `json:"position"`
	Length   int            `json:"length"`
	Done     bool           `json:"done"`
	Kind     string         `json:"kind" enum:"practice,check"`
	Thin     bool           `json:"thin" doc:"even the closest question was more than 0.15 off the target"`
	Question *ShownQuestion `json:"question,omitempty"`
}

type OptionFeedback struct {
	ID          int    `json:"id"`
	Text        string `json:"text"`
	Correct     bool   `json:"correct"`
	Chosen      bool   `json:"chosen"`
	Explanation string `json:"explanation"`
	Mistake     string `json:"mistake" doc:"the mistake this wrong option stands for, or empty"`
	OnList      bool   `json:"on_list" doc:"the mistake is one the learner has made, not only a trap Claude wrote"`
}

type MistakeHit struct {
	Belief     string    `json:"belief"`
	Correction string    `json:"correction"`
	Hits       int       `json:"hits" doc:"times the learner has made this mistake"`
	DueAt      time.Time `json:"due_at" doc:"from when it comes back: the next quiz in this topic"`
	New        bool      `json:"new" doc:"the first time the learner fell for a trap Claude wrote, so it's new on their list"`
}

// MistakeNote is where a mistake stands in its schedule: which of its 5
// checks comes next and from when, or done.
type MistakeNote struct {
	Belief     string     `json:"belief"`
	Correction string     `json:"correction"`
	NextDue    *time.Time `json:"next_due" doc:"from when it comes back; null when done"`
	NextCheck  int        `json:"next_check" doc:"which of the 5 checks comes next, 1 to 5; 0 when done"`
	Done       bool       `json:"done"`
}

func note(m store.Mistake) MistakeNote {
	n := MistakeNote{Belief: m.Belief, Correction: m.Correction}
	if m.Status == string(mistakes.Resolved) {
		n.Done = true
		return n
	}
	n.NextDue, n.NextCheck = m.DueAt, m.Stage+1
	return n
}

type Result struct {
	Correct        bool             `json:"correct"`
	CorrectIDs     []int            `json:"correct_ids"`
	Options        []OptionFeedback `json:"options"`
	MistakesHit    []MistakeHit     `json:"mistakes_hit"`
	MovedOn        []MistakeNote    `json:"moved_on" doc:"mistakes this right answer moved a step towards done"`
	P              float64          `json:"p" doc:"the predicted chance of a right answer"`
	AnswersInTopic int              `json:"answers_in_topic" doc:"the learner's answers in this topic so far, this one included"`
	EloBefore      int              `json:"elo_before"`
	EloAfter       int              `json:"elo_after"`
	MarginAfter    int              `json:"margin_after" doc:"± Elo points after this answer"`
	LevelBefore    string           `json:"level_before"`
	Level          string           `json:"level"`
	Finished       bool             `json:"finished"`
}

// ReviewItem is one answered question in a quiz's review.
type ReviewItem struct {
	Position   int              `json:"position"`
	QuestionID int64            `json:"question_id"`
	Subtopic   string           `json:"subtopic" doc:"subtopic name"`
	Kind       string           `json:"kind" enum:"single,multi"`
	Stem       string           `json:"stem"`
	Code       string           `json:"code"`
	CodeLang   string           `json:"code_lang"`
	Correct    bool             `json:"correct"`
	Options    []OptionFeedback `json:"options" doc:"in the order they were shown"`
}

type Summary struct {
	QuizID          int64         `json:"quiz_id"`
	Topic           string        `json:"topic"`
	TopicSlug       string        `json:"topic_slug"`
	Length          int           `json:"length"`
	Answered        int           `json:"answered"`
	Right           int           `json:"right"`
	Expected        float64       `json:"expected"`
	EloBefore       int           `json:"elo_before"`
	EloAfter        int           `json:"elo_after"`
	MarginAfter     int           `json:"margin_after" doc:"± Elo points after the quiz's last answer"`
	Kind            string        `json:"kind" enum:"practice,check"`
	Level           string        `json:"level"`
	MistakesHit     int           `json:"mistakes_hit"`
	MistakesCleared int           `json:"mistakes_cleared"`
	LevelBefore     string        `json:"level_before"`
	OffLevel        int           `json:"off_level" doc:"answered questions outside the learner's usual range, because none closer were left"`
	CameBack        []MistakeNote `json:"came_back" doc:"mistakes the learner made again in this quiz"`
	MovedOn         []MistakeNote `json:"moved_on" doc:"mistakes this quiz moved a step towards done"`
	Review          []ReviewItem  `json:"review" doc:"the answered questions, in order"`
	BankState       string        `json:"bank_state" enum:"plenty,low,offlevel,blocked" doc:"the topic's question bank now"`
	Check           *CheckResult  `json:"check" doc:"a finished skill check's result; null for practice"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) withRand(f func(r *rand.Rand)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Rand == nil {
		s.Rand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x51))
	}
	f(s.Rand)
}

func guessFloor(q store.Question) float64 {
	return rating.GuessFloor(q.Kind == "multi", len(q.Options))
}

func candidates(el []store.EligibleQuestion) []picker.Candidate {
	out := make([]picker.Candidate, len(el))
	for i, e := range el {
		out[i] = picker.Candidate{QuestionID: e.ID, SubtopicID: e.SubtopicID, B: e.B, C: guessFloor(e.Question), NeverSeen: e.NeverSeen,
			MistakeIDs: e.MistakeIDs, Multi: e.Kind == "multi"}
	}
	return out
}

// Start begins a quiz in a topic.
func (s *Service) Start(ctx context.Context, topicSlug string) (Started, error) {
	t, err := s.St.TopicBySlug(ctx, topicSlug)
	if err != nil {
		return Started{}, err
	}
	r, err := s.St.Rating(ctx, t.ID)
	if err != nil {
		return Started{}, err
	}
	now := s.now()
	el, err := s.St.Eligible(ctx, t.ID, 0, now)
	if err != nil {
		return Started{}, err
	}
	length, ok := picker.Length(len(el))
	if !ok {
		return Started{}, NotEnoughError{Available: len(el), Need: picker.MinLength, Kind: Practice}
	}
	due, err := s.St.DueMistakes(ctx, t.ID, now)
	if err != nil {
		return Started{}, err
	}
	settings, err := s.St.Settings(ctx)
	if err != nil {
		return Started{}, err
	}
	var slots []int
	s.withRand(func(rng *rand.Rand) { slots = picker.MistakeSlots(length, len(due), rng) })
	theta := r.Theta
	id, err := s.St.CreateQuiz(ctx, store.Quiz{TopicID: t.ID, Length: length, Target: settings.Target, StartedAt: now,
		SkillBefore: rating.Skill(r.Theta), MistakeSlots: slots, ThetaBefore: &theta, Kind: Practice})
	if err != nil {
		return Started{}, err
	}
	return Started{QuizID: id, Length: length, Kind: Practice}, nil
}

// StartCheck begins a skill check: no mistake slots, and it keeps a reserve
// of questions for practice.
func (s *Service) StartCheck(ctx context.Context, topicSlug string) (Started, error) {
	t, err := s.St.TopicBySlug(ctx, topicSlug)
	if err != nil {
		return Started{}, err
	}
	r, err := s.St.Rating(ctx, t.ID)
	if err != nil {
		return Started{}, err
	}
	now := s.now()
	el, err := s.St.Eligible(ctx, t.ID, 0, now)
	if err != nil {
		return Started{}, err
	}
	length, ok := picker.CheckLength(len(el))
	if !ok {
		return Started{}, NotEnoughError{Available: len(el), Need: picker.CheckMin, Kind: Check}
	}
	settings, err := s.St.Settings(ctx)
	if err != nil {
		return Started{}, err
	}
	theta := r.Theta
	id, err := s.St.CreateQuiz(ctx, store.Quiz{TopicID: t.ID, Length: length, Target: settings.Target, StartedAt: now,
		SkillBefore: rating.Skill(r.Theta), MistakeSlots: []int{}, ThetaBefore: &theta, Kind: Check})
	if err != nil {
		return Started{}, err
	}
	return Started{QuizID: id, Length: length, Kind: Check}, nil
}

// Next returns the question at the quiz's current position: the same one
// again if it hasn't been answered yet, otherwise a newly picked one.
func (s *Service) Next(ctx context.Context, quizID int64) (Shown, error) {
	z, err := s.St.Quiz(ctx, quizID)
	if err != nil {
		return Shown{}, err
	}
	done := Shown{QuizID: z.ID, Length: z.Length, Done: true, Kind: z.Kind}
	if z.FinishedAt != nil {
		return done, nil
	}
	answers, err := s.St.QuizAnswers(ctx, quizID)
	if err != nil {
		return Shown{}, err
	}
	if n := len(answers); n > 0 && answers[n-1].AnsweredAt == nil {
		return s.shown(ctx, z, answers[n-1])
	}
	now := s.now()
	position := len(answers) + 1
	el, err := s.St.Eligible(ctx, z.TopicID, quizID, now)
	if err != nil {
		return Shown{}, err
	}
	if z.Kind == Check && position > picker.CheckMinAnswers {
		r, err := s.St.Rating(ctx, z.TopicID)
		if err != nil {
			return Shown{}, err
		}
		if rating.Margin(currentRD(r, now)) <= picker.CheckStopMargin {
			if err := s.St.FinishQuiz(ctx, quizID, now); err != nil {
				return Shown{}, err
			}
			done.Position = len(answers)
			return done, nil
		}
	}
	// A check also stops rather than use the questions practice needs, even
	// when practice used the bank between its answers.
	if position > z.Length || len(el) == 0 || (z.Kind == Check && len(el) <= picker.CheckReserve) {
		if err := s.St.FinishQuiz(ctx, quizID, now); err != nil {
			return Shown{}, err
		}
		done.Position = len(answers)
		return done, nil
	}

	r, err := s.St.Rating(ctx, z.TopicID)
	if err != nil {
		return Shown{}, err
	}
	pq := picker.Quiz{Theta: r.Theta, Target: z.Target, SubtopicCounts: map[int64]int{}}
	usedMistakes := map[int64]bool{}
	multis := 0
	for _, a := range answers {
		q, err := s.St.Question(ctx, a.QuestionID)
		if err != nil {
			return Shown{}, err
		}
		pq.SubtopicCounts[q.SubtopicID]++
		if q.Kind == "multi" {
			multis++
		}
		pq.PrevSubtopic = q.SubtopicID
		if a.SlotMistakeID != nil {
			usedMistakes[*a.SlotMistakeID] = true
		}
	}
	cands := candidates(el)

	var pick picker.Pick
	slot := "rating"
	var slotMistake *int64
	if slices.Contains(z.MistakeSlots, position) {
		due, err := s.St.DueMistakes(ctx, z.TopicID, z.StartedAt)
		if err != nil {
			return Shown{}, err
		}
		for _, m := range due {
			if usedMistakes[m.ID] {
				continue
			}
			var ok bool
			s.withRand(func(rng *rand.Rand) { pick, ok = picker.Mistake(cands, m.ID, pq, rng) })
			if ok {
				slot, slotMistake = "mistake", &m.ID
				break
			}
			if err := s.St.SetNeedsQuestions(ctx, m.ID, true); err != nil {
				return Shown{}, err
			}
		}
	}
	if z.Kind == Check {
		// at most a quarter pick-all: they always score as more informative
		multiLeft := (position+3)/4 - multis
		var ok bool
		s.withRand(func(rng *rand.Rand) { pick, ok = picker.Check(cands, pq, multiLeft, rng) })
		if !ok {
			if err := s.St.FinishQuiz(ctx, quizID, now); err != nil {
				return Shown{}, err
			}
			done.Position = len(answers)
			return done, nil
		}
	} else if slot == "rating" {
		s.withRand(func(rng *rand.Rand) { pick, _ = picker.Rating(cands, pq, rng) })
	}

	var chosen store.EligibleQuestion
	for _, e := range el {
		if e.ID == pick.Candidate.QuestionID {
			chosen = e
		}
	}
	var order []int
	s.withRand(func(rng *rand.Rand) { order = rng.Perm(len(chosen.Options)) })
	a, err := s.St.InsertShown(ctx, store.Answer{QuizID: quizID, Position: position, QuestionID: chosen.ID, Slot: slot,
		SlotMistakeID: slotMistake, OptionOrder: order, ShownAt: now, Repeat: !chosen.NeverSeen, P: pick.P})
	if err != nil {
		return Shown{}, err
	}
	return s.shown(ctx, z, a)
}

func (s *Service) names(ctx context.Context, topicID, subtopicID int64) (topic, slug, subtopic string, err error) {
	tops, err := s.St.Topics(ctx)
	if err != nil {
		return "", "", "", err
	}
	for _, t := range tops {
		if t.ID == topicID {
			topic, slug = t.Name, t.Slug
		}
	}
	subs, err := s.St.Subtopics(ctx, topicID)
	if err != nil {
		return "", "", "", err
	}
	for _, sub := range subs {
		if sub.ID == subtopicID {
			subtopic = sub.Name
		}
	}
	return topic, slug, subtopic, nil
}

func (s *Service) shown(ctx context.Context, z store.Quiz, a store.Answer) (Shown, error) {
	q, err := s.St.Question(ctx, a.QuestionID)
	if err != nil {
		return Shown{}, err
	}
	topic, slug, subtopic, err := s.names(ctx, q.TopicID, q.SubtopicID)
	if err != nil {
		return Shown{}, err
	}
	opts := make([]ShownOption, 0, len(a.OptionOrder))
	for _, i := range a.OptionOrder {
		opts = append(opts, ShownOption{ID: i, Text: q.Options[i].Text})
	}
	return Shown{
		QuizID: z.ID, Position: a.Position, Length: z.Length, Kind: z.Kind,
		Thin: z.Kind != Check && a.Slot == "rating" && math.Abs(a.P-z.Target) > picker.FitWindow,
		Question: &ShownQuestion{ID: q.ID, Topic: topic, TopicSlug: slug, Subtopic: subtopic, Kind: q.Kind, Stem: q.Stem,
			Code: q.Code, CodeLang: q.CodeLang, Options: opts},
	}, nil
}

func correctIDs(q store.Question) []int {
	out := []int{}
	for i, o := range q.Options {
		if o.Correct {
			out = append(out, i)
		}
	}
	return out
}

func checkChoice(q store.Question, chosen []int) ([]int, error) {
	if len(chosen) == 0 || (q.Kind != "multi" && len(chosen) != 1) {
		return nil, ErrBadChoice
	}
	seen := map[int]bool{}
	for _, c := range chosen {
		if c < 0 || c >= len(q.Options) || seen[c] {
			return nil, ErrBadChoice
		}
		seen[c] = true
	}
	sorted := slices.Clone(chosen)
	slices.Sort(sorted)
	return sorted, nil
}

// Answer applies an answer. Answering the same position again returns the
// first result and changes nothing.
func (s *Service) Answer(ctx context.Context, quizID int64, position int, chosen []int) (Result, error) {
	now := s.now()
	after, _, err := s.St.ApplyAnswer(ctx, quizID, position, now, func(st store.AnswerState) (store.AnswerUpdate, error) {
		q := st.Question
		sorted, err := checkChoice(q, chosen)
		if err != nil {
			return store.AnswerUpdate{}, err
		}
		correct := slices.Equal(sorted, correctIDs(q))
		r := rating.Apply(st.Rating.Theta, q.B, guessFloor(q), st.Rating.N, q.N, correct, st.Answer.Repeat)
		rdBefore := currentRD(st.Rating, now)
		rdAfter := rating.Narrow(rdBefore, rating.Info(st.Answer.P, guessFloor(q)), st.Answer.Repeat)

		var changes []store.MistakeChange
		hit := map[int64]bool{}
		for _, i := range sorted {
			if o := q.Options[i]; !o.Correct && o.MistakeID != 0 && !hit[o.MistakeID] {
				hit[o.MistakeID] = true
				changes = append(changes, store.MistakeChange{ID: o.MistakeID, State: mistakes.Hit(now), Event: mistakes.HitEvent})
			}
		}
		if correct && !st.Answer.Repeat {
			for _, id := range q.MistakeIDs {
				if m, ok := st.Mistakes[id]; ok && mistakes.IsDue(m.State(), st.Quiz.StartedAt) {
					changes = append(changes, store.MistakeChange{ID: id, State: mistakes.Advance(m.State(), now), Event: mistakes.ClearEvent})
				}
			}
		}
		return store.AnswerUpdate{Chosen: sorted, Correct: correct, ThetaAfter: r.Theta, BAfter: r.B, RDBefore: rdBefore, RDAfter: rdAfter,
			Mistakes: changes, Finished: st.Answer.Position >= st.Quiz.Length}, nil
	})
	if err != nil {
		return Result{}, err
	}
	events, err := s.St.AnswerEvents(ctx, after.Answer.ID)
	if err != nil {
		return Result{}, err
	}
	return result(after, events), nil
}

// feedback is a's options in the order they were shown, with what each meant.
func feedback(q store.Question, a store.Answer, ms map[int64]store.Mistake) []OptionFeedback {
	out := make([]OptionFeedback, 0, len(a.OptionOrder))
	for _, i := range a.OptionOrder {
		o := q.Options[i]
		fb := OptionFeedback{ID: i, Text: o.Text, Correct: o.Correct, Chosen: slices.Contains(a.Chosen, i), Explanation: o.Explanation}
		if m, ok := ms[o.MistakeID]; ok && !o.Correct {
			fb.Mistake, fb.OnList = m.Belief, m.Status != string(mistakes.Watching)
		}
		out = append(out, fb)
	}
	return out
}

// result is the feedback on an answer. The mistakes it came back or moved
// on come from the events the answer recorded, so a repeat returns the same.
func result(st store.AnswerState, events []store.MistakeEvent) Result {
	q, a := st.Question, st.Answer
	res := Result{CorrectIDs: correctIDs(q), Options: feedback(q, a, st.Mistakes), MistakesHit: []MistakeHit{},
		MovedOn: []MistakeNote{}, P: a.P, AnswersInTopic: st.Rating.N, Finished: a.Position >= st.Quiz.Length}
	if a.Correct != nil {
		res.Correct = *a.Correct
	}
	if a.ThetaBefore != nil {
		res.EloBefore = rating.Elo(*a.ThetaBefore)
	}
	if a.ThetaAfter != nil {
		res.EloAfter = rating.Elo(*a.ThetaAfter)
	}
	if a.RDAfter != nil {
		res.MarginAfter = rating.Margin(*a.RDAfter)
	}
	res.LevelBefore, res.Level = rating.LevelOfElo(res.EloBefore), rating.LevelOfElo(res.EloAfter)
	for _, e := range events {
		m, ok := st.Mistakes[e.MistakeID]
		if !ok {
			continue
		}
		switch {
		case e.Kind == string(mistakes.HitEvent) && m.DueAt != nil:
			res.MistakesHit = append(res.MistakesHit, MistakeHit{Belief: m.Belief, Correction: m.Correction, Hits: m.Hits,
				DueAt: *m.DueAt, New: m.Source == string(mistakes.Quiz) && m.Hits == 1})
		case e.Kind == string(mistakes.ClearEvent):
			res.MovedOn = append(res.MovedOn, note(m))
		}
	}
	return res
}

// Summary sums up a quiz so far.
func (s *Service) Summary(ctx context.Context, quizID int64) (Summary, error) {
	z, err := s.St.Quiz(ctx, quizID)
	if err != nil {
		return Summary{}, err
	}
	answers, err := s.St.QuizAnswers(ctx, quizID)
	if err != nil {
		return Summary{}, err
	}
	tops, err := s.St.Topics(ctx)
	if err != nil {
		return Summary{}, err
	}
	before := rating.ThetaFromSkill(z.SkillBefore)
	if z.ThetaBefore != nil {
		before = *z.ThetaBefore
	}
	sum := Summary{QuizID: z.ID, Length: z.Length, Kind: z.Kind, EloBefore: rating.Elo(before), EloAfter: rating.Elo(before)}
	var lastRD *float64
	var topic store.Topic
	for _, t := range tops {
		if t.ID == z.TopicID {
			topic = t
			sum.Topic, sum.TopicSlug = t.Name, t.Slug
		}
	}
	subs, err := s.St.Subtopics(ctx, z.TopicID)
	if err != nil {
		return Summary{}, err
	}
	subName := map[int64]string{}
	for _, sub := range subs {
		subName[sub.ID] = sub.Name
	}
	ms, err := s.St.Mistakes(ctx, z.TopicID)
	if err != nil {
		return Summary{}, err
	}
	byID := map[int64]store.Mistake{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	sum.Review = []ReviewItem{}
	for _, a := range answers {
		if a.AnsweredAt == nil {
			continue
		}
		sum.Answered++
		sum.Expected += a.P
		if a.Correct != nil && *a.Correct {
			sum.Right++
		}
		if a.ThetaAfter != nil {
			sum.EloAfter = rating.Elo(*a.ThetaAfter)
		}
		if a.RDAfter != nil {
			lastRD = a.RDAfter
		}
		if z.Kind != Check && a.Slot == "rating" && math.Abs(a.P-z.Target) > picker.FitWindow {
			sum.OffLevel++
		}
		q, err := s.St.Question(ctx, a.QuestionID)
		if err != nil {
			return Summary{}, err
		}
		sum.Review = append(sum.Review, ReviewItem{Position: a.Position, QuestionID: q.ID, Subtopic: subName[q.SubtopicID], Kind: q.Kind,
			Stem: q.Stem, Code: q.Code, CodeLang: q.CodeLang, Correct: a.Correct != nil && *a.Correct,
			Options: feedback(q, a, byID)})
	}
	sum.LevelBefore, sum.Level = rating.LevelOfElo(sum.EloBefore), rating.LevelOfElo(sum.EloAfter)
	if sum.MistakesHit, sum.MistakesCleared, err = s.St.QuizMistakeCounts(ctx, quizID); err != nil {
		return Summary{}, err
	}
	events, err := s.St.QuizEvents(ctx, quizID)
	if err != nil {
		return Summary{}, err
	}
	sum.CameBack, sum.MovedOn = quizNotes(events, byID)
	r, err := s.St.Rating(ctx, z.TopicID)
	if err != nil {
		return Summary{}, err
	}
	b, err := s.bank(ctx, topic, r)
	if err != nil {
		return Summary{}, err
	}
	sum.BankState = b.State
	if lastRD != nil {
		sum.MarginAfter = rating.Margin(*lastRD)
	} else {
		sum.MarginAfter = rating.Margin(currentRD(r, s.now()))
	}
	if z.Kind == Check && z.FinishedAt != nil {
		if sum.Check, err = s.checkResult(ctx, z, sum); err != nil {
			return Summary{}, err
		}
	}
	return sum, nil
}

// quizNotes lists each mistake a quiz brought back, and each it moved on
// without bringing back, once, with where it stands now.
func quizNotes(events []store.MistakeEvent, ms map[int64]store.Mistake) (cameBack, movedOn []MistakeNote) {
	cameBack, movedOn = []MistakeNote{}, []MistakeNote{}
	hit, cleared := map[int64]bool{}, map[int64]bool{}
	var hitOrder, clearOrder []int64
	for _, e := range events {
		switch {
		case e.Kind == string(mistakes.HitEvent) && !hit[e.MistakeID]:
			hit[e.MistakeID] = true
			hitOrder = append(hitOrder, e.MistakeID)
		case e.Kind == string(mistakes.ClearEvent) && !cleared[e.MistakeID]:
			cleared[e.MistakeID] = true
			clearOrder = append(clearOrder, e.MistakeID)
		}
	}
	for _, id := range hitOrder {
		if m, ok := ms[id]; ok {
			cameBack = append(cameBack, note(m))
		}
	}
	for _, id := range clearOrder {
		if m, ok := ms[id]; ok && !hit[id] {
			movedOn = append(movedOn, note(m))
		}
	}
	return cameBack, movedOn
}

// Flag takes a question out of use and undoes the mistake hits it caused.
func (s *Service) Flag(ctx context.Context, questionID int64) error {
	return s.St.FlagQuestion(ctx, questionID)
}

// currentRD is a rating's deviation now: RD0 before any answer, widened for
// the days since it was last updated.
func currentRD(r store.Rating, now time.Time) float64 {
	if r.RD == nil {
		return rating.RD0
	}
	if r.RDAt == nil {
		return *r.RD
	}
	return rating.Widen(*r.RD, now.Sub(*r.RDAt).Hours()/24)
}

// BackfillRD gives every rating without a deviation one, by replaying its
// answers the way live answers update it. Ratings that have one are left
// alone, so running it again changes nothing.
func (s *Service) BackfillRD(ctx context.Context) error {
	ids, err := s.St.RatingsWithoutRD(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		hist, err := s.St.TopicAnswerHistory(ctx, id)
		if err != nil {
			return err
		}
		rd := rating.RD0
		var last *time.Time
		for i := range hist {
			h := hist[i]
			if last != nil {
				rd = rating.Widen(rd, h.At.Sub(*last).Hours()/24)
			}
			rd = rating.Narrow(rd, rating.Info(h.P, rating.GuessFloor(h.Multi, h.Options)), h.Repeat)
			last = &hist[i].At
		}
		if err := s.St.SetRD(ctx, id, &rd, last); err != nil {
			return err
		}
	}
	return nil
}

// CheckPoint is a skill check's result at one moment.
type CheckPoint struct {
	Elo    int       `json:"elo"`
	Margin int       `json:"margin"`
	At     time.Time `json:"at"`
}

// Missed counts a check's wrong answers in one subtopic.
type Missed struct {
	Subtopic string `json:"subtopic" doc:"subtopic name"`
	Count    int    `json:"count"`
}

// CheckResult is what a skill check measured.
type CheckResult struct {
	Elo      int         `json:"elo"`
	Margin   int         `json:"margin"`
	Level    string      `json:"level"`
	Previous *CheckPoint `json:"previous" doc:"the topic's check before this one, if any"`
	MissedIn []Missed    `json:"missed_in" doc:"subtopics with wrong answers, most first"`
}

// checkPoints turns stored check results into points, skipping any without answers.
func checkPoints(stats []store.CheckStat) []CheckRow {
	out := []CheckRow{}
	for _, c := range stats {
		if c.Theta == nil || c.RD == nil {
			continue
		}
		out = append(out, CheckRow{QuizID: c.QuizID, At: c.FinishedAt, Elo: rating.Elo(*c.Theta), Margin: rating.Margin(*c.RD),
			Answered: c.Answered})
	}
	return out
}

// checkResult builds a finished check's result from its summary so far.
func (s *Service) checkResult(ctx context.Context, z store.Quiz, sum Summary) (*CheckResult, error) {
	stats, err := s.St.TopicChecks(ctx, z.TopicID, 50)
	if err != nil {
		return nil, err
	}
	rows := checkPoints(stats)
	res := &CheckResult{MissedIn: []Missed{}}
	found := false
	for i, r := range rows {
		if r.QuizID != z.ID {
			continue
		}
		found = true
		res.Elo, res.Margin, res.Level = r.Elo, r.Margin, rating.LevelOfElo(r.Elo)
		if i+1 < len(rows) {
			p := rows[i+1]
			res.Previous = &CheckPoint{Elo: p.Elo, Margin: p.Margin, At: p.At}
		}
	}
	if !found {
		return nil, nil
	}
	counts := map[string]int{}
	for _, item := range sum.Review {
		if !item.Correct {
			counts[item.Subtopic]++
		}
	}
	for name, n := range counts {
		res.MissedIn = append(res.MissedIn, Missed{Subtopic: name, Count: n})
	}
	sort.Slice(res.MissedIn, func(a, b int) bool {
		if res.MissedIn[a].Count != res.MissedIn[b].Count {
			return res.MissedIn[a].Count > res.MissedIn[b].Count
		}
		return res.MissedIn[a].Subtopic < res.MissedIn[b].Subtopic
	})
	return res, nil
}
