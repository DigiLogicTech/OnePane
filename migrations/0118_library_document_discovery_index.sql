-- Parser-first navigation must not scan retained v1 histories before v2 rows.
CREATE INDEX library_document_indexes_by_parser_version
ON project_library_document_indexes(asset_id,parser_id,version);
