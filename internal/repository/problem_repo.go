package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
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
	const q = `SELECT id, title, description, time_limit, package_path, created_at FROM problems ORDER BY id`

	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	problems := []model.Problem{}
	for rows.Next() {
		var p model.Problem
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.TimeLimit, &p.PackagePath, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.ResolveJudgeMode()
		problems = append(problems, p)
	}
	return problems, rows.Err()
}

func (r *ProblemRepository) FindByID(ctx context.Context, id int) (*model.Problem, error) {
	const q = `SELECT id, title, description, time_limit, package_path, created_at FROM problems WHERE id = $1`

	var p model.Problem
	err := r.db.QueryRow(ctx, q, id).
		Scan(&p.ID, &p.Title, &p.Description, &p.TimeLimit, &p.PackagePath, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.ResolveJudgeMode()
	return &p, nil
}

// Upsert creates a new problem or updates an existing one (by ID), and replaces all testcases.
// The whole operation runs in a single transaction so a problem never ends up with a
// half-updated set of testcases.
func (r *ProblemRepository) Upsert(ctx context.Context, p *model.Problem, testcases []model.Testcase) (*model.Problem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var result model.Problem

	// id <= 0 (absent, zero, or negative) always means "create a new problem"
	// with a server-assigned id; only a positive id updates (or inserts with
	// that explicit id).
	if p.ID > 0 {
		// Try to update an existing problem first (package_path is managed
		// separately via UpdatePackagePath and left untouched here).
		const upd = `
			UPDATE problems SET title = $1, description = $2, time_limit = $3
			WHERE id = $4
			RETURNING id, title, description, time_limit, package_path, created_at`
		err = tx.QueryRow(ctx, upd, p.Title, p.Description, p.TimeLimit, p.ID).
			Scan(&result.ID, &result.Title, &result.Description, &result.TimeLimit, &result.PackagePath, &result.CreatedAt)

		// The client supplied an ID that does not exist yet → insert with that explicit ID.
		if err == pgx.ErrNoRows {
			const ins = `
				INSERT INTO problems (id, title, description, time_limit)
				VALUES ($1, $2, $3, $4)
				RETURNING id, title, description, time_limit, package_path, created_at`
			err = tx.QueryRow(ctx, ins, p.ID, p.Title, p.Description, p.TimeLimit).
				Scan(&result.ID, &result.Title, &result.Description, &result.TimeLimit, &result.PackagePath, &result.CreatedAt)
		}
		if err != nil {
			return nil, err
		}
	} else {
		const ins = `
			INSERT INTO problems (title, description, time_limit)
			VALUES ($1, $2, $3)
			RETURNING id, title, description, time_limit, package_path, created_at`
		err = tx.QueryRow(ctx, ins, p.Title, p.Description, p.TimeLimit).
			Scan(&result.ID, &result.Title, &result.Description, &result.TimeLimit, &result.PackagePath, &result.CreatedAt)
		if err != nil {
			return nil, err
		}
	}

	// Replace all testcases: drop the old ones, insert the new set.
	if _, err = tx.Exec(ctx, `DELETE FROM testcases WHERE problem_id = $1`, result.ID); err != nil {
		return nil, err
	}
	for _, tc := range testcases {
		if _, err = tx.Exec(ctx,
			`INSERT INTO testcases (problem_id, input, expected) VALUES ($1, $2, $3)`,
			result.ID, tc.Input, tc.Expected); err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	result.ResolveJudgeMode()
	return &result, nil
}

// UpdatePackagePath records where a problem's package ZIP is stored (test-based mode).
func (r *ProblemRepository) UpdatePackagePath(ctx context.Context, id int, path string) error {
	ct, err := r.db.Exec(ctx, `UPDATE problems SET package_path = $1 WHERE id = $2`, path, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *ProblemRepository) Delete(ctx context.Context, id int) error {
	ct, err := r.db.Exec(ctx, `DELETE FROM problems WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *ProblemRepository) GetTestcases(ctx context.Context, problemID int) ([]model.Testcase, error) {
	const q = `SELECT id, problem_id, input, expected FROM testcases WHERE problem_id = $1 ORDER BY id`

	rows, err := r.db.Query(ctx, q, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	testcases := []model.Testcase{}
	for rows.Next() {
		var tc model.Testcase
		if err := rows.Scan(&tc.ID, &tc.ProblemID, &tc.Input, &tc.Expected); err != nil {
			return nil, err
		}
		testcases = append(testcases, tc)
	}
	return testcases, rows.Err()
}
