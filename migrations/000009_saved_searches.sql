CREATE TABLE saved_searches (
 id UUID PRIMARY KEY,
 owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
 conditions JSONB NOT NULL CHECK (jsonb_typeof(conditions) = 'object'),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX saved_searches_owner_idx ON saved_searches(owner_id, updated_at DESC, id);
