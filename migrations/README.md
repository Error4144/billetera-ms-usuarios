# Migraciones de Usuarios

000001_usuarios.sql propone usuarios, preferencias_usuario, sesiones y outbox_eventos.
No crea la base ni el rol. Inspeccionar tablas, datos y migraciones previas antes de
aplicarla sobre una base existente. No borrar volúmenes para adoptar este esquema.

000002_login_limits.sql agrega límites de intentos persistidos, compartidos entre
instancias. Los tokens permanecen en sesiones como hashes; no se añade una tabla
de consentimiento de IA en esta entrega. Las migraciones ya aplicadas no se editan.

Los defaults de preferencias siguen RF-08 y están pendientes de coordinación con
la demo. El SQL usa una transacción y falla ante tablas existentes para evitar
ocultar diferencias. cmd/migrate ejecuta las migraciones embebidas en una sola
transacción, registra versión y checksum en schema_migrations y toma un bloqueo
transaccional para serializar ejecuciones concurrentes. Repetir no reaplica una
versión exitosa; modificar su SQL después de aplicarlo causa un error. Los cambios
posteriores deben agregarse en otra migración. El punto de entrada ms-usuarios no
migra automáticamente. No incluye borrado automático ni rollback destructivo.

PostgreSQL impone unicidad, referencias locales, rangos y orden de fechas. La
aplicación validará formato de correo, zona IANA, contraseñas, autorización y
vigencia de sesiones. La relación permite como máximo una fila de preferencias
por usuario; la creación conjunta en transacción garantizará su existencia.

La comprobación inicial del SQL se ejecutó con éxito en PostgreSQL 18 temporal.
Consultar [Entorno](../docs/ENTORNO.md) para el alcance de esa comprobación.

Aplicación local: `./scripts/dev.ps1 -Action migrate`. Las pruebas Go de integración
comprueban PostgreSQL 17, concurrencia del ejecutor, checksum e incompatibilidad
con un esquema preexistente. tests/schema.sql sigue disponible para comprobaciones
manuales del esquema en una base desechable ya migrada.
