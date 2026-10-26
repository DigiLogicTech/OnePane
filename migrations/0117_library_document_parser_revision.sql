-- Preserve every v1 receipt/unit; v2 accepts XML's leading UTF-8 signature.
-- Copy before dropping the old child/parent. The migration transaction and
-- normal pre-upgrade backup retain atomic recovery without disabling FKs.
CREATE TABLE library_document_indexes_upgrade (
 asset_id TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>=1),
 parser_id TEXT NOT NULL CHECK(parser_id IN ('docx-wordml-paragraphs-v1','docx-wordml-paragraphs-v2')),
 raw_source_sha TEXT NOT NULL CHECK(length(raw_source_sha)=71),
 raw_source_size INTEGER NOT NULL CHECK(raw_source_size>=0),
 derived_sha TEXT NOT NULL, manifest_sha TEXT NOT NULL CHECK(length(manifest_sha)=71), derived_utf8_bytes INTEGER NOT NULL CHECK(derived_utf8_bytes BETWEEN 0 AND 1048576),
 status TEXT NOT NULL CHECK(status IN ('ready','unsupported','too_large','invalid_document')),
 coverage TEXT NOT NULL, parts_json TEXT NOT NULL CHECK(length(CAST(parts_json AS BLOB))<=32768),
 unit_count INTEGER NOT NULL CHECK(unit_count BETWEEN 0 AND 8192),
 created_by TEXT NOT NULL REFERENCES principals(id), created_at INTEGER NOT NULL CHECK(created_at>0),
 PRIMARY KEY(asset_id,version,parser_id),
 FOREIGN KEY(asset_id,version) REFERENCES project_library_asset_versions(asset_id,version),
 CHECK(status='ready' OR (unit_count=0 AND derived_utf8_bytes=0 AND derived_sha='' AND coverage='' AND parts_json='[]'))
) STRICT;
CREATE TABLE library_document_units_upgrade (
 asset_id TEXT NOT NULL,version INTEGER NOT NULL,parser_id TEXT NOT NULL CHECK(parser_id IN ('docx-wordml-paragraphs-v1','docx-wordml-paragraphs-v2')),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 8192),
 part_name TEXT NOT NULL CHECK(length(CAST(part_name AS BLOB))<=256),
 paragraph_number INTEGER NOT NULL CHECK(paragraph_number BETWEEN 1 AND 8192),
 content TEXT NOT NULL CHECK(length(CAST(content AS BLOB))<=16384),
 text_hash TEXT NOT NULL CHECK(length(text_hash)=71),
 PRIMARY KEY(asset_id,version,parser_id,ordinal),
 FOREIGN KEY(asset_id,version,parser_id) REFERENCES library_document_indexes_upgrade(asset_id,version,parser_id)
) STRICT;
INSERT INTO library_document_indexes_upgrade SELECT * FROM project_library_document_indexes;
INSERT INTO library_document_units_upgrade SELECT * FROM project_library_document_units;
DROP TABLE project_library_document_units;
DROP TABLE project_library_document_indexes;
ALTER TABLE library_document_indexes_upgrade RENAME TO project_library_document_indexes;
ALTER TABLE library_document_units_upgrade RENAME TO project_library_document_units;
CREATE TRIGGER library_document_index_immutable BEFORE UPDATE ON project_library_document_indexes BEGIN SELECT RAISE(ABORT,'Library document indexes are immutable'); END;
CREATE TRIGGER library_document_index_retained BEFORE DELETE ON project_library_document_indexes BEGIN SELECT RAISE(ABORT,'Library document indexes must be retained'); END;
CREATE TRIGGER library_document_unit_immutable BEFORE UPDATE ON project_library_document_units BEGIN SELECT RAISE(ABORT,'Library document units are immutable'); END;
CREATE TRIGGER library_document_unit_retained BEFORE DELETE ON project_library_document_units BEGIN SELECT RAISE(ABORT,'Library document units must be retained'); END;
