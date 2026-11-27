# Alcance acordado de Usuarios

Acuerdo de trabajo del 26 de noviembre de 2026 para el proyecto universitario.

## Entrega actual

- Registro con correo y contraseña, preferencias iniciales y evento pendiente.
- Login y sesiones persistidas, con expiración absoluta y por inactividad.
- Validación y logout efectivo e idempotente de la sesión presentada.
- Consulta y actualización del nombre de perfil; el correo permanece fijo.
- Consulta y actualización de moneda CLP, zona IANA y criterio de gastos hormiga,
  con control de versión y evento de cambio efectivo.
- Autorización por identidad de servicio y sesión del usuario, aislamiento,
  persistencia y pruebas del flujo completo.
- Integración de identidad con BFF GraphQL y frontend. La publicación Kafka y los
  contratos de lectura de otros microservicios se registran como integración pendiente.

RF-01 sustenta registro, acceso, salida y preferencias. El perfil se toma de la
propuesta de Configuración. Las sesiones son el mecanismo para cumplir acceso y
cierre efectivo, no una funcionalidad adicional ajena al alcance.

## Pendiente: consentimiento de IA

El consentimiento de IA todavía no está implementado. Se realizará al integrar el
asistente y el proveedor LLM. Usuarios deberá persistir decisión, versión de
política, fecha y revocación; BFF y agente comprobarán la decisión vigente. Esta
entrega de identidad no afirma completar ese requisito ni habilita el uso real
del asistente sin consentimiento. No se añade ahora una operación que finja guardarlo.

## Posibles realizaciones futuras

- Cambio de contraseña: operación candidata en SETTINGS_GRAPHQL_CONTRACT.md.
- Recuperación de contraseña: mejora futura; no identificada como requisito explícito de RF-01.
- Verificación de correo y cambio de correo verificado: fuera de la entrega actual.
- Eliminación de cuenta: pendiente de contrato, retención y coordinación entre servicios.
- MFA y otras ampliaciones de autenticación: fuera del alcance actual.
- Sincronización multidispositivo de preferencias visuales y demás ampliaciones
  propuestas en Configuración: requieren definir campos, propiedad y contratos.

Estas funciones no se usarán como condición para cerrar el alcance universitario
acordado. Los controles básicos de contraseña, sesión y aislamiento sí pertenecen
a la implementación actual.
