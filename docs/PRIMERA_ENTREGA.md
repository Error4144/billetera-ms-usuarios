# Primera entrega de Usuarios

Estado: diseño inicial con cierre de implementación al 26 de noviembre de 2026.

Para el estado actual y los pendientes usar [Entrega al equipo](ENTREGA_EQUIPO.md).
Las etapas y propuestas siguientes conservan el razonamiento del diseño; no son
una lista de trabajo todavía sin realizar. La guía conjunta se versiona en BFF.

Este documento convierte el contexto compartido en una entrega concreta. Distingue
las reglas documentadas de las decisiones que proponemos y que deben coordinarse
con los responsables del BFF y de los consumidores.

## Resultado esperado

Registrar un usuario y sus preferencias, iniciar sesión, consultar y actualizar
su perfil y preferencias, y cerrar la sesión de forma efectiva. Las operaciones
persisten en PostgreSQL y se atienden por gRPC. El frontend accede por GraphQL al
BFF; no accede directamente a Usuarios.

El registro no inicia sesión automáticamente. La integración propuesta requiere
un inicio de sesión explícito después del registro.

## Base definida por el proyecto

| Tema | Definición existente | Fuente compartida |
| --- | --- | --- |
| Tecnología | Go, gRPC, PostgreSQL propio y publicación Kafka | TECH_STACK.md y TOPOLOGY.md |
| Identidad | UUID, correo normalizado con trim y minúsculas; unicidad incluso con concurrencia | DOMAIN_MODEL.md y RN-02 |
| Credenciales | Hash de contraseña con sal, sin exposición en respuestas, eventos o logs | RN-02 y SECURITY_REQUIREMENTS.md |
| Preferencias | Una por usuario, creada junto con la identidad; moneda CLP, zona IANA y criterio de gastos hormiga | DOMAIN_MODEL.md y RF-01 |
| Aislamiento | Identidad obtenida del contexto autenticado; el servicio verifica el propietario | RN-02 y RT-01 |
| Eventos | Cambio y outbox guardados en una transacción; reenvío con el mismo eventoId | DOMAIN_EVENTS.md |
| Pruebas | Casos críticos y cobertura mínima de 60 % del código de aplicación | RT-04 |

## Decisiones propuestas para esta entrega

1. Usar sesiones opacas persistidas y revocables. Guardar únicamente el hash del
   token aleatorio; el token original se entrega solo al BFF al iniciar sesión.
   La expiración y los procedimientos de emisión y hashing se concretan como
   propuesta en AUTENTICACION.md.
2. El BFF almacenará la credencial de sesión en una cookie HttpOnly y Secure en
   HTTPS, con SameSite y protección CSRF propuestas en AUTENTICACION.md.
   No se expondrá el token como campo de una respuesta GraphQL. El BFF propagará
   el token a Usuarios en metadatos gRPC `authorization: Bearer <token>`.
3. Autenticar también al servicio llamante. La red interna por sí sola no autoriza
   solicitudes. Se propone TLS mutuo para identidad de servicio y transporte;
   coordinar certificados con BFF. Las operaciones de este primer contrato se destinan
   al BFF; los permisos de Transacciones y Agente se especifican por separado.
4. Incorporar nombre visible al perfil y una versión para evitar sobrescrituras
   de ediciones concurrentes. El correo permanece fijo en esta entrega.
5. Proponer inicialmente `CLP`, `America/Santiago`, máximo de 3000 CLP y frecuencia
   de 3, siguiendo RF-08 y MVP_SCOPE.md. El frontend demo usa 5000 CLP y un criterio
   ampliado: esta diferencia debe resolverse con el equipo. Los valores iniciales
   se asignan en el servidor y se devuelven en la respuesta del registro.
6. Reemplazar el bloque completo de preferencias con una versión esperada.
   Si los valores son idénticos y la versión coincide, devolver el estado actual
   sin aumentar versión ni generar otro evento.

La propuesta concreta de contraseña, sesiones, límites e identidad de servicio
está en [Autenticación](AUTENTICACION.md). Requiere coordinación con el BFF antes
de activar los endpoints.

## Datos y migraciones por implementar

No se ha comprobado el contenido de los volúmenes Docker existentes. Antes de
aplicar migraciones se debe inspeccionar la base destino y conservar sus datos.
No se requiere crear otra instancia si la preparada por Compose sigue disponible.

| Tabla propuesta | Datos principales y restricciones |
| --- | --- |
| usuarios | id UUID PK; nombre; email normalizado UNIQUE; hash_contrasena; version positiva; creado_en y actualizado_en UTC |
| preferencias_usuario | usuario_id PK y FK local a usuarios; moneda CLP; zona IANA validada por aplicación; monto_maximo_hormiga BIGINT positivo; frecuencia_minima positiva; version positiva; actualizado_en UTC |
| sesiones | id UUID PK; usuario_id FK; hash_token UNIQUE; creado_en; ultima_actividad_en; expira_en; revocado_en opcional; expiración posterior a creación |
| outbox_eventos | evento_id UUID PK; usuario_id; tipo; version_esquema; ocurrido_en; correlacion_id; datos JSONB; publicado_en opcional; intentos y próximo intento |

La versión inicial de perfil y preferencias será 1. Usuario, preferencias iniciales
y evento UsuarioRegistrado se guardan en una única transacción. No hay claves
foráneas hacia las bases de otros microservicios.

El consentimiento de IA requiere historial de decisión, política y revocación en
una entrega posterior, antes de habilitar el agente. La recuperación de contraseña,
el cambio de correo, la eliminación distribuida y la sincronización de preferencias
visuales quedan fuera de esta primera entrega.

## Contrato gRPC propuesto

Archivo: `proto/billetera/usuarios/v1/usuarios.proto`.

| Operación | Resultado y regla principal |
| --- | --- |
| RegisterUser | Crea usuario, preferencias y outbox atómicamente; correo duplicado devuelve ALREADY_EXISTS |
| Login | Credenciales válidas crean sesión; error genérico para correo inexistente o contraseña incorrecta |
| ValidateSession | Devuelve identidad y vencimiento de una sesión vigente para que el BFF autorice la solicitud |
| Logout | Revoca solo la sesión presentada; una repetición con token bien formado desconocido, vencido o revocado devuelve éxito sin informar existencia |
| GetProfile / UpdateProfile | Derivan propietario de la sesión; UpdateProfile exige version esperada y modifica solo nombre |
| GetPreferences / UpdatePreferences | Derivan propietario de la sesión; UpdatePreferences valida el bloque y su version esperada |

RegisterUser y Login requieren llamante BFF autorizado pero no sesión de usuario.
El resto exige sesión, salvo la semántica idempotente de Logout. Ninguna solicitud
de perfil o preferencias permite seleccionar un usuario ajeno mediante un campo
usuario_id. ValidateSession devuelve lo mínimo necesario y no incluye correo.

| Código gRPC | Situación |
| --- | --- |
| INVALID_ARGUMENT | Campo, UUID, moneda, zona, importe o versión inválidos |
| ALREADY_EXISTS | Correo normalizado duplicado |
| UNAUTHENTICATED | Credenciales incorrectas o sesión ausente, vencida o revocada |
| PERMISSION_DENIED | Llamante autenticado sin permiso para esa operación |
| ABORTED | Conflicto de versión; volver a leer antes de editar |
| RESOURCE_EXHAUSTED | Límite de intentos o solicitudes alcanzado |
| UNAVAILABLE | Dependencia necesaria indisponible; Kafka caído no revierte un cambio ya guardado con outbox |
| INTERNAL | Fallo inesperado; devolver diagnóstico seguro con correlación, sin secretos |

Las respuestas de error no incluyen SQL, hashes, tokens ni contraseñas. Los importes
gRPC usan int64; los clientes TypeScript deben decodificarlos como cadenas o una
representación exacta, nunca como Number si pueden superar su rango seguro.
GraphQL y eventos JSON usan cadenas decimales para importes y versiones int64.

## Eventos propuestos

Se toma el catálogo base de DOMAIN_EVENTS.md; los nombres alternativos de otros
documentos siguen pendientes de unificación con los consumidores.

- Tópico: `billetera.usuarios.eventos.v1`; partición por usuarioId.
- `UsuarioRegistrado`: datos `{}`.
- `PreferenciasActualizadas`: versionPreferencias como texto decimal y
  zonaHorariaCambio booleano.
- Sobre: eventoId, tipo, versionEsquema, ocurridoEn, usuarioId, agregadoTipo
  `Usuario`, agregadoId, correlacionId y datos.

No se publican correo, nombre, credenciales ni tokens. El publicador marca el
evento después de la confirmación de Kafka. Si cae entre envío y marcado, lo
reenvía con el mismo eventoId; los consumidores deben deduplicar. La creación del
tópico y la estrategia de publicación se implementarán de manera explícita.

## Plan de ejecución

1. Cerrar decisiones de autenticación y valores iniciales; revisar este contrato
   con BFF, Transacciones y Agente. Acordar distribución y versiones del .proto.
2. Comprobar herramientas Go, protoc y plugins de generación; fijar sus versiones.
3. Inspeccionar PostgreSQL; crear migraciones versionadas y su comando de ejecución.
4. Implementar repositorios pgx y registro atómico con outbox.
5. Implementar hash y validación de contraseñas, sesiones y autorización de llamantes.
6. Generar stubs, implementar handlers gRPC y servidor persistente con cierre ordenado.
7. Implementar publicación Kafka con reintentos y manejo de duplicados.
8. Añadir pruebas, imagen ejecutable, disponibilidad y CI. Integrar luego con BFF.

## Aceptación de la primera entrega

- Registro válido crea exactamente un usuario y un bloque de preferencias.
- Dos registros concurrentes con correo equivalente dejan una sola identidad.
- Fallo durante registro revierte también preferencias y outbox.
- Login incorrecto no crea sesión ni distingue correo inexistente de contraseña errónea.
- Sesión vigente permite operaciones propias; sesión vencida o revocada no autoriza.
- Logout repetido no falla y no revoca las otras sesiones del mismo usuario.
- Llamante sin autorización de servicio no puede operar aunque conozca un UUID.
- Edición con versión antigua no sobrescribe preferencias o perfil nuevos.
- Zona inválida, moneda ajena a CLP, fracciones y valores fuera de rango se rechazan.
- Kafka caído deja eventos pendientes sin perder el cambio; al recuperarse se publican.
- Reiniciar el servicio conserva usuarios, preferencias y sesiones persistidas.
- Ninguna respuesta pública, evento o log contiene hashes de contraseña o tokens.
  La única entrega del token es Login al BFF autorizado por canal protegido.
- Pruebas con PostgreSQL real para transacciones, restricciones y concurrencia;
  pruebas de contrato, aislamiento y al menos 60 % de cobertura de aplicación.

## Estado de este cambio

Se prepararon documentación y contrato Protobuf con stubs Go generados. La primera
migración se aplicó en un nuevo entorno local de Usuarios, después de comprobar
que su base estaba vacía. Se implementaron ejecutor de migraciones, registro
transaccional y hashing Argon2id, con pruebas unitarias e integración PostgreSQL.
No se alteraron bases preexistentes de otros proyectos. RegisterUser ya se expone
por gRPC con TLS mutuo, autorización de BFF y comprobación de salud PostgreSQL.
Se completaron los ocho RPC de registro, login, sesiones, logout, perfil y
preferencias, con sus pruebas. La segunda migración persiste límites de login.
El BFF y el modo integration del frontend ya consumen estas operaciones.
Publicación Kafka y consentimiento IA siguen pendientes. Ver ALCANCE_ACORDADO.md
y [la guía conjunta](../../billetera-bff-graphql/docs/INTEGRACION_LOCAL.md) para el alcance actual.
