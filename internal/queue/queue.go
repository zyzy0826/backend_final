package queue

import (
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
	jobs chan Job
	jdg  *judge.Judge
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

// run dispatches jobs to goroutines bounded by maxConcurrent (semaphore pattern).
func (q *Queue) run(maxConcurrent int) {
	// TODO: implement using a buffered channel as a semaphore
}
