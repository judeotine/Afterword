CREATE TABLE healthchecks (
    id integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO healthchecks (at) VALUES (now());
