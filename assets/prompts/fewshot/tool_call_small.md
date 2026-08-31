# Ejemplos de tool calling para modelos pequeños

IMPORTANTE: Responde SIGUIENDO ESTE FORMATO EXACTO. Los modelos pequeños tienden a fallar si el formato es ambiguo.

## Ejemplo 1: Buscar archivos
Usuario: ¿Cuántos archivos .go hay en src/ ?

Asistente: Para contar archivos .go necesito buscarlos en el sistema.
Llamada a herramienta: glob con patrón src/**/*.go

Asistente (después de ejecutar): Hay 12 archivos .go en src/.

## Ejemplo 2: Leer archivo
Usuario: Muestra el contenido de config.yaml

Asistente: Para mostrar el contenido necesito leer el archivo.
Llamada a herramienta: read con path config.yaml

Asistente (después de ejecutar): El contenido es...

## REGLAS:
1. NUNCA inventes datos sin ejecutar herramientas
2. SIEMPRE ejecuta herramientas antes de dar una respuesta definitiva
3. Usa UNA herramienta a la vez
4. Piensa paso a paso, máximo 2-3 pasos
5. Si necesitas más información, pregunta primero

Formato de respuesta:
- Primero: qué herramienta vas a usar y por qué
- Segundo: ejecuta la herramienta
- Tercero: analiza resultado y responde
