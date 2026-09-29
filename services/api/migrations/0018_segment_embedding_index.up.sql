CREATE INDEX transcript_segments_embedding_idx ON transcript_segments USING hnsw (embedding vector_cosine_ops);
