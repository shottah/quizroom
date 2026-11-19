package quiz

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"quizroom/internal/mistakes"
	"quizroom/internal/picker"
	"quizroom/internal/rating"
	"quizroom/internal/store"
)

// ThinBelow is how few questions at the learner's level make the bank "low":
// two quizzes' worth (see the Task 4 ruling). Fewer than the next quiz needs
// makes it "offlevel".
const ThinBelow = 20

// Bank is a topic's question bank as the learner sees it: how many questions
// fit their level now, how many are harder or easier, and how many are resting.
type Bank struct {
	Questions  int        `json:"questions" doc:"active questions"`
	Available  int        `json:"available" doc:"questions a quiz could show now: not shown in the last 7 days"`
	NearLevel  int        `json:"near_level" doc:"available questions at the learner's level (within the fit window of the target)"`
	Harder     int        `json:"harder" doc:"available questions predicted harder than the target"`
	Easier     int        `json:"easier" doc:"available questions predicted easier than the target"`
	Resting    int        `json:"resting" doc:"questions shown in the last 7 days"`
	BackAt     *time.Time `json:"back_at" doc:"when the first resting question can be shown again"`
	State      string     `json:"state" enum:"plenty,low,offlevel,blocked"`
	Writing    bool       `json:"writing" doc:"Claude is writing questions for this topic now"`
	QuizLength int        `json:"quiz_length" doc:"questions in the next quiz; 0 when a quiz can't start"`
}

// BankState grades a bank: blocked when a quiz can't start; offlevel when the
// next quiz would include questions off the learner's level; then low or
// plenty by how many available questions are at their level.
func BankState(near, available int) string {
	length, ok := picker.Length(available)
	switch {
	case !ok:
		return "blocked"
	case near < length:
		return "offlevel"
	case near < ThinBelow:
		return "low"
	default:
		return "plenty"
	}
}

type Card struct {
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Elo         int        `json:"elo" doc:"0–300"`
	Margin      int        `json:"margin" doc:"± Elo points: one standard deviation"`
	Level       string     `json:"level"`
	Answers     int        `json:"answers"`
	MistakesDue int        `json:"mistakes_due"`
	Questions   int        `json:"questions"`
	Bank        Bank       `json:"bank"`
	Empty       bool       `json:"empty"`
	Spark       []int      `json:"spark" doc:"Elo at the end of each day with answers, over the last 30 days, oldest first"`
	LastQuiz    *QuizBrief `json:"last_quiz" doc:"the latest finished quiz"`
	OpenQuiz    *QuizBrief `json:"open_quiz" doc:"the latest quiz, when it isn't finished"`
}

// QuizBrief is one quiz in a line: how far it got and how the skill moved.
type QuizBrief struct {
	ID        int64     `json:"id"`
	StartedAt time.Time `json:"started_at"`
	Length    int       `json:"length"`
	Answered  int       `json:"answered"`
	Right     int       `json:"right"`
	EloBefore int       `json:"elo_before"`
	EloAfter  int       `json:"elo_after"`
	Finished  bool      `json:"finished"`
	Kind      string    `json:"kind" enum:"practice,check"`
}

// QuizRow is a quiz in the topic page's list of recent quizzes.
type QuizRow struct {
	QuizBrief
	Expected float64 `json:"expected" doc:"right answers predicted"`
	CameBack int     `json:"came_back" doc:"mistakes the learner made again"`
	MovedOn  int     `json:"moved_on" doc:"mistakes that moved a step towards done"`
}

func quizRows(stats []store.QuizStat) []QuizRow {
	out := make([]QuizRow, len(stats))
	for i, q := range stats {
		before := rating.ThetaFromSkill(q.SkillBefore)
		if q.ThetaBefore != nil {
			before = *q.ThetaBefore
		}
		after := before
		if q.LastTheta != nil {
			after = *q.LastTheta
		}
		out[i] = QuizRow{QuizBrief: QuizBrief{ID: q.ID, StartedAt: q.StartedAt, Length: q.Length, Answered: q.Answered,
			Right: q.Right, EloBefore: rating.Elo(before), EloAfter: rating.Elo(after), Finished: q.FinishedAt != nil,
			Kind: q.Kind},
			Expected: q.Expected, CameBack: q.Hits, MovedOn: q.Clears}
	}
	return out
}

// quizBriefs picks the open quiz (the latest, when unfinished) and the latest
// finished one. An older unfinished quiz is abandoned, not open.
func quizBriefs(rows []QuizRow) (open, last *QuizBrief) {
	for i, r := range rows {
		if !r.Finished && i == 0 {
			b := r.QuizBrief
			open = &b
		}
		if r.Finished {
			b := r.QuizBrief
			return open, &b
		}
	}
	return open, nil
}

type Point struct {
	At     time.Time `json:"at"`
	Elo    int       `json:"elo"`
	QuizID int64     `json:"quiz_id"`
	Kind   string    `json:"kind" enum:"practice,check"`
}

type SubtopicView struct {
	Slug     string  `json:"slug"`
	Name     string  `json:"name"`
	Answered int     `json:"answered"`
	Right    int     `json:"right"`
	Expected float64 `json:"expected"`
	Weak     bool    `json:"weak"`
}

type MistakeView struct {
	ID         int64      `json:"id"`
	Topic      string     `json:"topic" doc:"topic slug"`
	Subtopic   string     `json:"subtopic" doc:"subtopic slug"`
	Belief     string     `json:"belief"`
	Correction string     `json:"correction"`
	Source     string     `json:"source"`
	Status     string     `json:"status" enum:"watching,active,resolved"`
	Stage      int        `json:"stage"`
	Hits       int        `json:"hits"`
	DueAt      *time.Time `json:"due_at"`
	// NeedsQuestions is an active mistake with no unseen question to test it.
	NeedsQuestions bool `json:"needs_questions" doc:"no unseen question tests this mistake yet"`
}

type Detail struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Elo         int            `json:"elo" doc:"0–300"`
	Margin      int            `json:"margin" doc:"± Elo points: one standard deviation"`
	Level       string         `json:"level"`
	History     []Point        `json:"history"`
	Subtopics   []SubtopicView `json:"subtopics"`
	Mistakes    []MistakeView  `json:"mistakes"`
	Bank        Bank           `json:"bank"`
	Recent      Recent         `json:"recent" doc:"the last 40 answers"`
	Quizzes     []QuizRow      `json:"quizzes" doc:"the last 10 quizzes, newest first"`
	Checks      []CheckRow     `json:"checks" doc:"finished skill checks, newest first"`
}

// recentAnswers and recentQuizzes size the topic page's "lately" figures.
const (
	recentAnswers = 40
	recentQuizzes = 10
	recentChecks  = 20
)

// CheckRow is one finished skill check on the topic page.
type CheckRow struct {
	QuizID   int64     `json:"quiz_id"`
	At       time.Time `json:"at"`
	Elo      int       `json:"elo"`
	Margin   int       `json:"margin"`
	Answered int       `json:"answered"`
}

type Recent struct {
	Answered int     `json:"answered"`
	Right    int     `json:"right"`
	Expected float64 `json:"expected"`
}

type TopicSummary struct {
	Slug           string         `json:"slug"`
	Elo            int            `json:"elo" doc:"0–300"`
	Margin         int            `json:"margin" doc:"± Elo points: one standard deviation"`
	Level          string         `json:"level"`
	Weakest        []SubtopicView `json:"weakest"`
	ActiveMistakes []MistakeView  `json:"active_mistakes"`
	Recent         Recent         `json:"recent"`
}

type AddMistakeInput struct {
	Topic      string `json:"topic" doc:"topic slug"`
	Subtopic   string `json:"subtopic" doc:"subtopic slug"`
	Belief     string `json:"belief" minLength:"1" maxLength:"300"`
	Correction string `json:"correction" minLength:"1" maxLength:"300"`
	Source     string `json:"source" enum:"mock,chat"`
}

// ErrBadSource is an AddMistake source other than mock or chat.
var ErrBadSource = errors.New("source must be mock or chat")

// bank sizes up a topic's questions against the learner's rating r.
func (s *Service) bank(ctx context.Context, t store.Topic, r store.Rating) (Bank, error) {
	settings, err := s.St.Settings(ctx)
	if err != nil {
		return Bank{}, err
	}
	now := s.now()
	el, err := s.St.Eligible(ctx, t.ID, 0, now)
	if err != nil {
		return Bank{}, err
	}
	total, err := s.St.CountQuestions(ctx, t.ID)
	if err != nil {
		return Bank{}, err
	}
	back, err := s.St.BackAt(ctx, t.ID, now)
	if err != nil {
		return Bank{}, err
	}
	writing, err := s.St.PendingJobForTopic(ctx, t.Slug)
	if err != nil {
		return Bank{}, err
	}
	b := Bank{Questions: total, Available: len(el), Resting: total - len(el), BackAt: back, Writing: writing}
	for _, e := range el {
		p := rating.Predict(r.Theta, e.B, guessFloor(e.Question))
		switch {
		case math.Abs(p-settings.Target) <= picker.FitWindow:
			b.NearLevel++
		case p < settings.Target:
			b.Harder++
		default:
			b.Easier++
		}
	}
	b.State = BankState(b.NearLevel, b.Available)
	if n, ok := picker.Length(b.Available); ok {
		b.QuizLength = n
	}
	return b, nil
}

// Cards are the home page's topic cards.
func (s *Service) Cards(ctx context.Context) ([]Card, error) {
	tops, err := s.St.Topics(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	cards := []Card{}
	for _, t := range tops {
		r, err := s.St.Rating(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		b, err := s.bank(ctx, t, r)
		if err != nil {
			return nil, err
		}
		due, err := s.St.DueMistakes(ctx, t.ID, now)
		if err != nil {
			return nil, err
		}
		hist, err := s.St.SkillHistory(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		qs, err := s.St.TopicQuizzes(ctx, t.ID, recentQuizzes)
		if err != nil {
			return nil, err
		}
		open, last := quizBriefs(quizRows(qs))
		elo := rating.Elo(r.Theta)
		cards = append(cards, Card{Slug: t.Slug, Name: t.Name, Elo: elo, Margin: rating.Margin(currentRD(r, now)),
			Level: rating.LevelOfElo(elo), Answers: r.N,
			MistakesDue: len(due), Questions: b.Questions, Bank: b, Empty: b.Questions == 0, Spark: spark(hist, now),
			LastQuiz: last, OpenQuiz: open})
	}
	return cards, nil
}

// spark is the Elo at the end of each UTC day with answers, over the last 30 days.
func spark(hist []store.SkillPoint, now time.Time) []int {
	out := []int{}
	from := now.Add(-30 * 24 * time.Hour)
	var day string
	for _, p := range hist {
		if p.At.Before(from) {
			continue
		}
		d := p.At.UTC().Format(time.DateOnly)
		if d == day && len(out) > 0 {
			out[len(out)-1] = rating.Elo(p.Theta)
			continue
		}
		day = d
		out = append(out, rating.Elo(p.Theta))
	}
	return out
}

// weakMin is how many answers a subtopic needs before it can be a weak spot.
const weakMin = 5

// subtopicViews flags every weak spot: a subtopic with weakMin or more
// answers and fewer right than predicted. The topic page and the mock
// interview summary share this one rule.
func subtopicViews(stats []store.SubtopicStat) []SubtopicView {
	out := make([]SubtopicView, len(stats))
	for i, st := range stats {
		out[i] = SubtopicView{Slug: st.Slug, Name: st.Name, Answered: st.Answered, Right: st.Right, Expected: st.Expected,
			Weak: st.Answered >= weakMin && float64(st.Right) < st.Expected}
	}
	return out
}

func (s *Service) mistakeViews(ctx context.Context, ms []store.Mistake) ([]MistakeView, error) {
	tops, err := s.St.Topics(ctx)
	if err != nil {
		return nil, err
	}
	topicSlug := map[int64]string{}
	subSlug := map[int64]string{}
	for _, t := range tops {
		topicSlug[t.ID] = t.Slug
		subs, err := s.St.Subtopics(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		for _, sub := range subs {
			subSlug[sub.ID] = sub.Slug
		}
	}
	out := make([]MistakeView, 0, len(ms))
	for _, m := range ms {
		out = append(out, MistakeView{ID: m.ID, Topic: topicSlug[m.TopicID], Subtopic: subSlug[m.SubtopicID], Belief: m.Belief,
			Correction: m.Correction, Source: m.Source, Status: m.Status, Stage: m.Stage, Hits: m.Hits, DueAt: m.DueAt,
			NeedsQuestions: m.NeedsQuestions})
	}
	return out, nil
}

// Detail is the topic page: the skill history, subtopics and mistakes.
func (s *Service) Detail(ctx context.Context, slug string) (Detail, error) {
	t, err := s.St.TopicBySlug(ctx, slug)
	if err != nil {
		return Detail{}, err
	}
	r, err := s.St.Rating(ctx, t.ID)
	if err != nil {
		return Detail{}, err
	}
	hist, err := s.St.SkillHistory(ctx, t.ID)
	if err != nil {
		return Detail{}, err
	}
	stats, err := s.St.SubtopicStats(ctx, t.ID)
	if err != nil {
		return Detail{}, err
	}
	ms, err := s.St.Mistakes(ctx, t.ID)
	if err != nil {
		return Detail{}, err
	}
	views, err := s.mistakeViews(ctx, ms)
	if err != nil {
		return Detail{}, err
	}
	points := make([]Point, len(hist))
	for i, p := range hist {
		points[i] = Point{At: p.At, Elo: rating.Elo(p.Theta), QuizID: p.QuizID, Kind: p.Kind}
	}
	b, err := s.bank(ctx, t, r)
	if err != nil {
		return Detail{}, err
	}
	answered, right, expected, err := s.St.RecentStats(ctx, t.ID, recentAnswers)
	if err != nil {
		return Detail{}, err
	}
	qs, err := s.St.TopicQuizzes(ctx, t.ID, recentQuizzes)
	if err != nil {
		return Detail{}, err
	}
	checks, err := s.St.TopicChecks(ctx, t.ID, recentChecks)
	if err != nil {
		return Detail{}, err
	}
	elo := rating.Elo(r.Theta)
	return Detail{Slug: t.Slug, Name: t.Name, Description: t.Description, Elo: elo, Margin: rating.Margin(currentRD(r, s.now())),
		Level:   rating.LevelOfElo(elo),
		History: points, Subtopics: subtopicViews(stats), Mistakes: views, Bank: b,
		Recent: Recent{Answered: answered, Right: right, Expected: expected}, Quizzes: quizRows(qs), Checks: checkPoints(checks)}, nil
}

// TopicSummary is the compact view an AI agent reads, for example before a mock interview.
func (s *Service) TopicSummary(ctx context.Context, slug string) (TopicSummary, error) {
	t, err := s.St.TopicBySlug(ctx, slug)
	if err != nil {
		return TopicSummary{}, err
	}
	r, err := s.St.Rating(ctx, t.ID)
	if err != nil {
		return TopicSummary{}, err
	}
	stats, err := s.St.SubtopicStats(ctx, t.ID)
	if err != nil {
		return TopicSummary{}, err
	}
	weakest := []SubtopicView{}
	for _, v := range subtopicViews(stats) {
		if v.Weak {
			weakest = append(weakest, v)
		}
	}
	sort.SliceStable(weakest, func(a, b int) bool {
		return float64(weakest[a].Right)-weakest[a].Expected < float64(weakest[b].Right)-weakest[b].Expected
	})
	ms, err := s.St.Mistakes(ctx, t.ID)
	if err != nil {
		return TopicSummary{}, err
	}
	var active []store.Mistake
	for _, m := range ms {
		if m.Status == string(mistakes.Active) {
			active = append(active, m)
		}
	}
	views, err := s.mistakeViews(ctx, active)
	if err != nil {
		return TopicSummary{}, err
	}
	answered, right, expected, err := s.St.RecentStats(ctx, t.ID, 50)
	if err != nil {
		return TopicSummary{}, err
	}
	elo := rating.Elo(r.Theta)
	return TopicSummary{Slug: t.Slug, Elo: elo, Margin: rating.Margin(currentRD(r, s.now())), Level: rating.LevelOfElo(elo),
		Weakest: weakest, ActiveMistakes: views,
		Recent: Recent{Answered: answered, Right: right, Expected: expected}}, nil
}

// Mistakes lists mistakes for a topic slug, or all of them for "".
func (s *Service) Mistakes(ctx context.Context, topicSlug string) ([]MistakeView, error) {
	var topicID int64
	if topicSlug != "" {
		t, err := s.St.TopicBySlug(ctx, topicSlug)
		if err != nil {
			return nil, err
		}
		topicID = t.ID
	}
	ms, err := s.St.Mistakes(ctx, topicID)
	if err != nil {
		return nil, err
	}
	return s.mistakeViews(ctx, ms)
}

// AddMistake records a slip from a mock interview or the chat.
func (s *Service) AddMistake(ctx context.Context, in AddMistakeInput) (MistakeView, error) {
	var src mistakes.Source
	switch in.Source {
	case "mock":
		src = mistakes.Mock
	case "chat":
		src = mistakes.Chat
	default:
		return MistakeView{}, ErrBadSource
	}
	m, _, err := s.St.AddMistake(ctx, in.Topic, in.Subtopic, in.Belief, in.Correction, src, s.now())
	if err != nil {
		return MistakeView{}, err
	}
	views, err := s.mistakeViews(ctx, []store.Mistake{m})
	if err != nil {
		return MistakeView{}, err
	}
	return views[0], nil
}
