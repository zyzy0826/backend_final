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

// Create inserts a new submission (status = pending) together with an empty
// submission_logs row, so later log updates can rely on the row existing.
func (r *SubmissionRepository) Create(ctx context.Context, userID, problemID int, operatorID, sourcePath string) (*model.Submission, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const ins = `
		INSERT INTO submissions (operator_id, user_id, problem_id, source_path)
		VALUES ($1, $2, $3, $4)
		RETURNING id, operator_id, user_id, problem_id, status, source_path, created_at`

	var s model.Submission
	err = tx.QueryRow(ctx, ins, operatorID, userID, problemID, sourcePath).
		Scan(&s.ID, &s.OperatorID, &s.UserID, &s.ProblemID, &s.Status, &s.SourcePath, &s.CreatedAt)
	if err != nil {
		return nil, err
	}

	if _, err = tx.Exec(ctx, `INSERT INTO submission_logs (submission_id) VALUES ($1)`, s.ID); err != nil {
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SubmissionRepository) FindByOperatorID(ctx context.Context, operatorID string) (*model.SubmissionDetail, error) {
	const q = `
		SELECT s.id, s.operator_id, s.user_id, s.problem_id, s.status, s.source_path, s.created_at,
		       COALESCE(l.configure_log, ''), COALESCE(l.compile_log, ''), COALESCE(l.output_log, '')
		FROM submissions s
		LEFT JOIN submission_logs l ON l.submission_id = s.id
		WHERE s.operator_id = $1`

	var d model.SubmissionDetail
	err := r.db.QueryRow(ctx, q, operatorID).Scan(
		&d.ID, &d.OperatorID, &d.UserID, &d.ProblemID, &d.Status, &d.SourcePath, &d.CreatedAt,
		&d.ConfigureLog, &d.CompileLog, &d.OutputLog,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *SubmissionRepository) ListByUser(ctx context.Context, userID int) ([]model.Submission, error) {
	const q = `
		SELECT id, operator_id, user_id, problem_id, status, source_path, created_at
		FROM submissions
		WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subs := []model.Submission{}
	for rows.Next() {
		var s model.Submission
		if err := rows.Scan(&s.ID, &s.OperatorID, &s.UserID, &s.ProblemID, &s.Status, &s.SourcePath, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// ResetForRejudge puts a submission back to pending and clears its previous logs,
// so a re-run starts from a clean slate.
func (r *SubmissionRepository) ResetForRejudge(ctx context.Context, id int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `UPDATE submissions SET status = 'pending' WHERE id = $1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE submission_logs
		SET configure_log = '', compile_log = '', output_log = ''
		WHERE submission_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SubmissionRepository) UpdateStatus(ctx context.Context, id int, status model.Status) error {
	_, err := r.db.Exec(ctx, `UPDATE submissions SET status = $1 WHERE id = $2`, status, id)
	return err
}

// UpdateStatusWithLogs updates the final status and the three log segments atomically.
func (r *SubmissionRepository) UpdateStatusWithLogs(ctx context.Context, id int, status model.Status, configureLog, compileLog, outputLog string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `UPDATE submissions SET status = $1 WHERE id = $2`, status, id); err != nil {
		return err
	}

	const logQ = `
		INSERT INTO submission_logs (submission_id, configure_log, compile_log, output_log)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (submission_id) DO UPDATE
		SET configure_log = EXCLUDED.configure_log,
		    compile_log   = EXCLUDED.compile_log,
		    output_log    = EXCLUDED.output_log`
	if _, err = tx.Exec(ctx, logQ, id, configureLog, compileLog, outputLog); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *SubmissionRepository) GetProblemStats(ctx context.Context, problemID int) (*model.ProblemStats, error) {
	const q = `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'AC'),
			COUNT(*) FILTER (WHERE status = 'WA'),
			COUNT(*) FILTER (WHERE status = 'CE'),
			COUNT(*) FILTER (WHERE status = 'RE'),
			COUNT(*) FILTER (WHERE status = 'TLE')
		FROM submissions
		WHERE problem_id = $1`

	stats := &model.ProblemStats{ProblemID: problemID}
	err := r.db.QueryRow(ctx, q, problemID).
		Scan(&stats.Total, &stats.AC, &stats.WA, &stats.CE, &stats.RE, &stats.TLE)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (r *SubmissionRepository) GetUserStats(ctx context.Context, userID int) (*model.UserStats, error) {
	const q = `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'AC'),
			COUNT(DISTINCT problem_id) FILTER (WHERE status = 'AC')
		FROM submissions
		WHERE user_id = $1`

	stats := &model.UserStats{UserID: userID}
	err := r.db.QueryRow(ctx, q, userID).
		Scan(&stats.Total, &stats.AC, &stats.Solved)
	if err != nil {
		return nil, err
	}
	return stats, nil
}
