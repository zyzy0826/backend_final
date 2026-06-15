package repository

import (
	"context"

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
	// TODO: implement
	return nil, nil
}

func (r *ProblemRepository) FindByID(ctx context.Context, id int) (*model.Problem, error) {
	// TODO: implement
	return nil, nil
}

// Upsert creates a new problem or updates an existing one (by ID), and replaces all testcases.
func (r *ProblemRepository) Upsert(ctx context.Context, p *model.Problem, testcases []model.Testcase) (*model.Problem, error) {
	// TODO: implement (use a transaction)
	return nil, nil
}

func (r *ProblemRepository) Delete(ctx context.Context, id int) error {
	// TODO: implement
	return nil
}

func (r *ProblemRepository) GetTestcases(ctx context.Context, problemID int) ([]model.Testcase, error) {
	// TODO: implement
	return nil, nil
}
