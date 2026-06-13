package repository

import (
	"context"
	"fmt"

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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var sub model.Submission
	err = tx.QueryRow(ctx,
		`INSERT INTO submissions (operator_id, user_id, problem_id, source_path)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, operator_id, user_id, problem_id, status, source_path, created_at`,
		operatorID, userID, problemID, sourcePath,
	).Scan(&sub.ID, &sub.OperatorID, &sub.UserID, &sub.ProblemID, &sub.Status, &sub.SourcePath, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create submission: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO submission_logs (submission_id) VALUES ($1)`, sub.ID,
	); err != nil {
		return nil, err
	}

	return &sub, tx.Commit(ctx)
}

func (r *SubmissionRepository) FindByOperatorID(ctx context.Context, operatorID string) (*model.SubmissionDetail, error) {
	var d model.SubmissionDetail
	err := r.db.QueryRow(ctx, `
		SELECT s.id, s.operator_id, s.user_id, s.problem_id, s.status, s.source_path, s.created_at,
		       l.configure_log, l.compile_log, l.output_log
		FROM submissions s
		LEFT JOIN submission_logs l ON l.submission_id = s.id
		WHERE s.operator_id = $1`, operatorID,
	).Scan(
		&d.ID, &d.OperatorID, &d.UserID, &d.ProblemID, &d.Status, &d.SourcePath, &d.CreatedAt,
		&d.ConfigureLog, &d.CompileLog, &d.OutputLog,
	)
	if err != nil {
		return nil, fmt.Errorf("find submission: %w", err)
	}
	return &d, nil
}

func (r *SubmissionRepository) ListByUser(ctx context.Context, userID int) ([]model.Submission, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, operator_id, user_id, problem_id, status, source_path, created_at
		 FROM submissions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []model.Submission
	for rows.Next() {
		var s model.Submission
		if err := rows.Scan(&s.ID, &s.OperatorID, &s.UserID, &s.ProblemID, &s.Status, &s.SourcePath, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func (r *SubmissionRepository) UpdateStatus(ctx context.Context, id int, status model.Status) error {
	_, err := r.db.Exec(ctx, `UPDATE submissions SET status=$1 WHERE id=$2`, status, id)
	return err
}

func (r *SubmissionRepository) UpdateStatusWithLogs(ctx context.Context, id int, status model.Status, configureLog, compileLog, outputLog string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE submissions SET status=$1 WHERE id=$2`, status, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE submission_logs SET configure_log=$1, compile_log=$2, output_log=$3 WHERE submission_id=$4`,
		configureLog, compileLog, outputLog, id,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SubmissionRepository) GetProblemStats(ctx context.Context, problemID int) (*model.ProblemStats, error) {
	var s model.ProblemStats
	s.ProblemID = problemID
	err := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'AC'),
			COUNT(*) FILTER (WHERE status = 'WA'),
			COUNT(*) FILTER (WHERE status = 'CE'),
			COUNT(*) FILTER (WHERE status = 'RE'),
			COUNT(*) FILTER (WHERE status = 'TLE')
		FROM submissions WHERE problem_id = $1`, problemID,
	).Scan(&s.Total, &s.AC, &s.WA, &s.CE, &s.RE, &s.TLE)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SubmissionRepository) GetUserStats(ctx context.Context, userID int) (*model.UserStats, error) {
	var s model.UserStats
	s.UserID = userID
	err := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'AC'),
			COUNT(DISTINCT problem_id) FILTER (WHERE status = 'AC')
		FROM submissions WHERE user_id = $1`, userID,
	).Scan(&s.Total, &s.AC, &s.Solved)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
