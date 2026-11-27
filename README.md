# Microservicio de Usuarios

- [Entrega al equipo: cambios, implementación y pendientes](docs/ENTREGA_EQUIPO.md).
- [Guía conjunta versionada en BFF](../billetera-bff-graphql/docs/INTEGRACION_LOCAL.md).

Servicio de identidad y preferencias de Billetera Virtual, implementado con Go,
gRPC y PostgreSQL propio. Publicará eventos en Kafka para los consumidores
definidos por la topología del proyecto.

## Estado actual

ms-usuarios expone registro, login, validación de sesión, logout, perfil y
preferencias por gRPC con TLS mutuo. Registro y cambios de preferencias guardan
su evento pendiente en la misma transacción. Contraseñas usan Argon2id; sesiones
persisten únicamente el hash del token y vencen por duración e inactividad.
La identidad del BFF se valida desde su certificado. El BFF y el frontend ya
integran este alcance. Publicación Kafka y consentimiento de IA siguen pendientes.

La primera entrega se describe en [Primera entrega](docs/PRIMERA_ENTREGA.md).
El contrato de esta entrega está en [usuarios.proto](proto/billetera/usuarios/v1/usuarios.proto).
La política implementada está en [Autenticación](docs/AUTENTICACION.md) y el esquema
inicial en [Migraciones](migrations/README.md).
Los cambios futuros del contrato deben coordinarse con el equipo. El
ejecutor de migraciones y los ocho RPC del contrato están implementados.
El [alcance acordado](docs/ALCANCE_ACORDADO.md) separa entrega y mejoras futuras.
Las comprobaciones y límites del entorno están en [Entorno](docs/ENTORNO.md).

## Desarrollo local de Usuarios

`compose.dev.yaml` levanta PostgreSQL, Usuarios, BFF y frontend. PostgreSQL usa un volumen propio
y puerto local 55431; gRPC se publica solo en 127.0.0.1:50051. Requiere un `.env` privado con POSTGRES_HOST=127.0.0.1,
POSTGRES_PORT=55431, POSTGRES_DB=billetera_usuarios, POSTGRES_USER y una contraseña
propia. Para este entorno local POSTGRES_SSLMODE=disable; no usarlo en despliegues
compartidos. Conservar las credenciales si ya se inicializó el volumen.

```powershell
docker --context desktop-linux compose -f compose.dev.yaml up -d --wait postgres
./scripts/dev.ps1 -Action migrate
./scripts/dev.ps1 -Action certs
docker --context desktop-linux compose -f compose.dev.yaml up -d --build --wait
./scripts/dev.ps1 -Action check
./scripts/dev.ps1 -Action test-docker
./scripts/dev.ps1 -Action grpc-check-docker
```

Las pruebas de integración requieren la base separada billetera_usuarios_test,
creada una vez con el mismo rol de desarrollo:

```powershell
docker --context desktop-linux compose -f compose.dev.yaml exec -T postgres createdb -U app_usuarios billetera_usuarios_test
```

Usar el rol real del `.env` si difiere de app_usuarios. Las pruebas crean y
eliminan sus propios esquemas aleatorios, y rechazan una base cuyo nombre no sea
billetera_usuarios_test. Ningún test limpia las tablas de desarrollo.
Crear esa base antes de ejecutar test-docker por primera vez. Si los certificados
ya existen, omitir certs: el generador no los reemplaza. Son certificados locales
de siete días, no material para producción. Para renovarlos, conservar el directorio
anterior fuera de Git, generar otro y coordinar el cambio de CA con los clientes.

El script reconoce las herramientas portables del workspace en ../.tools y carga
el `.env` sin imprimir credenciales. Go 1.27.4, protoc 36.2, protoc-gen-go 1.36.12
y protoc-gen-go-grpc 1.5.1 se usaron para generar los archivos de gen/. Para
regenerarlos: `./scripts/dev.ps1 -Action generate`.

La cobertura se mide sobre internal y migrations; el código generado se excluye.
CI exige 60 % e incluye PostgreSQL real. Fuera del script, `go test ./...` omite
las pruebas de base si USUARIOS_TEST_DATABASE_URL no está definida.

La interfaz integrada se abre en http://127.0.0.1:8080. Las instrucciones y pruebas
de extremo a extremo están en la [guía del BFF](../billetera-bff-graphql/docs/INTEGRACION_LOCAL.md). SESSION_TTL (24h) y
SESSION_IDLE_TIMEOUT (30m) configuran la política de sesión. Login incorrecto se
limita mediante limites_login, compartido en PostgreSQL. La segunda migración añade
esa tabla; schema_migrations conserva ambas versiones. Las operaciones propias
leen authorization: Bearer <token> desde metadatos y no aceptan userId como autoridad.

## Endpoint gRPC

Contrato: proto/billetera/usuarios/v1/usuarios.proto, servicio
billetera.usuarios.v1.UsersService. RegisterUser recibe email, password y
display_name; devuelve profile y preferences sin hashes ni tokens. Entrada inválida
devuelve INVALID_ARGUMENT; correo duplicado ALREADY_EXISTS; fallos internos no
exponen SQL ni datos privados. Se limitan los mensajes a 16 KiB y se admiten hasta
cuatro registros o verificaciones de contraseña simultáneos por proceso. El límite
de fallos de login por correo está implementado en PostgreSQL; un límite adicional
por origen de red en BFF queda como mejora futura.

El cliente debe presentar un certificado de la CA configurada con URI SAN
spiffe://billetera/bff-graphql. Solo el certificado local del BFF sirve para los
RPC de negocio. La identidad spiffe://billetera/ms-usuarios-health puede consultar
únicamente grpc.health.v1.Health/Check, que responde NOT_SERVING si PostgreSQL
no está disponible. Se verifica migración y checksum al arrancar; no se migra
automáticamente. SIGTERM/Ctrl+C permite hasta cinco segundos para cerrar solicitudes.

La imagen monta solo la CA, certificado/clave de servidor y certificado/clave de
salud. Las claves del BFF no se montan dentro del servidor. TLS es obligatorio;
no hay alternativa plaintext. Ver [autenticación gRPC](https://grpc.io/docs/guides/auth/)
y [health checking](https://grpc.io/docs/guides/health-checking/) para los protocolos.

El modo `ms-usuarios --check-config` conserva la comprobación rápida de variables.
`--healthcheck` usa el certificado de salud configurado en GRPC_HEALTH_CERT_FILE y
GRPC_HEALTH_KEY_FILE; TLS usa GRPC_TLS_CERT_FILE, GRPC_TLS_KEY_FILE y GRPC_TLS_CA_FILE.

En esta máquina, Windows recibe un certificado distinto al configurado en el
servidor y falla la confianza TLS. Las pruebas en Linux Docker y las llamadas entre
contenedores pasan con validación completa. Por eso los comandos recomendados son
test-docker y grpc-check-docker. No se desactiva TLS ni la verificación de certificados
para sortear ese comportamiento local. El origen del certificado intermedio queda
pendiente de diagnóstico del entorno Windows.

## Contexto compartido

Esta entrega se entiende desde [ENTREGA_EQUIPO](docs/ENTREGA_EQUIPO.md) y la guía
versionada en BFF. La documentación original del monolito es referencia histórica;
en el workspace actual se conserva en ../docs/, pero no es un requisito oculto
para ejecutar lo entregado. Sus documentos de referencia son:

- `DOMAIN_MODEL.md`: Usuario y Preferencias.
- `BUSINESS_RULES.md`: RN-01 y RN-02.
- `FUNCTIONAL_REQUIREMENTS.md`: RF-01 y RT-01 a RT-04.
- `PREFERENCES_OWNERSHIP.md`: perfil, preferencias y consentimiento.
- `DOMAIN_EVENTS.md`: publicación transaccional y catálogo propuesto.
- `IMPLEMENTATION_PLAN.md`: secuencia y criterios de aceptación.

## Comprobaciones del servicio

Con la versión de Go indicada en `go.mod`:

```powershell
go mod download
go vet ./...
go build ./...
go test ./...
```

go test omite las pruebas de PostgreSQL si USUARIOS_TEST_DATABASE_URL no está
definida. Para la verificación completa usar test-docker con la base aislada
indicada más arriba. El .env.example documenta variables; config.Load lee el
entorno del proceso, mientras scripts/dev.ps1 carga el .env local.
