package queue

import (
	"context"

	"regs/internal/judge"
	"regs/internal/model"
)

type Job struct {
	SubmissionID int
	OperatorID   string
	ProblemID    int
	ZipPath      string
	Testcases    []model.Testcase
	TimeLimit    int
}

type Queue struct {
	jobs  chan Job
	jdg   *judge.Judge
}

func New(maxConcurrent int, jdg *judge.Judge) *Queue {
	q := &Queue{
		jobs: make(chan Job, 256),
		jdg:  jdg,
	}
	go q.run(maxConcurrent)
	return q
}

func (q *Queue) Push(job Job) {
	q.jobs <- job
}

// run dispatches jobs to goroutines, bounded by maxConcurrent (semaphore pattern).
func (q *Queue) run(maxConcurrent int) {
	sem := make(chan struct{}, maxConcurrent)
	for job := range q.jobs {
		sem <- struct{}{}
		go func(j Job) {
			defer func() { <-sem }()
			q.jdg.RunJob(context.Background(), judge.JobInput{
				SubmissionID: j.SubmissionID,
				OperatorID:   j.OperatorID,
				ProblemID:    j.ProblemID,
				ZipPath:      j.ZipPath,
				Testcases:    j.Testcases,
				TimeLimit:    j.TimeLimit,
			})
		}(job)
	}
}
