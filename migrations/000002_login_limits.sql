-- Shared login counters survive restarts and coordinate multiple service replicas.
CREATE TABLE limites_login (
    correo_hash BYTEA PRIMARY KEY CHECK (octet_length(correo_hash)=32),
    inicio_ventana TIMESTAMPTZ NOT NULL,
    fallos INTEGER NOT NULL DEFAULT 0 CHECK (fallos >= 0),
    bloqueado_hasta TIMESTAMPTZ
);
