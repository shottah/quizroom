package store

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

//go:embed seed.json
var seedJSON []byte

type seedData struct {
	Topics []struct {
		Slug            string   `json:"slug"`
		Name            string   `json:"name"`
		Description     string   `json:"description"`
		ConfusionGroups []string `json:"confusion_groups"`
		Subtopics       []struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"subtopics"`
	} `json:"topics"`
}

// seed applies seed.json idempotently: topics and subtopics by slug. Existing
// rows are never changed. No mistakes are seeded: a learner's list holds only
// the mistakes they make.
func (s *Store) seed(ctx context.Context) error {
	var d seedData
	if err := json.Unmarshal(seedJSON, &d); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, t := range d.Topics {
			groups, _ := json.Marshal(t.ConfusionGroups)
			if _, err := tx.Exec(ctx, `INSERT INTO topics (slug, name, description, confusion_groups)
				VALUES ($1, $2, $3, $4) ON CONFLICT (slug) DO NOTHING`, t.Slug, t.Name, t.Description, groups); err != nil {
				return err
			}
			for i, sub := range t.Subtopics {
				if _, err := tx.Exec(ctx, `INSERT INTO subtopics (topic_id, slug, name, position)
					SELECT id, $2, $3, $4 FROM topics WHERE slug = $1
					ON CONFLICT (topic_id, slug) DO NOTHING`, t.Slug, sub.Slug, sub.Name, i); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
