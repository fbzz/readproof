CREATE TABLE sources (
    source_id   TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('filesystem','github')),
    config_json TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE TABLE policies (
    policy_id          TEXT PRIMARY KEY,
    strategy           TEXT NOT NULL CHECK (strategy IN ('require_fresh','allow_stale','pinned')),
    max_age_seconds    INTEGER,
    pinned_snapshot_id TEXT,
    created_at         TEXT NOT NULL
);

CREATE TABLE resources (
    uri                 TEXT PRIMARY KEY,
    namespace           TEXT NOT NULL,
    path                TEXT NOT NULL,
    source_id           TEXT NOT NULL REFERENCES sources(source_id),
    policy_id           TEXT NOT NULL REFERENCES policies(policy_id),
    current_snapshot_id TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE INDEX idx_resources_namespace ON resources(namespace);

CREATE TABLE snapshots (
    snapshot_id     TEXT PRIMARY KEY,
    resource_uri    TEXT NOT NULL REFERENCES resources(uri),
    source_revision TEXT NOT NULL,
    content_hash    TEXT NOT NULL,
    observed_at     TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    bytes           INTEGER NOT NULL,
    provenance_json TEXT NOT NULL
);
CREATE INDEX idx_snapshots_resource_observed ON snapshots(resource_uri, observed_at DESC);
CREATE INDEX idx_snapshots_content_hash ON snapshots(content_hash);

CREATE TABLE materializations (
    materialization_id TEXT PRIMARY KEY,
    snapshot_id         TEXT NOT NULL REFERENCES snapshots(snapshot_id),
    strategy             TEXT NOT NULL DEFAULT 'raw' CHECK (strategy IN ('raw')),
    content_hash          TEXT NOT NULL,
    bytes                  INTEGER NOT NULL,
    created_at              TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_materializations_snapshot_strategy ON materializations(snapshot_id, strategy);

CREATE TABLE manifests (
    manifest_id TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL
);

CREATE TABLE manifest_entries (
    manifest_id         TEXT NOT NULL REFERENCES manifests(manifest_id),
    position             INTEGER NOT NULL,
    uri                    TEXT NOT NULL,
    snapshot_id             TEXT NOT NULL REFERENCES snapshots(snapshot_id),
    materialization_id       TEXT NOT NULL REFERENCES materializations(materialization_id),
    content_hash               TEXT NOT NULL,
    PRIMARY KEY (manifest_id, position)
);

CREATE TABLE runs (
    run_id       TEXT PRIMARY KEY,
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','committed')),
    created_at   TEXT NOT NULL,
    committed_at TEXT,
    manifest_id  TEXT REFERENCES manifests(manifest_id)
);

CREATE TABLE run_mounts (
    run_id              TEXT NOT NULL REFERENCES runs(run_id),
    position             INTEGER NOT NULL,
    uri                    TEXT NOT NULL,
    snapshot_id             TEXT NOT NULL REFERENCES snapshots(snapshot_id),
    materialization_id       TEXT NOT NULL REFERENCES materializations(materialization_id),
    content_hash               TEXT NOT NULL,
    PRIMARY KEY (run_id, position)
);
