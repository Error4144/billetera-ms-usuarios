-- Ejecutar solo en una base desechable con 000001_usuarios.sql aplicada.
-- Las inserciones de prueba se revierten. No prueba la aplicación ni autenticación.
BEGIN;

DO $$
DECLARE
    propietario UUID := '11111111-1111-4111-8111-111111111111';
    otro UUID := '22222222-2222-4222-8222-222222222222';
    ausente UUID := '33333333-3333-4333-8333-333333333333';
    sesion UUID := '44444444-4444-4444-8444-444444444444';
    evento UUID := '55555555-5555-4555-8555-555555555555';
    correlacion UUID := '66666666-6666-4666-8666-666666666666';
BEGIN
    INSERT INTO usuarios(id, nombre, email, hash_contrasena)
        VALUES (propietario, 'Prueba', 'schema@example.test', 'hash-ficticio-solo-esquema');
    INSERT INTO preferencias_usuario(usuario_id) VALUES (propietario);

    IF NOT EXISTS (SELECT 1 FROM preferencias_usuario WHERE usuario_id = propietario
        AND moneda = 'CLP' AND zona_horaria = 'America/Santiago'
        AND monto_maximo_hormiga = 3000 AND frecuencia_minima = 3 AND version = 1) THEN
        RAISE EXCEPTION 'Preferencias iniciales incorrectas';
    END IF;

    BEGIN
        INSERT INTO usuarios(id, nombre, email, hash_contrasena)
            VALUES (otro, 'Otro', 'schema@example.test', 'ficticio');
        RAISE EXCEPTION 'Se aceptó un correo duplicado';
    EXCEPTION WHEN unique_violation THEN NULL;
    END;

    BEGIN
        INSERT INTO usuarios(id, nombre, email, hash_contrasena)
            VALUES (otro, 'Otro', ' Schema@Example.test ', 'ficticio');
        RAISE EXCEPTION 'Se aceptó correo sin normalizar';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    BEGIN
        INSERT INTO preferencias_usuario(usuario_id) VALUES (propietario);
        RAISE EXCEPTION 'Se aceptaron dos preferencias por usuario';
    EXCEPTION WHEN unique_violation THEN NULL;
    END;

    BEGIN
        INSERT INTO preferencias_usuario(usuario_id) VALUES (ausente);
        RAISE EXCEPTION 'Se aceptaron preferencias sin usuario';
    EXCEPTION WHEN foreign_key_violation THEN NULL;
    END;

    BEGIN
        UPDATE preferencias_usuario SET moneda = 'USD' WHERE usuario_id = propietario;
        RAISE EXCEPTION 'Se aceptó moneda ajena a CLP';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    BEGIN
        UPDATE preferencias_usuario SET monto_maximo_hormiga = 0 WHERE usuario_id = propietario;
        RAISE EXCEPTION 'Se aceptó umbral no positivo';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    BEGIN
        UPDATE preferencias_usuario SET frecuencia_minima = 0 WHERE usuario_id = propietario;
        RAISE EXCEPTION 'Se aceptó frecuencia no positiva';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    -- RN-01: preservar valores superiores al rango seguro de Number en JavaScript.
    UPDATE preferencias_usuario SET monto_maximo_hormiga = 9007199254740993
        WHERE usuario_id = propietario;
    IF (SELECT monto_maximo_hormiga FROM preferencias_usuario WHERE usuario_id = propietario)
        <> 9007199254740993 THEN
        RAISE EXCEPTION 'Se perdió precisión en BIGINT';
    END IF;

    INSERT INTO sesiones(id, usuario_id, hash_token, expira_en)
        VALUES (sesion, propietario, decode(repeat('ab', 32), 'hex'), CURRENT_TIMESTAMP + INTERVAL '24 hours');
    BEGIN
        INSERT INTO sesiones(id, usuario_id, hash_token, expira_en)
            VALUES (otro, propietario, decode(repeat('ab', 32), 'hex'), CURRENT_TIMESTAMP + INTERVAL '24 hours');
        RAISE EXCEPTION 'Se aceptó hash de token duplicado';
    EXCEPTION WHEN unique_violation THEN NULL;
    END;
    BEGIN
        INSERT INTO sesiones(id, usuario_id, hash_token, expira_en)
            VALUES (otro, propietario, decode('ab', 'hex'), CURRENT_TIMESTAMP + INTERVAL '24 hours');
        RAISE EXCEPTION 'Se aceptó hash de longitud incorrecta';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    BEGIN
        UPDATE sesiones SET expira_en = creado_en WHERE id = sesion;
        RAISE EXCEPTION 'Se aceptó expiración sin duración';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    INSERT INTO outbox_eventos(evento_id, usuario_id, tipo, agregado_id, correlacion_id, datos)
        VALUES (evento, propietario, 'UsuarioRegistrado', propietario, correlacion, '{}');
    BEGIN
        UPDATE outbox_eventos SET agregado_id = otro WHERE evento_id = evento;
        RAISE EXCEPTION 'Se aceptó agregado ajeno en outbox de Usuario';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    BEGIN
        UPDATE outbox_eventos SET datos = '[]' WHERE evento_id = evento;
        RAISE EXCEPTION 'Se aceptó carga de evento distinta de objeto';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    -- Un fallo dentro de la transacción de registro no debe dejar un usuario parcial.
    BEGIN
        INSERT INTO usuarios(id, nombre, email, hash_contrasena)
            VALUES (otro, 'Rollback', 'rollback@example.test', 'ficticio');
        INSERT INTO preferencias_usuario(usuario_id, moneda) VALUES (otro, 'USD');
        RAISE EXCEPTION 'Se aceptó registro parcial inválido';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    IF EXISTS (SELECT 1 FROM usuarios WHERE id = otro) THEN
        RAISE EXCEPTION 'El subbloque dejó el usuario después del fallo';
    END IF;

    RAISE NOTICE 'Esquema verificado: defaults, unicidad, referencias, rangos, precisión, sesiones, outbox y rollback.';
END $$;

ROLLBACK;
