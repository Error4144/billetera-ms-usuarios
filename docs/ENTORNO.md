# Comprobación del entorno

Este archivo es un registro histórico de verificaciones, incluidos estados ya
superados. El estado vigente está en [Entrega al equipo](ENTREGA_EQUIPO.md) y la
[guía conjunta versionada en BFF](../../billetera-bff-graphql/docs/INTEGRACION_LOCAL.md).

Revisión inicial del 26 de noviembre de 2026 en el workspace Arquitectura2.

| Componente | Resultado observado |
| --- | --- |
| Go | No encontrado en PATH ni en la ubicación estándar revisada |
| protoc | No encontrado en PATH; código del contrato aún sin generar ni compilar |
| Cliente PostgreSQL | psql 18.1 disponible |
| PostgreSQL de Windows | Servicio postgresql-x64-18 activo; 127.0.0.1:5432 rechazó la conexión |
| Docker Desktop | Cliente instalado; contexto desktop-linux no dispone del canal del motor |
| Bases del proyecto en Docker | No se pudieron inspeccionar contenedores, volúmenes ni tablas |

La guía compartida registra bases creadas y probadas por el compañero en otra
sesión. Estos resultados no demuestran su ausencia ni justifican recrear o borrar
volúmenes. Tampoco se intentó adivinar credenciales ni modificar PostgreSQL de Windows.

## Validación del SQL

Se creó un clúster desechable PostgreSQL 18 en una carpeta temporal, separado de
las bases del proyecto y escuchando solo en 127.0.0.1:55432. Allí se ejecutaron
000001_usuarios.sql y tests/schema.sql con ON_ERROR_STOP=1. Ambos terminaron con
código 0. El servidor temporal se detuvo al finalizar.

Resultado: cuatro tablas creadas; verificación de defaults, correo único y
normalizado, relación de preferencias, moneda y rangos, BIGINT exacto por encima
de Number seguro, hashes de token únicos y de 32 bytes, fechas, outbox y rollback
de un registro parcial. Las inserciones de prueba se revirtieron y quedaron cero
usuarios. Son pruebas de esquema; no verifican la aplicación, concurrencia entre
procesos ni autenticación. No se probó PostgreSQL 17 de Compose.

## Siguiente preparación

1. Preparar Go conforme a go.mod y protoc con plugins Go fijados. Verificar la
   disponibilidad de las versiones antes de instalar o cambiar dependencias.
2. Arrancar Docker Desktop y consultar sus contenedores y volúmenes existentes.
3. Inspeccionar billetera_usuarios y coordinar credenciales sin publicarlas.
4. Implementar el ejecutor de migraciones con registro de versiones y bloqueo.
5. Implementar registro transaccional, hashing y sesiones con las políticas de
   AUTENTICACION.md; después handlers gRPC, outbox y pruebas de aplicación.

No se instalaron herramientas globales ni se cambiaron contratos de otros servicios.

## Actualización: Docker disponible y registro implementado

Al repetir la comprobación, el motor desktop-linux respondió. No se encontraron
contenedores o volúmenes con los nombres de billetera-local; los recursos de otros
proyectos se conservaron. Se preparó el entorno independiente compose.dev.yaml de
Usuarios: PostgreSQL 17.12, puerto 127.0.0.1:55431 y volumen
billetera-usuarios-dev_usuarios-data. Su contraseña aleatoria está en .env,
excluido de Git y del contexto Docker. El contenedor queda disponible para desarrollo.

La base billetera_usuarios estaba vacía antes de migrar. Ahora contiene usuarios,
preferencias_usuario, sesiones, outbox_eventos y schema_migrations, con la versión
000001_usuarios.sql aplicada. No se crearon usuarios de demostración. La base
billetera_usuarios_test separa las pruebas de los datos de desarrollo.

Go 1.27.4 y protoc 36.2 se descargaron como herramientas portables en ../.tools,
verificando SHA-256 contra metadatos oficiales de Go y de la publicación GitHub
de Protocol Buffers. Se instalaron allí los generadores protoc-gen-go 1.36.12 y
protoc-gen-go-grpc 1.5.1, y se generaron los stubs Go del contrato.

Se agregaron migraciones con bloqueo y checksum, registro transaccional y Argon2id.
go vet, go build y pruebas unitarias y de integración con PostgreSQL 17 pasaron.
La cobertura combinada sobre internal y migrations es 86,6 % de sentencias;
excluye comandos y código generado. Se comprobaron registros concurrentes,
rollback por fallo de outbox, checksum y migraciones concurrentes. Los tests
limpiaron sus esquemas temporales y dejaron cero usuarios de desarrollo.

La imagen billetera-ms-usuarios:local se construyó y ejecutó correctamente como
verificador de configuración. También se ejecutó /migrate desde esa imagen contra
PostgreSQL de desarrollo: reconoció la versión aplicada sin volver a crear tablas.

Quedan pendientes handlers gRPC, sesiones, autorización entre servicios, límites
de autenticación y publicador Kafka. ms-usuarios continúa siendo el verificador de
configuración; el nuevo comando migrate sí conecta a la base y aplica el esquema.

## Actualización: servidor gRPC activo

Se conectó RegisterUser al contrato y se levantó ms-usuarios en Docker, publicado
solo en 127.0.0.1:50051. El contenedor y PostgreSQL están saludables. TLS 1.3 mutuo
verifica CA y certificado de cliente; la URI SAN del BFF autoriza negocio y la
identidad de salud solo puede usar Health/Check. Las claves locales están excluidas
de Git y del contexto de build. El generador no conserva la clave privada de la CA.

Se probaron con éxito en Linux Docker: registro por TCP/gRPC con PostgreSQL real,
normalización, duplicado, validación, traducción de errores, rechazo de clientes
sin certificado o con identidad ajena, permisos restringidos de salud, caída de
base simulada y recuperación de panic sin filtrar secretos. También pasaron go vet,
build y la comprobación del contenedor real desde otro contenedor con identidad
del BFF. Cobertura de internal y migrations: 86,7 % de sentencias, excluyendo
comandos y código generado. Los registros de prueba se revirtieron al limpiar sus
esquemas aislados; la comprobación del contenedor no crea usuarios de desarrollo.

En Windows, los handshakes de pruebas locales se interrumpen y el cliente al puerto
publicado recibe un certificado con SHA-256 distinto al de .local-certs/server.crt,
por lo que devuelve unknown authority. No se modificó la confianza del sistema ni
se desactivó verificación. Las pruebas y el cliente entre contenedores pasan. El
origen de esa diferencia en Windows no se ha determinado. Usar test-docker y
grpc-check-docker en este entorno.

Pendiente: Login, ValidateSession, Logout, perfil y preferencias siguen devolviendo
UNIMPLEMENTED a llamantes BFF autorizados. Faltan sesiones, límites de intentos y
publicador Kafka. El BFF real todavía debe integrar el contrato y sus certificados.

## Actualización: identidad integrada de extremo a extremo

Se implementaron Login, ValidateSession, Logout, GetProfile, UpdateProfile,
GetPreferences y UpdatePreferences. Los tokens se verifican por hash y la fila
de sesión se bloquea durante las operaciones propias. Expiración e inactividad son
configurables. 000002_login_limits.sql persiste límites de acceso entre instancias.

BFF NestJS/Apollo ya consume esos RPC con certificados, cookies HttpOnly y control
de origen. El modo integration del frontend ofrece registro, login, perfil,
preferencias y logout reales. Nginx envía /graphql al BFF. Los cuatro contenedores
están saludables y la entrada es http://127.0.0.1:8080. La demo financiera sigue
separada, sin mezclar sus registros con cuentas autenticadas.

Pasaron pruebas Go con PostgreSQL/gRPC de expiración, límites, aislamiento,
sesiones independientes, conflictos y rollback de eventos. Cobertura actual:
83,9 % en internal y migrations. BFF pasó cuatro pruebas específicas y frontend
sus 37 unitarias, ESLint y build. La prueba HTTP verificó CLP exactos, CSRF y
revocación; Edge comprobó formularios, recarga, cookie HttpOnly, móvil y logout.
Los fixtures de esas pruebas se limpiaron. IA y publicación Kafka siguen pendientes.
Consultar ../../docs/AVANCE_USUARIOS.md para reproducir el entorno completo.
