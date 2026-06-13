package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"regs/internal/model"
)

type ProblemRepository struct {
	db *pgxpool.Pool
}

func NewProblemRepository(db *pgxpool.Pool) *ProblemRepository {
	return &ProblemRepository{db: db}
}

func (r *ProblemRepository) List(ctx context.Context) ([]model.Problem, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, title, description, time_limit, created_at FROM problems ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		var p model.Problem
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.TimeLimit, &p.CreatedAt); err != nil {
			return nil, err
		}
		problems = append(problems, p)
	}
	return problems, rows.Err()
}

func (r *ProblemRepository) FindByID(ctx context.Context, id int) (*model.Problem, error) {
	var p model.Problem
	err := r.db.QueryRow(ctx,
		`SELECT id, title, description, time_limit, created_at FROM problems WHERE id = $1`, id,
	).Scan(&p.ID, &p.Title, &p.Description, &p.TimeLimit, &p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("find problem: %w", err)
	}
	return &p, nil
}

// Upsert creates a new problem or updates an existing one (by ID), and replaces all testcases.
func (r *ProblemRepository) Upsert(ctx context.Context, p *model.Problem, testcases []model.Testcase) (*model.Problem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var result model.Problem
	if p.ID > 0 {
		err = tx.QueryRow(ctx,
			`UPDATE problems SET title=$1, description=$2, time_limit=$3
			 WHERE id=$4
			 RETURNING id, title, description, time_limit, created_at`,
			p.Title, p.Description, p.TimeLimit, p.ID,
		).Scan(&result.ID, &result.Title, &result.Description, &result.TimeLimit, &result.CreatedAt)
	} else {
		err = tx.QueryRow(ctx,
			`INSERT INTO problems (title, description, time_limit)
			 VALUES ($1, $2, $3)
			 RETURNING id, title, description, time_limit, created_at`,
			p.Title, p.Description, p.TimeLimit,
		).Scan(&result.ID, &result.Title, &result.Description, &result.TimeLimit, &result.CreatedAt)
	}
	if err != nil {
		return nil, fmt.Errorf("upsert problem: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM testcases WHERE problem_id = $1`, result.ID); err != nil {
		return nil, err
	}
	for _, tc := range testcases {
		if _, err := tx.Exec(ctx,
			`INSERT INTO testcases (problem_id, input, expected) VALUES ($1, $2, $3)`,
			result.ID, tc.Input, tc.Expected,
		); err != nil {
			return nil, err
		}
	}

	return &result, tx.Commit(ctx)
}

func (r *ProblemRepository) Delete(ctx context.Context, id int) error {
	_, err := r.db.Exec(ctx, `DELETE FROM problems WHERE id = $1`, id)
	return err
}

func (r *ProblemRepository) GetTestcases(ctx context.Context, problemID int) ([]model.Testcase, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, problem_id, input, expected FROM testcases WHERE problem_id = $1 ORDER BY id`, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tcs []model.Testcase
	for rows.Next() {
		var tc model.Testcase
		if err := rows.Scan(&tc.ID, &tc.ProblemID, &tc.Input, &tc.Expected); err != nil {
			return nil, err
		}
		tcs = append(tcs, tc)
	}
	return tcs, rows.Err()
}
