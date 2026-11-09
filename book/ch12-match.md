---
chapter: 12
title: `match` y exhaustividad
part: II
hours: 14
milestone: **M2**: tu programa `Shape`/`area`/`match`, con errores golden: un brazo que falta (con testigo), brazos de tipos distintos, un nombre desconocido y un argumento mal tipado
---

## Objetivo

Ki desmonta sus datos con `match`, y el compilador comprueba que cada `match` cubre todos los valores. Al terminar, `build/bin/ki run tests/golden/ch12/shapes.ki` imprime `6.0`, y `ki check` da los cuatro errores golden del hito M2:

```ki
type Shape {
  Circle(radius: Float)
  Rect(w: Float, h: Float)
}
fn area(s: Shape) -> Float {
  match s {
    Circle(r) => 3.141592653589793 * r * r
    Rect(w, h) => w * h
  }
}
fn main() {
  printFloat(area(Rect(2.0, 3.0)))
  printFloat(area(Circle(1.0)))
}
```

## Teoría

### Los patrones, un lenguaje pequeño

Un patrón dice qué forma tiene un valor y qué nombres reciben sus partes.

```math
pat   = alt { '|' alt }
alt   = '_' | lower | entero | '-' entero | 'true' | 'false'
      | Upper [ '(' pat { ',' pat } [ ',' ] ')' ]
      | Upper '{' [ fpat { ',' fpat } [ ',' [ '..' ] ] | '..' ] '}'
      | '(' pat ')' | '(' pat ',' pat { ',' pat } ')'
      | '[' [ pat { ',' pat } [ ',' resto ] | resto ] ']'          listas: sin tipo hasta su capítulo
resto = '..' ( lower | '_' )                                      el resto va siempre el último
fpat  = lower [ ':' pat ]                                         x sola liga el campo x (pun)
match = 'match' expr '{' { pat [ 'if' expr ] '=>' expr sep } '}'
stmt += 'let' pat [ ':' ty ] '=' expr
```

Las variables y los puns ligan; los constructores, los literales y la forma de una tupla o un record comprueban. Un patrón se tipa contra el valor y da las variables que liga, `p : τ ⊣ Δ`:

```math
_ : τ ⊣ ∅       x : τ ⊣ {x : τ}       n : Int ⊣ ∅       true, false : Bool ⊣ ∅

  C(τ₁, …, τₐ) es una variante de T    pᵢ : τᵢ ⊣ Δᵢ    los Δᵢ no comparten nombres
 ──────────────────────────────────────────────────────────── (PCtor)
                    C(p₁, …, pₐ) : ⟦T⟧ ⊣ Δ₁ ∪ … ∪ Δₐ

  p₁ : τ ⊣ Δ    p₂ : τ ⊣ Δ                Γ ⊢ e ⇒ τ    pᵢ : τ ⊣ Δᵢ    Γ, Δᵢ ⊢ gᵢ ⇐ Bool    Γ, Δᵢ ⊢ eᵢ ⇐ σ
 ───────────────────── (POr)    ────────────────────────────────────────────────────── (Match)
     p₁ | p₂ : τ ⊣ Δ                           Γ ⊢ match e { pᵢ [if gᵢ] => eᵢ } ⇒ σ

premisa rota:   un número de pᵢ que no es a → E0705    dos Δᵢ con un nombre → E0704    p₁ y p₂ ligan nombres distintos → E0703
```

`σ` es el tipo esperado o, sin él, el del primer brazo. Tuplas y records son (PCtor) con un solo constructor, y un `let` con patrón se tipa como un brazo.

**Los `Float` no son patrones** (E0706). Casar por valor exige una equivalencia, y el `==` de IEEE 754 no lo es: con `NaN != NaN`, un brazo `NaN` nunca casaría, y `0.0 == -0.0` junta dos valores distintos. La salida es una guarda: `x if x == 1.5`.

### Gana el primer brazo

Gana el primer brazo cuyo patrón casa con el valor del `match`, su **escrutinio**, y cuya guarda, si la tiene, es verdadera. **Fe de erratas** de los capítulos 9 y 10: un `true` ya no gana a un `_` anterior, y Lint ya no comprueba la cobertura, que es del pase `patterns`.

Un or-patrón con guarda son dos brazos:

```math
p₁ | p₂ if g => e      ≡      p₁ if g => e
                              p₂ if g => e
```

Si `g` es falsa con `p₁`, se prueba `p₂` antes que el brazo siguiente, como en Rust: con `type Opt { No, Yes(Int) }`, `(Yes(x), _) | (_, Yes(x)) if x > 0` gana con `(Yes(0), Yes(1))` y `x = 1`. La guarda puede correr dos veces.

### La matriz y la utilidad

Maranget (*Warnings for pattern matching*, 2007) trabaja sobre una **matriz de patrones** `P`: una fila por brazo, una columna por parte del valor por comparar. Un vector `q` es **útil** si algún valor casa con `q` y con ninguna fila:

```math
Σ        los constructores que encabezan filas de P
S(c, P)  de cada fila: c(p₁…pₐ) · resto → p₁…pₐ · resto;   _ · resto → _ × a · resto;   otro constructor → fuera
D(P)     las filas que empiezan por _, sin esa celda          una fila que empieza por r₁ | r₂ son dos filas

U(∅, q)  = sí                                     U(P, ()) = no, si P tiene filas
U(P, (c(r₁, …, rₐ), q₂, …, qₙ)) = U(S(c, P), (r₁, …, rₐ, q₂, …, qₙ))
U(P, (r₁ | r₂, q₂, …, qₙ))      = U(P, (r₁, q₂, …)) ∨ U(P, (r₂, q₂, …))
U(P, (_, q₂, …, qₙ))            = ∨_{c ∈ Σ} U(S(c, P), (_ × aridad(c), q₂, …, qₙ))   si Σ es completo
                                = U(D(P), (q₂, …, qₙ))                               si no
```

**Σ es completo** si nombra todos los constructores: los dos de `Bool`, el único de una tupla o un record, los de una suma. En `Int` nunca lo es. Las preguntas del pase son `U`:

```math
el match es exhaustivo          ⇔   ¬U(P, (_))                     P: los brazos sin guarda
el brazo i es inalcanzable      ⇔   ¬U(P₁…ᵢ₋₁, (pᵢ))               aviso W0701
el let p = e es irrefutable     ⇔   ¬U({p}, (_))                   si no, E0702
```

Una fila de solo comodines casa con todo, así que `U` da no en cuanto la matriz la tiene, sin buscar más (§7.2 de Maranget): un brazo `_` sale gratis.

### El testigo es el resultado

Cuando `U(P, (_))` da sí, el camino de la búsqueda describe un valor que ningún brazo casa. Ese **testigo** no está entre los patrones escritos: se construye.

```math
W(∅, n columnas)  = (_, …, _)
W(P, (_ : τ, …))  = (c(w₁, …, wₐ), w′)       Σ completo: algún c con W(S(c, P)) = (w₁, …, wₐ, w′)
                  = (c′(_, …, _), w′)        Σ incompleto, c′ ∉ Σ, con W(D(P)) = w′;  (_, w′) si Σ = ∅
                  = (k, w′)                  en Int: k es el menor número desde 0 que ninguna fila nombra
```

Así, `(true, _)` y `(_, true)` dejan fuera `(false, false)`:

```math
P = (true, _) y (_, true),  q = (_, _)
Σ = {true}, incompleto   →   U(D(P), (_)),  D(P) = la fila (true)
Σ = {true}, incompleto   →   U(∅, ()) = sí;   W = (false, false)
```

Y `0 => …` con `1 => …` deja fuera `2`.

### Varios testigos son otra búsqueda

`W`, el algoritmo I de Maranget, da un testigo y para. Seguir da más, pero cuáles depende de dónde parte la búsqueda. Con `type T { A, B, C }` y el brazo `(A, A)`, la primera columna no es completa y la búsqueda sigue por `D(P)`: da `(B, _)` y `(C, _)`, sin seguir por `A`. `(A, B)` solo aparece al añadir esos brazos.

El número de testigos no es canónico: E0701 nombra hasta tres, dice «and more» si existe un cuarto, y nunca cuenta.

### Difícil como SAT: el presupuesto

Decidir si un `match` de tuplas de `Bool` es exhaustivo resuelve SAT:

```math
φ = C₁ ∧ … ∧ Cₘ sobre x₁, …, xₙ
brazo j = tupla de n Bool:  false en i si xᵢ ∈ Cⱼ,   true si ¬xᵢ ∈ Cⱼ,   _ si xᵢ no aparece
el brazo j casa justo las asignaciones que hacen falsa Cⱼ
match exhaustivo  ⇔  toda asignación hace falsa alguna Cⱼ  ⇔  φ no tiene solución
testigo           =  una asignación que satisface φ
```

Así que el motor del pase cuenta pasos (un nodo, o llamada de `U`; una fila construida; una celda empujada) y, pasados 10 000 000, se rinde con E0707: callar daría por bueno lo no probado. Los brazos inalcanzables usan lo que sobra, y si se agota, callan: solo dan avisos.

### Las guardas no cuentan

Saber si una guarda es verdadera exige ejecutarla. Un brazo con guarda puede fallar con cualquier valor, así que no es una fila de `P`: no cubre nada. Su patrón sí se compara con los de arriba: tras `m => 2`, `k if k > 0` es inalcanzable.

### Error o aviso

La pregunta es si el programa significa algo para cada valor. Con E0701, no: un valor se quedaría sin brazo. Con E0707, no se sabe. Con W0701, un aviso (*warning*), sí: el brazo sobra, pero no cambia nada.

`ki check` escribe los avisos, ordenados por posición con los errores, y sale con 0 si no hay errores. `ki emit` los calla, y `ki run` si no hay errores: la primera línea de stderr de un panic sigue siendo suya.

### El matcher ingenuo del oráculo

El oráculo casa como dice la semántica, brazo a brazo:

```pseudo
función casa(match, v):
  para cada brazo, de arriba abajo:
    pila ← [(patrón del brazo, v)];  elecciones ← []
    mientras la pila no esté vacía:
      saca (p, x)
      si p es _ entonces sigue;  si p es una variable entonces escribe x en su casilla
      si p es un literal o un constructor que no es el de x:
        si quedan elecciones entonces retoma la última;  si no, el brazo falla
      si p es C(p₁, …, pₐ) entonces mete (pᵢ, campo i de x), p₁ arriba
      si p es p₁ | … | pₖ entonces mete (p₁, x) y guarda el resto como elección
    si el brazo casó y su guarda es verdadera entonces devuelve su cuerpo
    si la guarda es falsa y quedan elecciones entonces retoma la última en este brazo
  imposible tras `patterns`: un panic de Go, un ICE
```

Mira las mismas partes en cada brazo. Los árboles de decisión de los backends mirarán cada parte una vez, y deberán coincidir con él.

### Recursión sobre matrices, o una pila

Cada llamada de `U` consume una columna: su profundidad es el tamaño de los patrones, y una tupla de 100 000 elementos son 100 001 niveles. Motor y oráculo usan una pila propia (capítulo 9). Las filas son listas enlazadas inmutables: quitar la primera celda o poner delante los subpatrones cuesta lo añadido, y comparten colas. En el pase `patterns` solo recurre `lower`, que baja un patrón a la matriz: un nivel por anidamiento, acotado por `MaxPatternDepth = 256`.

### Bitsets

Para saber si `Σ` es completo, el motor anota los constructores vistos en un **bitset**: un `[]uint64`, un bit por constructor. Contarlos es un recuento de unos (*popcount*) por palabra, y los que faltan salen del complemento.

### Slices que comparten array

Un subslice comparte el array de su origen (capítulo 1): un `append` sobre `rows[:first]` escribiría encima de `rows[first]`, en filas ajenas. Por eso el motor parte los or-patrones sobre `slices.Clone(rows[:first])`.

### El hito M2

`shapes.ki`, el programa del Objetivo, pasa por cada etapa:

1. **El parser** lee los dos brazos.
2. **`resolve`** liga `r`, `w` y `h`.
3. **`declare`** da a `Shape` sus dos constructores.
4. **`check`** tipa los patrones contra `Shape` y los brazos como `Float`.
5. **`patterns`** ve `{Circle, Rect}` completo: ni E0701 ni W0701.
6. **La elaboración** hace un `Match` de Core con dos `PatCon`.
7. **El oráculo** elige el brazo de `Rect`, y `fmtnum` escribe `6.0`.

## Preguntas de repaso

:::review
¿Por qué un `Float` no puede ser un patrón y un `Int` sí?
:::

:::answer
Casar por valor pide una equivalencia. El `==` de `Int` lo es; el de IEEE 754 no: `NaN != NaN`, así que un brazo `NaN` nunca casaría, y `0.0 == -0.0` junta dos valores distintos. Con una guarda, `x if x == 1.5`, la comparación queda escrita y es responsabilidad del programa.
:::

:::review
¿Por qué un brazo con guarda no es una fila de la matriz, pero su patrón sí se compara con los de arriba?
:::

:::answer
Para cubrir, un brazo debe casar siempre que su patrón casa, y una guarda puede ser falsa con cualquier valor sin que el compilador lo sepa sin ejecutarla. Para ser alcanzable, en cambio, basta que algún valor llegue al brazo: si los brazos de arriba, sin guarda, ya los toman todos, la guarda nunca se evalúa.
:::

:::review
¿Por qué el testigo se construye en lugar de buscarse entre los patrones del programa?
:::

:::answer
Porque es justo lo que ningún brazo escribe. La búsqueda de `U` lo describe con su camino: el constructor que eligió en cada columna completa, y uno que falta, o el menor entero ausente, donde la columna no lo era. Leer ese camino da un patrón, `Rect(_, _)`, que no está en el texto.
:::

:::review
¿Por qué E0701 nombra hasta tres testigos y nunca dice cuántos faltan?
:::

:::answer
Porque el conjunto de testigos depende de por dónde parte la búsqueda: con `(A, A)` salen `(B, _)` y `(C, _)`, y `(A, B)` solo aparece después. Un total sería un número del algoritmo, no del programa. «and more» se dice solo si la búsqueda encontró un cuarto.
:::

:::review
¿Por qué agotar el presupuesto es un error en la cobertura y silencio en los brazos inalcanzables?
:::

:::answer
La cobertura es una garantía: sin ella, un valor podría quedarse sin brazo. Darla por buena sin probarla sería mentir, y callar también. Un brazo inalcanzable no cambia lo que el programa calcula, así que dejar de buscarlos solo pierde avisos.
:::

:::review
¿Por qué `p₁ | p₂ if g` prueba `p₂` antes que el brazo siguiente?
:::

:::answer
Porque el or-patrón con guarda son dos brazos, `p₁ if g` y `p₂ if g`, uno tras otro. Con `(Yes(x), _) | (_, Yes(x)) if x > 0` y `(Yes(0), Yes(1))`, la primera alternativa liga `x = 0` y la guarda falla; la segunda liga `x = 1` y gana. Saltar al brazo siguiente perdería ese valor.
:::

:::review
¿Por qué la búsqueda va en una pila propia si `U` se define por recursión?
:::

:::answer
Porque cada paso de `U` consume una columna o la cambia por los subpatrones, y su profundidad es el tamaño de los patrones, que el programa decide: ninguna pila de Go de tamaño fijo basta para todos, y los tests la bajan a 1 MiB para que se note. Con una pila de nodos en el heap, la profundidad es un slice que crece.
:::

## Resumen

- Un patrón comprueba una forma y liga nombres; un `let` con patrón se tipa como un brazo.
- Un `Float` no es un patrón: su `==` no es una equivalencia.
- Gana el primer brazo que casa; un or-patrón con guarda son dos brazos.
- La utilidad de Maranget decide la exhaustividad y los brazos inalcanzables.
- El testigo se construye con el camino de la búsqueda; varios no se cuentan.
- Saber si falta un valor es NP-completo: más de 10 000 000 pasos es E0707.
- Las guardas no cubren nada, pero su patrón puede ser inalcanzable.
- Un error, si algún valor queda sin significado; un aviso, si solo sobra código.
- Búsqueda y matcher usan pilas propias; un bitset con `math/bits` sigue los constructores.

## Go en el trabajo

**Genéricos o interfaz.** Ian Lance Taylor, *When To Use Generics* (blog de Go, 2022): si solo llamas a un método del valor, o la implementación cambia con el tipo, usa una interfaz. Los parámetros de tipo son para estructuras de datos generales, cuando vas a escribir el mismo código varias veces. `typeShapes`, la interfaz del motor, deja que sus tests inventen tipos.

**`switch` sin exhaustividad.** Un `switch` de Go al que le falta un caso no hace nada, y `go vet` calla. Dos linters de golangci-lint lo añaden: `exhaustive` mira los `switch` sobre un tipo enumerado (un tipo definido sobre un número o un string, con sus constantes en el mismo bloque) y, si se le pide, los de tipos; `gochecksumtype`, los `switch` de tipos sobre una interfaz sellada marcada `//sumtype:decl`, la forma de los `Value` del oráculo.

## Tests primero

{{cards}}

Por cada tarjeta: escribe el test, míralo fallar, implementa lo justo para el verde; solo entonces despliega su referencia. Las sesiones cambian tests anteriores; si uno tuyo falla, compáralo con `ref/ch12-sK`.

## Implementación

### Sesión 1: patrones y `match` en el parser (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T12.01–T12.17, T12.20 y T12.21.** La interfaz:

```go
package syntax
type PatID uint32
type PatKind uint8
const (
	PatWildcard PatKind = iota // `_`
	PatBind                    // `x`: Name, NameSpan
	PatLit                     // `0`, `-1`, `true`: Lit and its payload
	PatCtor                    // `Some(x)`, `Point { x, y: p, .. }`: Name, NameSpan, Args or Fields, Record, HasRest
	PatTuple                   // `(a, b)`: Args
	PatList                    // `[a, b, ..t]`: Args, Rest, HasRest
	PatOr                      // `A | B | C`: Args, two or more
	PatError                   // a pattern that could not be read, or a `Float` literal
)
type LitKind uint8
const (
	LitInt LitKind = iota
	LitBool
	LitChar
	LitStr
	LitUInt
)
type Pat struct {
	Kind           PatKind
	Span, NameSpan base.Span
	Name           base.Symbol
	Lit            LitKind
	Int            int64
	Bool           bool
	Char           rune
	Str            string
	Args           base.ListRange // over Module.PatPool
	Fields         base.ListRange // over Module.FieldPatPool
	Record         bool
	Rest           PatID
	HasRest        bool
}
type FieldPat struct {
	Name   base.Symbol
	Span   base.Span
	Pat    PatID
	HasPat bool // false: a pun, `Point { x }`
}
type MatchArm struct {
	Pat      PatID
	Guard    ExprID
	HasGuard bool
	Body     ExprID
	Span     base.Span
	Broken   bool // the arm could not be read to its end
}
const ExprMatch ExprKind = ExprTuple + 1 // Scrutinee, Arms
// Expr gains: Scrutinee ExprID; Arms base.ListRange. Stmt gains: Pat PatID; HasPat bool
// Module gains: Pats base.IdVec[PatID, Pat]; PatPool []PatID; ArmPool []MatchArm; FieldPatPool []FieldPat
func (m *Module) PatList(r base.ListRange) []PatID
func (m *Module) Arms(r base.ListRange) []MatchArm
func (m *Module) FieldPats(r base.ListRange) []FieldPat
const MaxPatternDepth = 256
```

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/syntax
# github.com/aldezex/kilang/internal/syntax_test [github.com/aldezex/kilang/internal/syntax.test]
internal/syntax/ast_printer_pat_test.go:13:29: undefined: syntax.Pat
…
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
FAIL
```

#### El parser

{{file ch12-s1 internal/syntax/parser_pat.go}}

{{diff ch11-s4..ch12-s1 internal/syntax/ast.go}}

{{diff ch11-s4..ch12-s1 internal/syntax/parser.go}}

{{diff ch11-s4..ch12-s1 internal/syntax/parser_expr.go}}

{{diff ch11-s4..ch12-s1 internal/syntax/parser_stmt.go}}

{{diff ch11-s4..ch12-s1 internal/syntax/recover.go}}

{{diff ch11-s4..ch12-s1 internal/syntax/ast_printer.go}}

1. **`pattern`** es la gramática de la Teoría, con las alternativas en un bucle: `A | B | C` es un nodo de tres hijos.
2. **Un brazo acaba** en un salto de línea o en `;`; un `=>` o un `|` al final de línea lo continúa.
3. **Un brazo roto** queda marcado con `Broken`, y se sigue en el siguiente.
4. **En la cabeza de un `match`**, como en la de un `if`, `Nombre {` no es un literal.

{{reftest T12.03}}

`armPat` lee el patrón del único brazo de `fn f() { match x { <patrón> => 1 } }`, y `patText`, el texto que cubre un span.

#### Lo demás espera

Hasta la sesión 2, `resolve` y `check` leen solo el escrutinio o el inicializador; la elaboración hace panic hasta la 4.

{{diff ch11-s4..ch12-s1 internal/sema/resolve.go}}

{{diff ch11-s4..ch12-s1 internal/sema/check.go}}

{{diff ch11-s4..ch12-s1 internal/core/elaborate.go}}

La ayuda de E0105 añade la guarda que casa el menor `Int`:

{{diff ch11-s4..ch12-s1 internal/syntax/numbers.go}}

{{diff ch11-s4..ch12-s1 docs/errors/E0105.md}}

{{diff ch11-s4..ch12-s1 docs/errors/E0201.md}}

{{diff ch11-s4..ch12-s1 docs/errors/E0204.md}}

{{file ch12-s1 docs/errors/E0706.md}}

{{file ch12-s1 tests/golden/ch12/patterns_ast.ki}}

{{file ch12-s1 tests/errors/ch12/float_pattern_render.ki}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch12-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch12-s1`; compárate con `ref/ch12-s1`.

### Sesión 2: tipos de los patrones y del `match` (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T12.18, T12.19, T12.22 y T12.24–T12.36.** La interfaz:

```go
package syntax
// Pat gains: OneLine, TrailingComma bool (the edits of E0705 and E0410)

package sema
// ModuleRes gains: PatLocal []LocalID (by PatID); FieldPatLocal []LocalID (by FieldPatPool index); PatRes []Res (by PatID)
// TypedModule gains: PatTypes []TypeID (by PatID)
const (
	ReasonScrutinee ReasonKind = ReasonField + 1 + iota
	ReasonMatchArms
	ReasonPatternArg
)
// Reason gains: Whole TypeID (ReasonScrutinee); InArm bool (ReasonBlockTail)
func (d *Decls) OwnerType(c CtorID) TypeID
func (u *Unifier) Reject(expected TypeID, found string, foundAt base.Span, r Reason)
```

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/sema
# github.com/aldezex/kilang/internal/sema_test [github.com/aldezex/kilang/internal/sema.test]
internal/sema/pattern_check_test.go:190:43: typed.PatTypes undefined (type *sema.TypedModule has no field or method PatTypes)
…
FAIL	github.com/aldezex/kilang/internal/sema [build failed]
FAIL
```

#### Los nombres

{{file ch12-s2 internal/sema/resolve_pat.go}}

{{diff ch12-s1..ch12-s2 internal/sema/resolve.go}}

1. **Cada variable es un `Local`**, visible en la guarda y el cuerpo de su brazo, o tras su `let`.
2. **Las alternativas** de un or-patrón comparten un `Local` por nombre.

#### Los tipos

{{file ch12-s2 internal/sema/check_pat.go}}

{{diff ch12-s1..ch12-s2 internal/sema/check.go}}

{{diff ch12-s1..ch12-s2 internal/sema/check_data.go}}

{{diff ch12-s1..ch12-s2 internal/sema/declare.go}}

{{diff ch12-s1..ch12-s2 internal/sema/reason.go}}

{{diff ch12-s1..ch12-s2 internal/sema/unify.go}}

{{diff ch12-s1..ch12-s2 internal/syntax/ast.go}}

{{diff ch12-s1..ch12-s2 internal/syntax/parser_pat.go}}

1. **`check_pat.go`** son (PCtor), (POr) y (Match); cada razón nueva da su etiqueta.
2. **Un patrón de record** se comprueba como un literal, con su propio E0412, y la edición de E0410 añade `..`.
3. **`Reject`** es el E0401 de una forma imposible, como una lista, que aún no tiene tipo.

{{reftest T12.28}}

`checkRows`, al principio del fichero, compara los diagnósticos de cada `diagRow` con su `want`; `scrutineeMismatch` es el E0401 contra el escrutinio.

{{diff ch12-s1..ch12-s2 docs/errors/E0306.md}}

{{diff ch12-s1..ch12-s2 docs/errors/E0401.md}}

{{diff ch12-s1..ch12-s2 docs/errors/E0409.md}}

{{diff ch12-s1..ch12-s2 docs/errors/E0410.md}}

{{diff ch12-s1..ch12-s2 docs/errors/E0411.md}}

{{diff ch12-s1..ch12-s2 docs/errors/E0412.md}}

{{file ch12-s2 docs/errors/E0703.md}}

{{file ch12-s2 docs/errors/E0704.md}}

{{file ch12-s2 docs/errors/E0705.md}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch12-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch12-s2`; compárate con `ref/ch12-s2`.

### Sesión 3: exhaustividad y redundancia (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T12.23, T12.37–T12.53 y T12.57.** La interfaz:

```go
package base
type BitSet struct{ /* words []uint64; n int */ }
func NewBitSet(n int) BitSet
func (s *BitSet) Len() int
func (s *BitSet) Set(i int)
func (s *BitSet) Has(i int) bool
func (s *BitSet) Count() int
func (s *BitSet) NextClear(from int) (int, bool)

package syntax
// MatchArm gains: Removal base.Span; RemovalOneLine bool (the edit of W0701)

package sema
func CheckPatterns(mods []*syntax.Module, res *ResolvedProgram, typed *TypedProgram, in *base.Interner, sink *diag.Sink)
// export_internal_test.go, compiled only for tests: they lower patternBudget
func SetPatternBudget(n int) (restore func())
```

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/base
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/bitset_test.go:16:10: undefined: NewBitSet
…
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

#### Go: `math/bits`

`bits.OnesCount64` cuenta los unos de una palabra, y `bits.TrailingZeros64`, los ceros por la derecha: la posición del primer uno, o 64 si no hay ninguno.

```go
bits.OnesCount64(0b1011)        // 3
bits.TrailingZeros64(0b1000)    // 3
bits.TrailingZeros64(0)         // 64
bits.TrailingZeros64(^uint64(0b0111)) // 3: el primer cero de 0b0111
```

{{file ch12-s3 internal/base/bitset.go}}

`NextClear` busca en el complemento, y descarta los bits de la última palabra que pasan de `Len`.

{{reftest T12.38}}

#### El motor

{{file ch12-s3 internal/sema/pattern_matrix.go}}

1. **`search`** es `U` y `W`: una pila de nodos, cada uno con sus filas, su vector y su camino.
2. **`expandWild`** abre un nodo por constructor si `Count` dice que `Σ` es completo; si no, sigue por `D(P)`.
3. **`spend`** cuenta antes de trabajar, y rechaza un nodo antes de construir sus filas.
4. **`armMatrix`** indexa las filas por su primer constructor: el brazo 100 000 mira las de su literal, no las 99 999.
5. **Escribe ahora T12.54–T12.56**: prueban el motor por dentro.

#### Go: un subslice comparte su array

`splitOr` recibe filas que no son suyas:

```go
rows := []int{1, 2, 3}
out := append(rows[:1], 9)               // rows es [1 9 3]
out = append(slices.Clone(rows[:1]), 9)  // rows no cambia
```

#### El pase `patterns`

{{file ch12-s3 internal/sema/patterns.go}}

{{diff ch12-s2..ch12-s3 internal/syntax/ast.go}}

{{diff ch12-s2..ch12-s3 internal/syntax/parser_stmt.go}}

{{diff ch12-s2..ch12-s3 internal/driver/pipeline.go}}

{{diff ch12-s2..ch12-s3 internal/driver/session.go}}

{{diff ch12-s2..ch12-s3 internal/driver/emit.go}}

{{diff ch12-s2..ch12-s3 internal/driver/run.go}}

{{diff ch12-s2..ch12-s3 internal/diag/diagnostic.go}}

1. **`lower`** da a cada patrón la forma del motor; si no encaja, `check` ya habló y el `match` se deja.
2. **`checkMatch`** pregunta la cobertura con todo el presupuesto, y los brazos inalcanzables con lo que sobra.
3. **`reportErrors`**, para `ki emit`, escribe solo los errores.

{{file ch12-s3 docs/errors/E0701.md}}

{{file ch12-s3 docs/errors/W0701.md}}

:::note
**Fe de erratas.** Con errores, `ki run` sí escribe el aviso.
:::

{{file ch12-s3 docs/errors/E0702.md}}

{{file ch12-s3 docs/errors/E0707.md}}

:::note
**Fe de erratas.** Que ningún algoritmo responda rápido, aquí y en `pattern_matrix.go`, es la conjetura P ≠ NP.
:::

{{diff ch12-s2..ch12-s3 docs/errors/E0704.md}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch12-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch12-s3`; compárate con `ref/ch12-s3`.

### Sesión 4: patrones en Core y el matcher ingenuo (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T12.58–T12.84.** La interfaz:

```go
package core
type PatID uint32
const (
	PatInt  PatKind = PatWildcard + 1 + iota // Int
	PatBind                                  // Var
	PatCon                                   // Ctor, Args
	PatOr                                    // Args: the alternatives
)
type Pat struct {
	Kind PatKind
	Span base.Span
	Int  int64
	Var  VarID
	Ctor CtorID
	Args base.ListRange // over Program.PatPool
}
type Arm struct {
	Pat      PatID // was a PatKind; an `if` has PatTrue and PatFalse nodes
	Guard    ExprID
	HasGuard bool
	Body     ExprID
}
// Program gains: Pats base.IdVec[PatID, Pat]; PatPool []PatID
func (p *Program) PatList(r base.ListRange) []PatID
func (b *Builder) Pat(p Pat) PatID

package coretest
func UncoveredMatchProgram() *core.Program // a Match that no value falls through, for the ICE
```

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/core
# github.com/aldezex/kilang/internal/core_test [github.com/aldezex/kilang/internal/core.test]
internal/core/lint_test.go:854:16: undefined: core.PatID
…
FAIL	github.com/aldezex/kilang/internal/core [build failed]
FAIL
```

#### Core

{{diff ch12-s3..ch12-s4 internal/core/core.go}}

{{diff ch12-s3..ch12-s4 internal/core/builder.go}}

{{diff ch12-s3..ch12-s4 internal/core/elaborate.go}}

{{diff ch12-s3..ch12-s4 internal/core/lint.go}}

{{diff ch12-s3..ch12-s4 internal/core/print.go}}

{{diff ch12-s3..ch12-s4 internal/core/coretest/extern.go}}

1. **Una tupla o un record** es un `PatCon`; un campo no nombrado, `_`.
2. **Un `let` con patrón** es un `Match` de un brazo sobre el resto del bloque.
3. **Lint** comprueba la casilla de cada `PatBind` y la aridad de cada `PatCon`.

#### El matcher

{{diff ch12-s3..ch12-s4 internal/interp/machine.go}}

`matchFrom` y `fit` son `casa`. La pila es una lista enlazada de `patWork`: una elección guarda la pila de su momento, y retomarla vuelve a esa pila. Un `guardFrame` espera la guarda y cuenta como casillas lo que retiene.

{{diff ch12-s3..ch12-s4 internal/interp/machine_test.go}}

Este diff borra la regla del capítulo 10: ahora `[_ => w, true => t]` elige `w`.

{{reftest T12.75}}

`matchRows` compila el programa y llama a cada función de la tabla, que compara con su valor.

{{file ch12-s4 tests/golden/ch12/match_run.ki}}

{{file ch12-s4 tests/golden/ch12/match_core.ki}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch12-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch12-s4`; compárate con `ref/ch12-s4`.

### Sesión 5: el hito M2, los goldens y la caza de cascadas (≈ 2,5 h)

1. **Escribe T12.85–T12.96**, los goldens del hito. Pasan a la primera: fijan lo que el compilador ya hace.
2. **Las cascadas**, un error que arrastra otros, las buscó la referencia: `bookx cascade`, que tu repo no necesita, rompió los programas de los capítulos 11 y 12, y `ki` no tuvo que cambiar.

{{file ch12-s5 tests/golden/ch12/shapes.ki}}

{{file ch12-s5 tests/errors/ch12/missing_arm.ki}}

{{file ch12-s5 tests/errors/ch12/arm_types.ki}}

{{file ch12-s5 tests/errors/ch12/unknown_name.ki}}

{{file ch12-s5 tests/errors/ch12/bad_argument.ki}}

{{file ch12-s5 tests/errors/ch12/missing_brace.ki}}

Los cuatro primeros tienen `_render`, el dibujo de «Ejecútalo»; dos más fijan el aviso, solo y entre dos errores.

```console
$ go test ./tests
ok  	github.com/aldezex/kilang/tests	…
```

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch12-s5` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch12-s5`; compárate con `ref/ch12-s5`.

## Ejecútalo

El hito:

```console
$ build/bin/ki run tests/golden/ch12/shapes.ki
6.0
3.141592653589793
$ build/bin/ki check tests/errors/ch12/missing_arm_render.ki
error[E0701]: non-exhaustive match
  --> tests/errors/ch12/missing_arm_render.ki:17:9
   |
17 |   match s {
   |         ^ pattern `Rect(_, _)` not covered
   = note: a `match` must cover every value; guards do not count
   = help: add an arm for `Rect(_, _)`
$ build/bin/ki check tests/errors/ch12/arm_types_render.ki
error[E0401]: mismatched types
  --> tests/errors/ch12/arm_types_render.ki:20:19
   |
19 |     Circle(r) => 2.0 * r
   |                  ------- the first arm has type `Float`
20 |     Rect(w, h) => w > h
   |                   ^^^^^ expected `Float`, found `Bool`
$ build/bin/ki check tests/errors/ch12/unknown_name_render.ki
error[E0301]: unknown name `ares`
  --> tests/errors/ch12/unknown_name_render.ki:25:14
   |
25 |   printFloat(ares(Rect(2.0, 3.0)))
   |              ^^^^ not found in this scope
   = help: did you mean `area`?
   |
25 -   printFloat(ares(Rect(2.0, 3.0)))
25 +   printFloat(area(Rect(2.0, 3.0)))
$ build/bin/ki check tests/errors/ch12/bad_argument_render.ki
error[E0401]: mismatched types
  --> tests/errors/ch12/bad_argument_render.ki:24:19
   |
17 | fn area(s: Shape) -> Float {
   |         -------- the parameter is declared here
   ...
24 |   printFloat(area(2.0))
   |                   ^^^ expected `Shape`, found `Float`
```

Con el aviso, `ki check` sale con 0:

```console
$ build/bin/ki check tests/errors/ch12/warning_render.ki
warning[W0701]: unreachable pattern
  --> tests/errors/ch12/warning_render.ki:22:5
   |
22 |     Rect(w, _) => w
   |     ^^^^^^^^^^ covered by the arms above
   = help: remove this arm
   |
22 -     Rect(w, _) => w
22 +
$ echo $?
0
$ build/bin/ki run tests/errors/ch12/warning_render.ki
6.0
```

Y Core:

```console
$ build/bin/ki emit core tests/golden/ch12/match_core.ki
fn Opt() =
  ()
fn No() =
  No
fn Yes(a0) =
  Yes(a0)
fn f(p, n) =
  match p {
    (a, _) => match a {
      Yes(0 | -1) => 1
      Yes(k) if gt_int(k, n) => k
      _ => n
    }
  }
fn g(p) =
  match p {
    (Yes(x), _) | (_, Yes(x)) if gt_int(x, 0) => x
    _ => 0
  }
fn main() =
  ()
```

## Ejercicios

:::exercise
**1. SAT con dos variables.** Escribe como `match` sobre `(Bool, Bool)` la fórmula `(x ∨ y) ∧ (¬x ∨ y) ∧ (x ∨ ¬y)`, con la codificación de la Teoría. ¿Qué testigo da `ki check`, y qué dice de la fórmula? Añade la cláusula `(¬x ∨ ¬y)` y explica el resultado.
:::

:::solution
```text
fn sat(v: (Bool, Bool)) -> Int {
  match v {
    (false, false) => 1
    (true, false) => 2
    (false, true) => 3
  }
}
fn main() {}
```

Cada brazo casa las asignaciones que hacen falsa su cláusula: `(x ∨ y)` es falsa con `x = false, y = false`. `ki check` da E0701 con «pattern `(true, true)` not covered»: el testigo es una solución, `x = y = true`. La cuarta cláusula añade el brazo `(true, true) => 4`, y el `match` pasa sin diagnósticos: cubrir todas las asignaciones es que ninguna satisface la fórmula.
:::

:::exercise
**2. Un testigo recursivo.** Con `type Bools { Nil, Cons(Bool, Bools) }`, escribe un `match` con los brazos `Nil`, `Cons(true, _)` y `Cons(false, Nil)`. Predice el testigo antes de ejecutar `ki check`.
:::

:::solution
```text
type Bools { Nil, Cons(Bool, Bools) }
fn f(l: Bools) -> Int {
  match l {
    Nil => 0
    Cons(true, _) => 1
    Cons(false, Nil) => 2
  }
}
fn main() {}
```

E0701 con «pattern `Cons(false, Cons(_, _))` not covered». `{Nil, Cons}` es completo; dentro de `Cons`, `{true, false}` también, y bajo `false` la cola solo tiene `Nil`, que no es completo: falta `Cons`, con comodines. El testigo baja tanto como los patrones, no más.
:::

:::exercise
**3. Rompe la cobertura con una guarda.** Escribe `fn sign(n: Int) -> Int` con dos brazos, `x if x >= 0` y `x if x < 0`. Tú sabes que cubren todo `Int`; ¿qué dice `ki check`, y por qué? Arréglalo sin perder la intención.
:::

:::solution
```text
fn sign(n: Int) -> Int {
  match n {
    x if x >= 0 => 1
    x if x < 0 => -1
  }
}
fn main() { print(sign(-5)) }
```

E0701 con «pattern `_` not covered»: los dos brazos tienen guarda, así que ninguno es una fila de la matriz, y el testigo de una matriz vacía es `_`. Que las dos guardas sumen todo `Int` es aritmética que el compilador no hace. El arreglo es cambiar el último brazo por `_ => -1`: `ki check` no dice nada y `ki run` imprime `-1`.
:::

:::exercise
**4. Inalcanzable por la unión.** Escribe un `match` sobre `(Bool, Bool)` con un brazo que W0701 marque aunque ninguno de los de arriba lo cubra solo. ¿Qué imprime `ki run` con `(true, true)`?
:::

:::solution
```text
fn f(p: (Bool, Bool)) -> Int {
  match p {
    (true, false) => 1
    (true, true) => 2
    (true, _) => 3
    _ => 4
  }
}
fn main() { print(f((true, true))) }
```

W0701 marca `(true, _)`, con la edición que borra el brazo, y `ki check` sale con 0. Ningún brazo de arriba lo cubre solo, pero los dos juntos sí: la pregunta es `U(P₁…ᵢ₋₁, (pᵢ))`, contra todas las filas de arriba a la vez. `ki run` imprime `2`: gana el primer brazo que casa. `_ => 4` sigue siendo alcanzable con `(false, false)`.
:::
