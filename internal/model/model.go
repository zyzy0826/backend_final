/* 定義物件型別 */

package model

import "time"

// --- Roles ---

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// --- User ---

type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// --- Problem ---

type Problem struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	TimeLimit   int       `json:"time_limit"`
	CreatedAt   time.Time `json:"created_at"`
}

type Testcase struct {
	ID        int    `json:"id"`
	ProblemID int    `json:"problem_id"`
	Input     string `json:"input"`
	Expected  string `json:"expected"`
}

// --- Submission ---

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusAC      Status = "AC"
	StatusWA      Status = "WA"
	StatusCE      Status = "CE"
	StatusSE      Status = "SE"
	StatusRE      Status = "RE"
	StatusTLE     Status = "TLE"
)

type Submission struct {
	ID         int       `json:"id"`
	OperatorID string    `json:"operator_id"`
	UserID     int       `json:"user_id"`
	ProblemID  int       `json:"problem_id"`
	Status     Status    `json:"status"`
	SourcePath string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}

type SubmissionDetail struct {
	Submission          // 嵌入，類似於繼承的概念
	ConfigureLog string `json:"configure_log,omitempty"`
	CompileLog   string `json:"compile_log,omitempty"`
	OutputLog    string `json:"output_log,omitempty"`
}

// --- Stats ---

type ProblemStats struct {
	ProblemID int `json:"problem_id"`
	Total     int `json:"total"`
	AC        int `json:"ac"`
	WA        int `json:"wa"`
	CE        int `json:"ce"`
	RE        int `json:"re"`
	TLE       int `json:"tle"`
}

type UserStats struct {
	UserID int `json:"user_id"`
	Total  int `json:"total"`
	AC     int `json:"ac"`
	Solved int `json:"solved"`
}
