-- Derived text indexes retain exact immutable source-version identity. They
-- confer no read grant and cannot become Task/Research execution evidence.
CREATE TABLE project_library_text_indexes (
 asset_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>=1),
 parser_version INTEGER NOT NULL CHECK(parser_version=1),
 content_hash TEXT NOT NULL CHECK(length(content_hash)=71),
 size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
 status TEXT NOT NULL CHECK(status IN ('ready','unsupported','too_large','invalid_text')),
 line_count INTEGER NOT NULL CHECK(line_count BETWEEN 0 AND 16384),
 created_by TEXT NOT NULL REFERENCES principals(id),
 created_at INTEGER NOT NULL CHECK(created_at>0),
 PRIMARY KEY(asset_id,version,parser_version),
 FOREIGN KEY(asset_id,version) REFERENCES project_library_asset_versions(asset_id,version),
 CHECK(status='ready' OR line_count=0)
) STRICT;
CREATE TABLE project_library_text_lines (
 asset_id TEXT NOT NULL,
 version INTEGER NOT NULL,
 parser_version INTEGER NOT NULL CHECK(parser_version=1),
 line_number INTEGER NOT NULL CHECK(line_number BETWEEN 1 AND 16384),
 byte_start INTEGER NOT NULL CHECK(byte_start>=0),
 byte_end INTEGER NOT NULL CHECK(byte_end>=byte_start AND byte_end-byte_start<=16384),
 content TEXT NOT NULL CHECK(length(CAST(content AS BLOB))<=16384),
 line_hash TEXT NOT NULL CHECK(length(line_hash)=71),
 PRIMARY KEY(asset_id,version,parser_version,line_number),
 FOREIGN KEY(asset_id,version,parser_version) REFERENCES project_library_text_indexes(asset_id,version,parser_version)
) STRICT;
CREATE TRIGGER library_text_index_immutable BEFORE UPDATE ON project_library_text_indexes
BEGIN SELECT RAISE(ABORT,'Library text indexes are immutable'); END;
CREATE TRIGGER library_text_index_retained BEFORE DELETE ON project_library_text_indexes
BEGIN SELECT RAISE(ABORT,'Library text indexes must be retained'); END;
CREATE TRIGGER library_text_line_immutable BEFORE UPDATE ON project_library_text_lines
BEGIN SELECT RAISE(ABORT,'Library text lines are immutable'); END;
CREATE TRIGGER library_text_line_retained BEFORE DELETE ON project_library_text_lines
BEGIN SELECT RAISE(ABORT,'Library text lines must be retained'); END;
