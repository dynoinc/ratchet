DROP INDEX messages_v2_to_tsvector_idx;

ALTER TABLE
    messages_v2
ADD
    COLUMN text_search tsvector
    GENERATED ALWAYS AS (to_tsvector('english', attrs -> 'message' ->> 'text')) STORED;;

CREATE INDEX ON messages_v2 USING GIN (text_search);