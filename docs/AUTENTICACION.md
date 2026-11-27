# Autenticación de la primera entrega

Política implementada para la primera entrega del 26 de noviembre de 2026.
Complementa [Primera entrega](PRIMERA_ENTREGA.md) y [Alcance acordado](ALCANCE_ACORDADO.md).
La integración local con BFF ya usa sesiones y cookies conforme a esta política,
con la excepción HTTP de loopback que se indica más abajo.

## Registro y contraseñas

- Nombre: 1 a 60 caracteres Unicode después de recortar espacios exteriores.
  Correo: dirección simple válida, normalizada con trim y minúsculas y única en
  PostgreSQL, también ante registros concurrentes.
- Contraseña: de 15 a 128 puntos de código Unicode, aceptando espacios y símbolos,
  sin exigir combinaciones de mayúsculas, números o signos. Rechazar UTF-8 inválido;
  no recortar, normalizar ni truncar la contraseña al crear o verificar su hash.
- Argon2id v19: memoria 19456 KiB, 2 iteraciones, paralelismo 1, sal aleatoria de
  16 bytes por contraseña y resultado de 32 bytes. Guardar formato PHC con sus
  parámetros. Medir el costo en el entorno objetivo y limitar hashes simultáneos.
- Comparación de tiempo constante. Login con correo inexistente verifica un hash
  ficticio de igual costo y devuelve el mismo error que una contraseña incorrecta.
- Registro crea usuario, preferencias y evento en una transacción y no inicia
  sesión. Correo duplicado devuelve ALREADY_EXISTS, según el borrador; esto revela
  disponibilidad del correo y debe revisarse antes de exponer registro público.
- Una lista de contraseñas comunes o comprometidas y verificación de correo se
  registran como mejoras futuras; no se envían contraseñas a servicios externos.

Referencias para longitud y hashing:
[OWASP Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
y [OWASP Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).
Los parámetros específicos son decisiones propuestas para esta implementación.

## Sesiones

- Generar 32 bytes con crypto/rand y codificar base64url sin padding. Exigir al
  recibirlo la representación canónica exacta de 32 bytes.
- Guardar SHA-256 de esos bytes en hash_token, nunca el token original. Este hash
  rápido verifica un token aleatorio de alta entropía, no una contraseña.
- Expiración absoluta propuesta: 24 horas. Inactividad máxima: 30 minutos. Ambas
  configurables; la inactividad debe ser positiva y no superar la duración absoluta.
- Usuarios valida revocación y ambos vencimientos en cada operación propia. Una
  validación exitosa actualiza ultima_actividad_en atómicamente solo si la sesión
  aún está vigente; no modifica la expiración absoluta. El instante exacto de
  vencimiento ya se considera inválido. Todos los tiempos se asignan en servidor.
- Login siempre emite un token nuevo. Se admiten varias sesiones por usuario.
  Logout revoca solo la presentada. Con BFF autenticado, un token canónico ya
  revocado, vencido o desconocido devuelve éxito. Token ausente o mal formado
  devuelve UNAUTHENTICATED. El BFF no cachea autorización después de logout.
- ValidateSession.expires_at devuelve el menor entre expiración absoluta y última
  actividad más límite de inactividad, después de validar la sesión.

Referencia: [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
Las duraciones anteriores son propuestas del proyecto, no valores exigidos por OWASP.

## BFF y transporte

- Navegador a BFF por HTTPS: cookie __Host-billetera_session, HttpOnly, Secure,
  SameSite=Lax, Path=/ y sin Domain. Duración no mayor a la sesión; eliminar con
  iguales atributos al salir. No guardar token en localStorage ni devolverlo en GraphQL.
- CSRF en BFF: comprobar Origin frente al origen público configurado, exigir JSON
  y cabecera propia en autenticación y escrituras, rechazar mutaciones por GET y
  evitar CORS con credenciales para orígenes arbitrarios. La cabecera no es identidad.
- BFF a Usuarios mediante gRPC con TLS mutuo. Verificar CA, vigencia, propósito y
  SAN de certificados y permitir estos RPC solo a la identidad configurada del BFF.
  No confiar en caller_id ni en un usuario_id enviado por el navegador. La identidad
  del servicio no sustituye la sesión de usuario.
- BFF propaga token en metadatos authorization: Bearer <token>. Solo Login lo
  devuelve al BFF autorizado por canal protegido; no registrar valores sensibles.
- Certificados fuera de Git. Acordar CA, SAN y rutas por entorno con BFF. Las
  pruebas entre procesos usarán certificados de prueba y TLS mutuo; las unitarias
  pueden usar transportes en memoria. No habilitar login en red compartida sin TLS.
- Cookie, Origin y CSRF son responsabilidad del BFF. Los permisos de consulta de
  Transacciones y Agente se definirán en otro contrato con datos mínimos.

## Límites propuestos

Hasta 5 fallos por correo normalizado en una ventana de 15 minutos; enfriamiento
de 15 minutos al alcanzar el límite. Misma regla para correos inexistentes. BFF
limita además registro y login por origen de red tomando solo proxies confiables.
Usuarios no confunde la IP del BFF con la del usuario final. El límite de fallos
por correo ya se implementó; un límite adicional por origen en BFF queda como mejora.

La migración 000002_login_limits.sql implementa almacenamiento y coordinación de
límites entre réplicas mediante PostgreSQL. No hay bloqueo permanente de cuenta.

El entorno universitario local usa HTTP en 127.0.0.1:8080 y cookie billetera_session
HttpOnly/SameSite=Lax sin Secure. El BFF rechaza esa configuración para otros hosts.
En HTTPS se conserva la política Secure y prefijo __Host-. El transporte gRPC usa
TLS mutuo en ambos casos.

## Consecuencias y pendientes

- sesiones incluye ultima_actividad_en. Perfil y preferencias tienen versiones
  independientes y las actualizaciones exigen su versión esperada.
- El binario scratch debe incorporar time/tzdata para validar zonas IANA.
- Configuración y pruebas deben cubrir expiración, revocación, concurrencia,
  llamante no autorizado y errores que no expongan secretos.
- Coordinar con BFF cookie, CSRF, certificados, límites y distribución del .proto.
- Confirmar CLP, America/Santiago y criterio inicial 3000 CLP / 3 ocurrencias
  frente a la demo que usa 5000 CLP.
- Cambio y recuperación de contraseña, verificación de correo, MFA y consentimiento
  IA corresponden a entregas posteriores antes de activar los flujos que los requieran.
