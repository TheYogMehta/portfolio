-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS github_stats (
    id INT PRIMARY KEY DEFAULT 1,
    commits VARCHAR(50) NOT NULL,
    last_updated VARCHAR(50) NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS github_stats;
-- +goose StatementEnd
