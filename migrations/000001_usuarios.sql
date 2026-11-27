-- Propuesta inicial. Inspeccionar la base destino antes de aplicar migraciones.
-- UUID y hashes se generan en la aplicación. No crea bases ni roles.

CREATE TABLE usuarios (
    id UUID PRIMARY KEY,
    nombre TEXT NOT NULL CHECK (char_length(btrim(nombre)) BETWEEN 1 AND 60),
    email TEXT NOT NULL UNIQUE,
    hash_contrasena TEXT NOT NULL CHECK (hash_contrasena <> ''),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    creado_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT usuarios_email_normalizado CHECK (
        email <> '' AND email = lower(btrim(email))
    ),
    CONSTRAINT usuarios_fechas CHECK (actualizado_en >= creado_en)
);

CREATE TABLE preferencias_usuario (
    usuario_id UUID PRIMARY KEY REFERENCES usuarios(id),
    moneda TEXT NOT NULL DEFAULT 'CLP' CHECK (moneda = 'CLP'),
    zona_horaria TEXT NOT NULL DEFAULT 'America/Santiago'
        CHECK (zona_horaria <> '' AND zona_horaria = btrim(zona_horaria)),
    monto_maximo_hormiga BIGINT NOT NULL DEFAULT 3000 CHECK (monto_maximo_hormiga > 0),
    frecuencia_minima INTEGER NOT NULL DEFAULT 3 CHECK (frecuencia_minima > 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sesiones (
    id UUID PRIMARY KEY,
    usuario_id UUID NOT NULL REFERENCES usuarios(id),
    hash_token BYTEA NOT NULL UNIQUE CHECK (octet_length(hash_token) = 32),
    creado_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ultima_actividad_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expira_en TIMESTAMPTZ NOT NULL,
    revocado_en TIMESTAMPTZ,
    CONSTRAINT sesiones_fechas CHECK (
        expira_en > creado_en
        AND ultima_actividad_en >= creado_en
        AND ultima_actividad_en <= expira_en
        AND (revocado_en IS NULL OR revocado_en >= creado_en)
    )
);

CREATE INDEX sesiones_usuario_vigentes_idx ON sesiones (usuario_id)
    WHERE revocado_en IS NULL;
CREATE INDEX sesiones_expiracion_idx ON sesiones (expira_en);

CREATE TABLE outbox_eventos (
    evento_id UUID PRIMARY KEY,
    usuario_id UUID NOT NULL REFERENCES usuarios(id),
    tipo TEXT NOT NULL CHECK (tipo <> ''),
    version_esquema INTEGER NOT NULL DEFAULT 1 CHECK (version_esquema > 0),
    ocurrido_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    agregado_tipo TEXT NOT NULL DEFAULT 'Usuario' CHECK (agregado_tipo = 'Usuario'),
    agregado_id UUID NOT NULL,
    correlacion_id UUID NOT NULL,
    datos JSONB NOT NULL CHECK (jsonb_typeof(datos) = 'object'),
    publicado_en TIMESTAMPTZ,
    intentos INTEGER NOT NULL DEFAULT 0 CHECK (intentos >= 0),
    proximo_intento_en TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT outbox_usuario_agregado CHECK (agregado_id = usuario_id),
    CONSTRAINT outbox_fechas CHECK (publicado_en IS NULL OR publicado_en >= ocurrido_en)
);

CREATE INDEX outbox_pendientes_idx ON outbox_eventos (proximo_intento_en, ocurrido_en)
    WHERE publicado_en IS NULL;

