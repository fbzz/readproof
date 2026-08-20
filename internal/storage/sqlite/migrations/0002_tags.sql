CREATE TABLE tags (
    resource_uri TEXT NOT NULL REFERENCES resources(uri),
    tag          TEXT NOT NULL,
    snapshot_id  TEXT NOT NULL REFERENCES snapshots(snapshot_id),
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (resource_uri, tag)
);
CREATE INDEX idx_tags_snapshot ON tags(snapshot_id);

ALTER TABLE manifest_entries ADD COLUMN ref TEXT NOT NULL DEFAULT '';

ALTER TABLE run_mounts ADD COLUMN ref TEXT NOT NULL DEFAULT '';
