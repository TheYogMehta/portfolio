-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS projects (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    github_url TEXT,
    live_url TEXT,
    tags TEXT,
    short_description TEXT,
    long_description TEXT,
    is_public BOOLEAN NOT NULL DEFAULT true,
    is_github_private BOOLEAN NOT NULL DEFAULT false,
    is_featured BOOLEAN NOT NULL DEFAULT false,
    thumbnail_url TEXT,
    stars_count INT NOT NULL DEFAULT 0,
    commits_count INT NOT NULL DEFAULT 0,
    views_count INT NOT NULL DEFAULT 0,
    last_commit_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS project_languages (
    project_id INT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(50) NOT NULL,
    percentage NUMERIC(5,2) NOT NULL CHECK (percentage >= 0 AND percentage <= 100),
    PRIMARY KEY (project_id, name)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_languages;
DROP TABLE IF EXISTS projects;
-- +goose StatementEnd