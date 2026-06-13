CREATE TABLE IF NOT EXISTS users (
    id         SERIAL PRIMARY KEY,
    username   VARCHAR(64) UNIQUE NOT NULL,
    password   VARCHAR(255) NOT NULL,
    role       VARCHAR(10)  NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS problems (
    id           SERIAL PRIMARY KEY,
    title        VARCHAR(255) NOT NULL,
    description  TEXT         NOT NULL DEFAULT '',
    time_limit   INT          NOT NULL DEFAULT 5,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS testcases (
    id         SERIAL PRIMARY KEY,
    problem_id INT  NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    input      TEXT NOT NULL DEFAULT '',
    expected   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS submissions (
    id          SERIAL       PRIMARY KEY,
    operator_id UUID         UNIQUE NOT NULL,
    user_id     INT          NOT NULL REFERENCES users(id),
    problem_id  INT          NOT NULL REFERENCES problems(id),
    status      VARCHAR(20)  NOT NULL DEFAULT 'pending',
    source_path VARCHAR(512) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS submission_logs (
    submission_id INT  PRIMARY KEY REFERENCES submissions(id) ON DELETE CASCADE,
    configure_log TEXT NOT NULL DEFAULT '',
    compile_log   TEXT NOT NULL DEFAULT '',
    output_log    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_submissions_user_id    ON submissions(user_id);
CREATE INDEX IF NOT EXISTS idx_submissions_problem_id ON submissions(problem_id);
CREATE INDEX IF NOT EXISTS idx_submissions_status     ON submissions(status);
CREATE INDEX IF NOT EXISTS idx_testcases_problem_id   ON testcases(problem_id);
