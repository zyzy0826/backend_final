package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"regs/internal/model"
)

type SubmissionRepository struct {
	db *pgxpool.Pool
}

func NewSubmissionRepository(db *pgxpool.Pool) *SubmissionRepository {
	return &SubmissionRepository{db: db}
}

func (r *SubmissionRepository) Create(ctx context.Context, userID, problemID int, operatorID, sourcePath string) (*model.Submission, error) {
	// TODO: implement (use a transaction to insert submission + submission_logs row)
	return nil, nil
}

func (r *SubmissionRepository) FindByOperatorID(ctx context.Context, operatorID string) (*model.SubmissionDetail, error) {
	// TODO: implement (JOIN submissions and submission_logs)
	return nil, nil
}

func (r *SubmissionRepository) ListByUser(ctx context.Context, userID int) ([]model.Submission, error) {
	// TODO: implement
	return nil, nil
}

func (r *SubmissionRepository) UpdateStatus(ctx context.Context, id int, status model.Status) error {
	// TODO: implement
	return nil
}

func (r *SubmissionRepository) UpdateStatusWithLogs(ctx context.Context, id int, status model.Status, configureLog, compileLog, outputLog string) error {
	// TODO: implement (use a transaction to update both submissions and submission_logs)
	return nil
}

func (r *SubmissionRepository) GetProblemStats(ctx context.Context, problemID int) (*model.ProblemStats, error) {
	// TODO: implement (aggregate query with FILTER)
	return nil, nil
}

func (r *SubmissionRepository) GetUserStats(ctx context.Context, userID int) (*model.UserStats, error) {
	// TODO: implement (aggregate query with FILTER and DISTINCT)
	return nil, nil
}
