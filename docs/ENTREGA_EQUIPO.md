# Entrega al equipo: Usuarios

Estado del 26 de noviembre de 2026. Esta guía describe lo implementado, sus límites
y cómo continuar. [Alcance acordado](ALCANCE_ACORDADO.md) conserva la decisión de
trabajo del proyecto universitario; [Autenticación](AUTENTICACION.md) detalla la política.

## Qué se avanzó

El servicio pasó de comprobar variables y terminar a atender ocho operaciones
gRPC persistentes. El flujo ya se integra con BFF y frontend: registro, login,
sesiones, logout, perfil y preferencias. El correo permanece fijo; editar perfil
modifica únicamente el nombre. Las preferencias reales son moneda CLP, zona IANA,
monto máximo y frecuencia mínima de gastos hormiga.

| Operación | Comportamiento |
| --- | --- |
| RegisterUser | Normaliza correo; crea usuario, preferencias y evento en una transacción. No inicia sesión. |
| Login | Verifica contraseña; crea sesión independiente y entrega token solo al BFF autorizado. |
| ValidateSession | Comprueba token, revocación, expiración e inactividad y actualiza actividad. |
| Logout | Revoca solo la sesión presentada; repetirlo con un token canónico es seguro. |
| GetProfile / UpdateProfile | Operan sobre el usuario de la sesión; la edición exige versión esperada. |
| GetPreferences / UpdatePreferences | Validan valores y versión; un cambio efectivo genera outbox, un no-op conserva versión. |

## Cómo se implementó

| Ubicación | Responsabilidad |
| --- | --- |
| [cmd/ms-usuarios](../cmd/ms-usuarios/main.go) | Inicio, conexión PostgreSQL, verificación de migraciones, gRPC, salud y cierre ordenado. |
| [internal/users/register.go](../internal/users/register.go) | Registro atómico y correo único, incluso con concurrencia. |
| [internal/users/sessions.go](../internal/users/sessions.go) | Login, sesiones, logout y límites persistidos. |
| [internal/users/profile.go](../internal/users/profile.go) | Perfil y preferencias con autorización y versiones. |
| [internal/security/password.go](../internal/security/password.go) | Argon2id con sal por contraseña y verificación. |
| [internal/rpc](../internal/rpc/server.go) | Adaptación Protobuf, errores gRPC y autorización por certificado. |
| [migrations](../migrations/README.md) | SQL, versiones, checksum y bloqueo de ejecución concurrente. |
| [compose.dev.yaml](../compose.dev.yaml) | PostgreSQL, Usuarios y composición con BFF/frontend hermanos. |

El contrato fuente es
[usuarios.proto](../proto/billetera/usuarios/v1/usuarios.proto); el BFF mantiene una
copia idéntica. Coordinar todo cambio de contrato y regenerar sus stubs.

TLS mutuo autoriza la URI spiffe://billetera/bff-graphql para negocio. Una identidad
separada solo consulta Health/Check. Las operaciones propias reciben metadatos
authorization: Bearer <token>; no aceptan userId como identidad elegida por el cliente.
Las claves y el .env no se comparten por Git.

## Datos y decisiones actuales

Tablas: usuarios, preferencias_usuario, sesiones, outbox_eventos, limites_login y
schema_migrations. Solo hay referencias dentro de la base propia. El ejecutor
migra explícitamente; el servidor no crea ni altera tablas al arrancar.

Las contraseñas admiten 15 a 128 caracteres Unicode y usan Argon2id. Cada sesión
emite 32 bytes aleatorios y almacena su SHA-256, no el token original. Por defecto:
SESSION_TTL=24h y SESSION_IDLE_TIMEOUT=30m. Hay hasta cinco fallos por correo en
quince minutos y enfriamiento de quince minutos, compartido en PostgreSQL.

Preferencias iniciales: CLP, America/Santiago, 3000 CLP y frecuencia 3, siguiendo
RF-08. La demo financiera original usa un criterio ampliado de 5000 CLP; permanece
separada de los datos reales y esa diferencia debe coordinarse con Transacciones.
Importes gRPC int64 y versiones deben leerse sin pérdida de precisión en otros lenguajes.

## Ejecutar y comprobar

La guía reproducible de los tres repositorios se versiona en el BFF:
[INTEGRACION_LOCAL.md](../../billetera-bff-graphql/docs/INTEGRACION_LOCAL.md).
Requiere clones hermanos con esos nombres. Para Usuarios, los comandos relevantes son:

```powershell
./scripts/dev.ps1 -Action migrate
./scripts/dev.ps1 -Action check
./scripts/dev.ps1 -Action test-docker
./scripts/dev.ps1 -Action grpc-check-docker
```

test-docker requiere la base separada billetera_usuarios_test y crea esquemas
temporales, que limpia al finalizar. No limpia las cuentas de desarrollo. Se
verificaron registro concurrente, rollback, límites, expiración, sesiones
independientes, aislamiento y conflictos de perfil/preferencias con PostgreSQL y
gRPC reales. Cobertura local registrada: 83,9 % en internal y migrations; excluye
comandos y código generado. CI exige 60 %; su ejecución remota no fue comprobada.

## Qué falta y quién debe coordinarlo

- **Usuarios + responsables de Kafka:** implementar el publicador de outbox,
  reintentos y confirmación; acordar tópicos/esquemas con sus consumidores.
  Hoy se guardan eventos, pero no se envían a Kafka.
- **Usuarios + Transacciones/Agente:** definir consultas mínimas y permisos para
  lectores internos. Sus identidades aún no están autorizadas por el servidor.
- **Usuarios + BFF + Agente:** consentimiento IA pendiente para integrar el LLM;
  persistir decisión, versión, fecha y revocación antes de utilizarlo.
- **Futuro, fuera de esta entrega:** recuperación/cambio de contraseña,
  verificación/cambio de correo, eliminación distribuida, MFA y ampliaciones de
  preferencias. No son condiciones añadidas para cerrar el alcance acordado.

Los informes históricos en ENTORNO.md conservan los pasos anteriores; para conocer
el estado actual, usar esta guía y los README de los tres repositorios.
