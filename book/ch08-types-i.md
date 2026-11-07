---
chapter: 8
title: Tipos I: comprobar contra firmas, con errores legibles
part: I
hours: 9
milestone: errores de tipo con dos etiquetas y su origen
---

## Objetivo

Al terminar, `ki check` sabe qué tipo tiene cada expresión. Cuando dos no encajan, lo dice con dos etiquetas: el sitio que está mal y el que explica por qué se esperaba otra cosa. Aquí, `true` no es un `Int`:

```ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  print(fib(true))
}
```

La primaria señala `true`; la secundaria, el parámetro `n: Int` que lo pedía. Esa secundaria es el hito, camino de M1 (capítulo 10). Nacen E0401 a E0406 y un objetivo: una equivocación, un error.

## Teoría

### Los tipos como datos: una tabla que solo crece

Un tipo es un dato más: `Int`, `Bool`, `()`, una función o una variable por resolver. `internal/sema` los guarda en un `base.IdVec[TypeID, Type]` (capítulo 4), y un tipo nombra a otros por id, nunca por puntero.

**Hash-consing.** La tabla construye cada tipo una vez: `Fun` busca la estructura de una función en un map antes de crear un nodo. Así, comparar tipos sin variables ligadas es comparar ids:

```math
Fun(ps, r, ε) = Fun(ps′, r′, ε′)   ⇔   (ps, r, ε) = (ps′, r′, ε′)
τ, τ′ sin variables ligadas:   τ = τ′   ⇔   id(τ) = id(τ′)
```

**Inmutabilidad por convención.** Nadie debe cambiar lo compartido, pero Go no tiene valores `const` compuestos ni un `&` de solo lectura. Así que los tipos son ids, la tabla solo crece y `Fun` copia los parámetros, por si quien llama reutiliza su slice.

No es una garantía: quien tenga el nodo puede convertir `(Int) -> Int` en `(Bool) -> Int`.

:::from Rust
Una clave con `Vec<TypeId>` compila, y `&[TypeId]` garantiza lo que aquí nadie garantiza.
:::

:::more Embedding: ya lo usa el capítulo 9
Un struct puede llevar otro sin nombre de campo (capítulo 3). Los campos y métodos del de dentro se usan como propios, promocionados, y el campo entero se nombra por su tipo.

No es herencia: quien pide el de dentro necesita ese campo.

Lo verás en `Compiled`, con un `*Program`; el capítulo 14 lo desarrolla.
:::

### Union-find sobre slices

Una variable de tipo se liga a otro tipo, que puede ser otra variable. Esos enlaces forman cadenas: un bosque *union-find*, sin punteros, donde el enlace al padre es un id.

```pseudo
función liga(v, τ):                       // une, sin comprobar nada
    enlace(v) ← τ;  v queda ligada

función resuelve(t):                      // busca
    fin ← t
    mientras fin es una variable ligada:  fin ← enlace(fin)
    mientras t ≠ fin:                     // compresión de caminos
        siguiente ← enlace(t);  enlace(t) ← fin;  t ← siguiente
    devuelve fin
```

El segundo bucle de `resuelve`, la **compresión de caminos**, liga cada variable del camino al final: la siguiente consulta da un solo paso.

### Unificación de Robinson y el occurs check

Comprobar es plantear ecuaciones: en `fn double(x) -> Int { x * 2 }`, `x` tiene que ser `Int`. **Unificar** dos tipos (`x ~ y`) es hacerlos iguales ligando variables. El algoritmo de Robinson mira sus cabezas (el nodo de fuera), ya resueltas:

```pseudo
función unifica(esperado, hallado, razón):
    pendientes ← [(esperado, hallado)];  vistos ← ∅
    mientras haya pendientes:
        saca (e, f);  e ← resuelve(e);  f ← resuelve(f)
        si e = f entonces pasa al siguiente                          // 1
        si no, si e o f es {error} entonces pasa al siguiente        // 2: sin ligar nada
        si no, si e es una variable v entonces liga_si_cabe(v, f)    // 3
        si no, si f es una variable v entonces liga_si_cabe(v, e)
        si no, si e y f son funciones con el mismo número de parámetros:  // 4
            si (e, f) ∉ vistos entonces
                vistos ← vistos ∪ {(e, f)}
                apila los resultados y cada par de parámetros, el primero arriba
        si no:                                                       // 5
            informa E0401 de (esperado, hallado) con la razón;  devuelve falso
    devuelve verdadero

función liga_si_cabe(v, τ):
    si aparece(v, τ) entonces informa E0405;  unifica devuelve falso
    si no, liga(v, τ)

función aparece(v, τ):
    pendientes ← [τ];  vistos ← ∅
    mientras haya pendientes:
        t ← resuelve(saca uno)
        si t es la variable v entonces devuelve verdadero
        si t es una función y t ∉ vistos entonces
            vistos ← vistos ∪ {t};  apila sus parámetros y su resultado
    devuelve falso
```

El tipo de error del paso 2 llega más abajo.

**El occurs check.** `a ~ (a) -> Int` no tiene solución: ningún tipo finito se contiene a sí mismo. `liga` no lo mira; `liga_si_cabe` sí, y da E0405. Por eso el paso 1 va primero: `a ~ a` daría un E0405 falso.

**Compartir también explota.** Como cada tipo existe una vez, `(t, t) -> Int`, con `t` el nivel de abajo, lleva el mismo nodo dos veces. Cuarenta niveles son cuarenta nodos y 2⁴⁰ caminos: recorrerlos tardaría horas.

Unas cuarenta líneas lo construyen con parámetros sin anotación, `p1(p0, p0)`, `p2(p1, p1)`…: la regla (App-var), más abajo.

Por eso los dos llevan `vistos`, el conjunto de lo ya visitado.

Usan pila propia, sin recursión, porque la profundidad de un tipo la decide el programa.

### Comprobación bidireccional

Hay dos juicios, *inferir* y *comprobar*, y cada regla dice cómo se obtiene uno:

```math
Γ              lo que se sabe de cada nombre: el tipo de cada variable y de cada función
Γ ⊢ e ⇒ τ      inferir: τ sale de lo que e lleva dentro
Γ ⊢ e ⇐ τ      comprobar: e recibe el τ que su sitio espera
Γ ⊢ s ⊣ Γ′     una sentencia: s deja el contexto Γ′ (un let añade x : τ)

                                                        x : τ ∈ Γ
 ──────────── (Int)      ──────────────── (Bool)      ─────────── (Var)
  Γ ⊢ n ⇒ Int             Γ ⊢ true ⇒ Bool              Γ ⊢ x ⇒ τ

  Γ ⊢ f ⇒ (τ₁, …, τₙ) → τ      Γ ⊢ eᵢ ⇐ τᵢ  (1 ≤ i ≤ n)
 ────────────────────────────────────────────────────── (App)
                 Γ ⊢ f(e₁, …, eₙ) ⇒ τ

  Γ ⊢ f ⇒ α, sin ligar      Γ ⊢ eᵢ ⇒ τᵢ      β nueva      α ~ (τ₁, …, τₙ) → β
 ────────────────────────────────────────────────────────────────────── (App-var)
                         Γ ⊢ f(e₁, …, eₙ) ⇒ β

  op : (σ, σ) → ρ      Γ ⊢ e₁ ⇒ τ₁      σ ~ τ₁      Γ ⊢ e₂ ⇒ τ₂      σ ~ τ₂
 ─────────────────────────────────────────────────────────────────── (Op)
                         Γ ⊢ e₁ op e₂ ⇒ ρ
  + − * / % : (Int, Int) → Int     < <= > >= : (Int, Int) → Bool     && || : (Bool, Bool) → Bool
  − : (Int) → Int     ! : (Bool) → Bool          el unario, igual con un operando
  el error va sobre el operando entero: un operando no recibe la expectativa

  Γ ⊢ e₁ ⇒ τ      τ ∈ {Int, Bool}      Γ ⊢ e₂ ⇒ τ′      τ ~ τ′
 ───────────────────────────────────────────────────────────── (Eq)    igual con !=
                   Γ ⊢ e₁ == e₂ ⇒ Bool
  si τ es aún una variable, se juzga al final: se rechaza solo si resultó ser una
  función o (); si sigue libre, se acepta: fn same(x, y) -> Bool { x == y }, sin llamadas

  Γ ⊢ e ⇒ τ′      τ ~ τ′
 ──────────────────────── (Sub)          el error va sobre e entero
        Γ ⊢ e ⇐ τ

  Γ ⊢ s₁ … sₙ ⊣ Γ′      Γ′ ⊢ e ⇐ τ
 ────────────────────────────────── (Bloque)
       Γ ⊢ { s₁ … sₙ e } ⇐ τ

  Γ ⊢ s₁ … sₙ ⊣ Γ′      τ ~ ()
 ──────────────────────────────── (Bloque-())    el error va sobre el bloque
       Γ ⊢ { s₁ … sₙ } ⇐ τ

  Γ ⊢ c ⇐ Bool      Γ ⊢ e₁ ⇒ τ      Γ ⊢ e₂ ⇐ τ
 ──────────────────────────────────────────────── (If)    si e₂ no cuadra, el if es {error}
        Γ ⊢ if c { e₁ } else { e₂ } ⇒ τ

  Γ ⊢ c ⇐ Bool      Γ ⊢ e₁ ⇐ ()
 ──────────────────────────────── (If-sin-else)
     Γ ⊢ if c { e₁ } ⇒ ()

  Γ ⊢ c ⇐ Bool      τ ~ ()      Γ ⊢ e₁ ⇐ ()
 ─────────────────────────────────────────── (If⇐)    si τ ~ () falla, e₁ solo se infiere
           Γ ⊢ if c { e₁ } ⇐ τ

         Γ ⊢ e ⇐ τ                                  Γ ⊢ e ⇒ τ
 ──────────────────────────── (Let:)      ──────────────────────── (Let)
  Γ ⊢ let x: τ = e ⊣ Γ, x : τ              Γ ⊢ let x = e ⊣ Γ, x : τ
```

(Bloque) pasa lo esperado hacia la cola, y las dos direcciones se encuentran en (Sub), cuando no hay hacia dónde empujar, y en los operandos. En `let x: Bool = { 1 }`, inferir y comparar pondría el error sobre las llaves; (Bloque) lo pone sobre el `1`.

Un `if` con `else` no empuja lo esperado: (If) compara sus ramas y (Sub), el resultado sobre el `if` entero. Si las ramas no cuadran, el `if` tiene el tipo de error, para que cada uso no dé otro.

Sin `else`, (If⇐) compara primero `()` con lo esperado, sobre el `if` entero.

Las funciones son un solo grupo **monomórfico**, cada una con un solo tipo:

```pseudo
función comprueba(programa):
    para cada Def f:                                  // las firmas
        un parámetro anotado tiene su tipo; uno sin anotar, una variable nueva
        sin ->, el resultado es ()
    comprueba que main es fn main()                   // E0404
    para cada Def f con cuerpo:  Γ ⊢ cuerpo(f) ⇐ resultado(f)
    juzga las igualdades aplazadas
```

Una llamada encuentra así la firma de una función definida más abajo. Lo que un cuerpo o una llamada aprende de un parámetro lo ve cada uso de la función. Por eso (Eq) aplaza una variable: una llamada posterior puede ligarla, y el veredicto no debe depender del orden. La generalización (una función con tipos distintos) llega en el capítulo 13.

### Procedencia y mensajes al estilo de rustc

La secundaria es el **origen** de la expectativa. Cada `unifica` lleva un `Reason` con ese sitio y, si falla, elige:

```math
razón                      lo que dice el E0401
anotación                  secundaria en la anotación
cola del cuerpo, retorno   secundaria en el tipo de retorno, o en el nombre si no hay ->
argumento                  secundaria en el parámetro
ramas del if               secundaria en la otra rama, o en el `if` si no hay else
condición del if           nota: la condición tiene que ser Bool
operando                   secundaria en el operador
sin secundaria si su sitio no existe o está en otro fichero
```

Una cola de función con valor donde se espera `()` lleva una ayuda: ligarlo con `let` o, si devolverlo lo arregla, devolverlo.

Una secundaria se pinta bajo el fragmento de la primaria: la del parámetro de `print`, en el preludio, no se dibuja.

Un mensaje con dos tipos usa una sola serie de letras: si cada uno empezara en `a`, parecerían hablar de la misma variable.

```math
nombre(v) = la k-ésima letra (tras la z, a1, b1…), con k el orden en que v aparece al escribir el esperado y luego el hallado
una variable que está en los dos tipos lleva la misma letra
```

### El tipo de error, que unifica en silencio

Si el comprobador inventara un tipo para un nombre desconocido, que ya dio su E0301, `nope + 1` añadiría un E0401 por el mismo descuido.

`TypeErr`, escrito `{error}`, es el tipo de lo que una fase anterior no entendió:

```math
{error} ~ τ   y   τ ~ {error}   aciertan para todo τ, sin ligar nada
Γ ⊢ e₁ ⇒ {error}  ∨  Γ ⊢ e₂ ⇒ {error}   ⟹   Γ ⊢ e₁ op e₂ ⇒ {error}
salvo:  Γ ⊢ e₁ ⇒ τ, con τ una función o ()   ⟹   Γ ⊢ e₁ == e₂ ⇒ Bool, y un E0401 sobre e₁
```

`unifica` no liga variables al error: una variable ligada al error taparía los errores de después. La última línea es la excepción de `==`, que ya ha dado su error.

### Una goroutine que vigila: `within`

Un recorrido equivocado de 40 niveles no falla, se cuelga: `go test` lo corta a los diez minutos sin decir la fila. `within.Run` le pone un plazo:

```pseudo
función dentro_de(plazo, trabajo):
    hecho ← un canal nuevo
    en otra goroutine:  trabajo();  cierra hecho
    espera lo primero que ocurra:
        hecho se cierra   → vuelve
        pasa el plazo     → el test falla
```

La goroutine colgada no se puede matar: solo se deja de esperarla.

:::from Rust
Es `thread::spawn` con un `mpsc::channel` y `rx.recv_timeout(d)`.
:::

## Preguntas de repaso

:::review
¿Por qué un `if` cuyas ramas no cuadran tiene el tipo de error, en vez del de su rama `then`?
:::

:::answer
Porque el `if` no tiene ningún tipo válido: elegir el de `then` sería apostar por una rama, y si la buena era la otra, cada uso del `if` daría un segundo error por la misma equivocación.
:::

:::review
¿Por qué `let x: Int = if true { 1 }` da su error sobre el `if` entero y no sobre el `1`?
:::

:::answer
Porque la equivocación es el `else` que falta: con él, el `1` estaría bien. Por eso (If⇐) compara primero `()` con lo esperado y, si falla, solo infiere la rama, para no perder los errores que tenga dentro. Si lo esperado es `()`, la comparación cuadra y entonces sí sobra el `1`.
:::

:::review
¿Por qué `aparece`, el occurs check, necesita un conjunto de visitados, si un tipo de la Parte I no puede ser cíclico?
:::

:::answer
Porque no hace falta un ciclo para que un recorrido en árbol explote: basta compartir. Cuarenta niveles de `(t, t) -> Int` son cuarenta nodos y 2⁴⁰ caminos. Un programa válido de unas cuarenta líneas lo construye, y el occurs check corre en cada ligadura.
:::

:::review
¿Por qué desaparece la secundaria de un argumento de `print(true)`, si el parámetro tiene un sitio?
:::

:::answer
Porque ese sitio está en `std/prelude.ki`, otro fichero, y el renderizador dibuja las secundarias bajo el fragmento de la primaria: con esa, haría panic.
:::

:::review
¿Por qué `unifica` compara los ids antes de mirar si un lado es una variable?
:::

:::answer
Porque `a ~ a` llegaría al paso 3, y el occurs check encontraría `a` dentro de `a`: un E0405 falso. Tras resolver los dos lados, ids iguales son ya el mismo tipo, y no hay nada que ligar.
:::

:::review
¿Por qué `unifica` no liga una variable al tipo de error, si este unifica con todo?
:::

:::answer
Porque desde entonces la variable sería `{error}` en cada uso, y con el error todo unifica en silencio: una equivocación distinta, más abajo, ya no daría su E0401. Sin ligarla, la variable sigue abierta y el resto del programa se comprueba.
:::

## Resumen

- Con hash-consing, comparar tipos sin variables ligadas es comparar ids.
- Sin `const` compuestos, la inmutabilidad es una convención.
- Las variables son un union-find con compresión de caminos.
- Unificar es resolver, comparar, ligar con occurs check y bajar por las partes.
- Compartir nodos multiplica los caminos: se recuerda lo visitado.
- Comprobar empuja la expectativa, cuando puede, hacia la expresión que está mal.
- Las funciones son un grupo monomórfico: firmas, luego cuerpos.
- Un `Reason` elige la secundaria, la nota o la ayuda.
- El tipo de error unifica en silencio y se propaga.
- Un plazo en otra goroutine hace de un cuelgue un fallo.

## Go en el trabajo

**Los mensajes, revisados como producto.** Los UI tests de rustc guardan la salida esperada y exigen anotar los errores en la fuente. En Ki, un golden guarda el texto entero, y otro el código y la posición de todos los errores.

**Claves compuestas.** `go/types` también reutiliza instancias genéricas idénticas, con claves `string`: dos tipos idénticos pueden ser punteros distintos, así que la clave escribe su estructura.

:::more Por qué un `string` busca más rápido
Los maps tienen caminos rápidos para dos formas de clave: 4 u 8 bytes que se comparan como memoria, y strings. Cualquier otra va por el camino general, que llama a las funciones de hash e igualdad del tipo.

Una medida, con 1 000 claves ya construidas: buscar con la forma de `funKey` (dos `uint32` y un `string`) costó unos 14,3 ns, y con todo en un `string`, unos 8,7. `funKey` se queda como struct por legibilidad: su coste está en construir la clave (ejercicio 4).
:::

## Tests primero

{{cards}}

Escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega su referencia.

Las tarjetas describen el final del capítulo. Las que cambian después se escriben, en su sesión, como en `git show ref/ch08-sK:book/tests/ch08.tests`.

:::more Los ayudantes de test
Los tests de referencia y las soluciones de los ejercicios cuentan con ellos; están en `ref/ch08-sK`.

- **`unify_internal_test.go`:** `newUnifyEnv` da una tabla con los tres tipos predefinidos; `fn(ret, params...)` y `fresh` crean funciones y variables; `unify` llama a `Unify` con un sink propio; `annotated` es la razón de un `let` anotado.
- **`check_test.go`:** pasa cada fuente por `testsupport.Analyze` y reutiliza `spanIn`, de `scope_test.go`. `wantDiags` exige exactamente esos diagnósticos; `mismatch` y `secondary` construyen los esperados.
- **`export_internal_test.go`:** `VarOrigin` va aquí porque lo exportado en un `_test.go` de `package sema` lo ve `sema_test`, y la API no cambia.
- **`within`** es un paquete aparte: `package sema` no puede importar `testsupport`.
:::

## Implementación

### Sesión 1: la tabla de tipos (≈ 2 h)

#### Go: una clave de map compuesta

`Fun` busca en un map con clave `funKey`. Una clave tiene que admitir `==`, y un slice no. Así que `funKey` lleva los parámetros en un `string`, cuatro bytes por id: en decimal y pegados, `[1, 12]` y `[11, 2]` darían los dos `"112"`.

`binary.LittleEndian.PutUint32`, de `encoding/binary`, pone un `uint32` en cuatro bytes; `word[:]` es un slice sobre el array entero.

#### Go: goroutines, canales y `select`

`within.Run` es `dentro_de`, de la Teoría: ejecuta el trabajo en otra goroutine y espera con un `select`. En pequeño:

```go
func within(t *testing.T, d time.Duration, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("not finished after %v", d)
	}
}
```

- `go f()` lanza `f` en una goroutine nueva.
- `make(chan struct{})` crea un canal; `close(done)` lo cierra, y recibir de un canal cerrado (`<-done`) no espera.
- `time.Duration` cuenta nanosegundos; `time.After(d)` devuelve un canal que recibe un valor pasado `d`.
- `select` espera al primero de sus casos que pueda avanzar.
- `t.Fatalf` va en la goroutine del test: dentro del `go func()`, `go vet` lo rechaza.

#### Go: `struct{}` como conjunto

El `vistos` de `Occurs` es un `map[TypeID]struct{}`: solo importa la clave. El valor, `struct{}{}`, mide cero bytes, pero en Go 1.27 su hueco en el map ocupa lo mismo que un `bool`.

#### Los tests, en rojo

1. **Escribe T08.01–T08.09.** La interfaz, completa:

```go
type TypeID uint32; type RowID uint32; type TypeVarID uint32
type Builtin uint8   // BuiltinInt, BuiltinBool, BuiltinUnit
type TypeKind uint8  // TypeBuiltin, TypeFun, TypeVariable, TypeErr
type Type struct { Kind TypeKind; Builtin Builtin; Params []TypeID; Ret TypeID; Effects RowID; Var TypeVarID }
type Row struct { Labels []TypeID; Tail TypeVarID; HasTail bool } // Part I: only the empty row
type VarKind uint8   // VarKindType, VarKindRow
type Origin struct { Span base.Span; Note string }
type TypeVar struct { Bound TypeID; IsBound bool; Rank uint8; Level uint32; Kind VarKind; Origin Origin }
type funKey struct { ret TypeID; effects RowID; params string }
type TypeTable struct { types base.IdVec[TypeID, Type]; vars base.IdVec[TypeVarID, TypeVar]; rows base.IdVec[RowID, Row]; funs map[funKey]TypeID; … }
func NewTypeTable() *TypeTable
func (t *TypeTable) Builtin(b Builtin) TypeID
func (t *TypeTable) Error() TypeID
func (t *TypeTable) EmptyRow() RowID
func (t *TypeTable) Fun(params []TypeID, ret TypeID, effects RowID) TypeID
func (t *TypeTable) FreshVar(origin Origin) TypeID
func (t *TypeTable) Node(id TypeID) Type
func (t *TypeTable) Resolve(id TypeID) TypeID
func (t *TypeTable) Bind(v TypeVarID, to TypeID)
func (t *TypeTable) Occurs(v TypeVarID, in TypeID) bool
func (t *TypeTable) Show(id TypeID) string
const MaxTypeText = 256 // Show cuts a longer text and ends it in `…`
// package within (internal/testsupport/within), copied from ref/ch08-s1
const Deadline = 10 * time.Second
func Run(t *testing.T, what string, work func())
```

2. **Copia `internal/testsupport/within`** de `ref/ch08-s1`.
3. **Míralo fallar.** Sobre `ch07-s4`, el test no compila: `undefined: TypeTable`.
4. **Escribe las declaraciones, `MaxTypeText` y métodos que devuelvan el valor cero** (`Resolve` devuelve su `id`, `Show` devuelve `""`). Una fila dice qué efectos puede hacer una función: `NewTypeTable` crea la vacía (`t.rows.Push(Row{})`), la única de la Parte I. Sin ella, el test de `Fun` hace panic. Fallan los nueve subtests, sin panic.

#### El código

{{file ch08-s1 internal/sema/types.go}}

`Resolve`, `Bind` y `Occurs` son `resuelve`, `liga` y `aparece`, de la Teoría.

{{reftest T08.08}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s1`; compárate con `ref/ch08-s1`.

### Sesión 2: unificación con procedencia (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T08.10–T08.19** (T08.17, como en `ref/ch08-s2`). La interfaz:

```go
type ReasonKind uint8 // ReasonAnnotation, ReasonLetAnnotation, ReasonReturnType, ReasonBlockTail,
                      // ReasonArgument, ReasonIfBranches, ReasonIfCondition, ReasonOperand
type Reason struct { Kind ReasonKind; Because base.Span; Index int; Detail string; CanReturn bool }
type Unifier struct { Types *TypeTable; Sink *diag.Sink }
func (u *Unifier) Unify(expected, found TypeID, foundAt base.Span, r Reason) bool
func (t *TypeTable) showAll(ids ...TypeID) []string // one set of names for all of them
```

2. **Míralos fallar.** Sobre `ch08-s1` no compila: `undefined: Reason`.
3. **Copia `reason.go` entero y escribe un `Unify` que devuelva `true`.** Fallan nueve de los diez subtests, sin panic; el de dos tipos iguales ya pasa.

#### El código

{{file ch08-s2 internal/sema/reason.go}}

**El catálogo antes que el comprobador.** Su test exige una entrada por cada código de la fuente, y al revés. `unify.go` trae E0401 y E0405. Pero nadie llama aún a `Unify`: sus ejemplos erróneos no pueden dar su código.

Con la palabra de valla `pending=<valor>`, el ejemplo se ejecuta y el test **falla** el día que da su código.

El valor dice quién hará alcanzable el código: en tu repo, `pending=ch08-s3` (`T36` en la referencia, nombre antiguo de la sesión).

1. **Escribe T08.20.**
2. **Cambia `parseDoc` y `exampleProblems`** (T07.86, T07.87: `git show ref/ch08-s2:book/tests/ch07.tests`).
3. **Escribe `E0405.md` y `E0401.md`,** este con siete pares, uno por sitio: anotación de `let`, tipo de retorno, cola `()`, argumento, otra rama, condición y operando.
4. **Escribe `unify.go`.**

{{diff ch08-s1..ch08-s2 tests/errors_docs_test.go}}

{{file ch08-s2 docs/errors/E0405.md}}

{{file ch08-s2 internal/sema/unify.go}}

`showAll`:

{{diff ch08-s1..ch08-s2 internal/sema/types.go}}

{{reftest T08.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s2`; compárate con `ref/ch08-s2`.

### Sesión 3: el comprobador bidireccional (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T08.21–T08.37** (T08.23, como en `ref/ch08-s3`) **y pon T08.17 como en su tarjeta.** La interfaz:

```go
// package sema
type TypedModule struct { ExprTypes []TypeID } // indexed by ExprID
type TypedProgram struct { Types *TypeTable; Modules []TypedModule; DefTypes base.IdVec[DefID, TypeID]; LocalTypes base.IdVec[LocalID, TypeID] }
func Check(mods []*syntax.Module, res *ResolvedProgram, in *base.Interner, sink *diag.Sink) *TypedProgram
// package driver
type Frontend struct { …; Resolved *sema.ResolvedProgram; Typed *sema.TypedProgram }
// package syntax: Expr.OpSpan of an ExprCall is the argument list, from `(` to `)`
// package testsupport (test-only code)
func Resolve(t *testing.T, source string) *Program // names only, no types
func DefType(t *testing.T, p *Program, name string) string
// package sema, export_internal_test.go (compiled only for tests)
func VarOrigin(t *TypeTable, v TypeVarID) Origin
```

2. **Copia `testsupport` entero:**

{{diff ch08-s2..ch08-s3 internal/testsupport/compile.go}}

`Analyze` comprobará tipos, y algunos tests del capítulo 7 analizan fuentes que no tipan. **Pasa a `testsupport.Resolve`** las cinco llamadas que enseña `git diff ref/ch08-s2 ref/ch08-s3 -- internal/sema/scope_test.go internal/sema/resolve_test.go`.

3. **Míralos fallar.** Sobre `ch08-s2` no compila: `p.Frontend.Typed undefined`.
4. **Añade el campo, haz que `Session.Analyze` llame a `Check` y escribe un `Check`** que cree las tablas con el tipo de error en cada entrada. Fallan los dieciocho tests, sin panic.

#### El código

{{file ch08-s3 internal/sema/check.go}}

`Check` es `comprueba`, de la Teoría:

- **`inferIf`** es (If-sin-else) o, con `else`, (If) vía `elseBranch`; **`checkIf`**, (If⇐) o (If) y (Sub).
- **`call`** es (App) y (App-var). Otra aridad es E0402 sobre la lista de argumentos; lo que no es función, E0403.
- **`equality`** es (Eq), y `judgeEqualities`, su juicio aplazado.

`Unify` omite la secundaria en otro fichero:

{{diff ch08-s2..ch08-s3 internal/sema/unify.go}}

La lista de argumentos, en el parser:

{{diff ch08-s2..ch08-s3 internal/syntax/parser_expr.go}}

{{diff ch08-s2..ch08-s3 internal/driver/pipeline.go}}

**El catálogo, al día.** Con `Check` en `Analyze`, su test falla por las ocho `pending=` y pide `E0402.md` y `E0403.md`. Los ejemplos corregidos de E0201 y E0203 ya no están limpios.

1. **Quítalas** y los dos párrafos que decían que `ki check` no comprobaba tipos; T07.87 cambia con ellas (`git show ref/ch08-s3:book/tests/ch07.tests`).
2. **Escribe las dos entradas.** `E0403.md` sigue el molde de `E0402.md`.
3. **Cambia esos ejemplos.** `fn main() { 1 + 2 }` devuelve un `Int` donde se espera `()`, y `print` no admite un `Bool`. Quedan `let sum = 1 + 2` y `let ok = 1 < 2 && 2 < 3`; los erróneos, `let sum = 1 +` y `let ok = 1 < 2 < 3`.
4. **Añade a `E0401.md` dos pares,** los de `==` entre funciones.

{{diff ch08-s2..ch08-s3 docs/errors/E0405.md}}

{{file ch08-s3 docs/errors/E0402.md}}

{{reftest T08.28}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s3`; compárate con `ref/ch08-s3`.

### Sesión 4: errores legibles, sin cascadas, y `main` (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T08.38–T08.48 y pon T08.23 como en su tarjeta.** La interfaz:

```go
// package syntax
type Item struct { …; ParamList, RetClause base.Span; ParamListOneLine, RetClauseOneLine bool }
func (p *parser) oneLine(from int) bool
func (p *parser) blanksBefore(hi uint32) uint32
// package testsupport (test-only code)
func ApplyFixits(src string, d diag.Diagnostic) string
```

Copia `fixits.go`, que aplica las ediciones:

{{file ch08-s4 internal/testsupport/fixits.go}}

2. **Añade a `analyze_fuzz_test.go` las tres semillas de T07.94** (`git show ref/ch08-s4:book/tests/ch07.tests`).
3. **Míralos fallar.** Sobre `ch08-s3` no compila: `it.ParamList undefined`.
4. **Añade los cuatro campos, sin rellenarlos.** Fallan los tests de `main` (también en T08.23), E0406, cascadas tras un error de sintaxis, spans de la firma y el sink lleno. El de E0301 y los cuatro golden ya pasan: la sesión 3 cumple lo que fijan.

Un golden de `tests/errors/ch08`:

{{file ch08-s4 tests/errors/ch08/if_branches_render.ki}}

La cabecera lleva el error a la línea 12, y las dos cifras ensanchan el margen: una anotación `// ERROR` no lo vería.

#### El código

- **E0404 y el parser.** `main` tiene que ser `fn main()`, y quitar lo que sobra necesita dos spans. Los guarda el parser: el comprobador no tiene el texto, y buscar el `(` fallaría con `fn main /* ( */ (x)`, que es válido.
- **Cascadas tras un error de sintaxis.** Un `if` sin `else` con la rama ilegible tiene el tipo de error. Mientras falte un argumento, `call` no juzga la cuenta: `g(1,,2)` daría un E0402 que se iría al borrar la coma. Lo decide `c.missingArg`, un valor de método.
- **El sink lleno.** Si ya perdió un error (`Truncated`), no se escriben los tipos de los siguientes: un tipo puede tener miles de parámetros.

:::more Medir lo que se reserva
El test del sink lleno mide bytes reservados, más estables que el reloj. `runtime.ReadMemStats(&m)` rellena un `runtime.MemStats`, y `TotalAlloc`, que solo crece, se resta entre dos lecturas.
:::

{{diff ch08-s3..ch08-s4 internal/syntax/ast.go}}

{{diff ch08-s3..ch08-s4 internal/syntax/parser.go}}

{{diff ch08-s3..ch08-s4 internal/sema/check.go}}

{{diff ch08-s3..ch08-s4 internal/sema/unify.go}}

**Las entradas.** Escribe `E0404.md` y `E0406.md`, cambia `E0402.md` y añade a `E0401.md` el par de un valor sin usar en `main`. E0406 es un nombre de tipo desconocido (desde ahí, tipo de error), con dos pares.

{{file ch08-s4 docs/errors/E0404.md}}

{{diff ch08-s3..ch08-s4 docs/errors/E0402.md}}

{{reftest T08.38}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s4`; compárate con `ref/ch08-s4`.

## Ejecútalo

Con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el programa del objetivo da el error del hito y sale con 1:

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  print(fib(true))
}
$ build/bin/ki check fib.ki
error[E0401]: mismatched types
 --> fib.ki:6:13
  |
1 | fn fib(n: Int) -> Int {
  |        ------ the parameter is declared here
  ...
6 |   print(fib(true))
  |             ^^^^ expected `Int`, found `Bool`
```

Cuatro equivocaciones:

```console
$ cat bad.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main(args: Int) {
  let big = fib(20) > 100
  print(if big { 1 } else { false })
  let total: Bool = fib(10) + nope
  let pair = fib(1, 2)
  print(pair)
}
$ build/bin/ki check bad.ki
error[E0404]: `main` must be `fn main()`
 --> bad.ki:5:4
  |
5 | fn main(args: Int) {
  |    ^^^^ declared here with 1 parameter(s)
  = note: it takes no parameters and returns `()`
  = help: remove the parameters
  |
5 - fn main(args: Int) {
5 + fn main() {

error[E0401]: mismatched types
 --> bad.ki:7:29
  |
7 |   print(if big { 1 } else { false })
  |                             ^^^^^ expected `Int`, found `Bool`
  |                  - the other branch has type `Int`

error[E0301]: unknown name `nope`
 --> bad.ki:8:31
  |
8 |   let total: Bool = fib(10) + nope
  |                               ^^^^ not found in this scope

error[E0402]: this function takes 1 argument(s) but 2 were supplied
 --> bad.ki:9:17
  |
1 | fn fib(n: Int) -> Int {
  |    --- `fib` is declared here
  ...
9 |   let pair = fib(1, 2)
  |                 ^^^^^^ supplied here
  = note: `fib` takes `(Int)`
$ echo $?
1
```

La línea 8 da un solo error: la suma tiene tipo error y la anotación no se queja. Un tipo infinito, y el formato corto:

```console
$ cat inf.ki
fn twice(f) -> Int { f(f) }

fn main() {
  print(twice(1))
}
$ build/bin/ki check inf.ki
error[E0405]: cannot construct an infinite type
 --> inf.ki:1:22
  |
1 | fn twice(f) -> Int { f(f) }
  |                      ^ this type would contain itself
  = note: `a` would have to be `(a) -> b`, which has no finite size
$ build/bin/ki check --diagnostics=short bad.ki
bad.ki:5:4: error[E0404]: `main` must be `fn main()`
bad.ki:7:29: error[E0401]: mismatched types
bad.ki:8:31: error[E0301]: unknown name `nope`
bad.ki:9:17: error[E0402]: this function takes 1 argument(s) but 2 were supplied
```

## Ejercicios

:::exercise
**1. Un `if` sin `else` usado como valor.** Haz que `let x: Int = if true { 1 }` lleve la nota «this `if` has no `else`, so it is `()`», con test y golden.
:::

:::solution
`Unify` informa directamente al sink, así que `checkIf` no puede añadir nada después. Dale un sink propio y reenvía lo que reciba, con la nota:

```go
	if !e.HasElse {
		// The E0401 goes to a sink of its own first, so that it can say why
		// the `if` is `()` before it is reported. Once c.sink has dropped an
		// error nothing reaches it: a new sink is never Truncated, and mismatch
		// would write the types for an error nobody sees.
		var own diag.Sink
		u := Unifier{Types: c.tp.Types, Sink: &own}
		if c.sink.Truncated() {
			u.Sink = c.sink
		}
		if !u.Unify(expected, c.unitT, e.Span, r) {
			for _, d := range own.All() {
				c.sink.Report(d.WithNote("this `if` has no `else`, so it is `()`"))
			}
			c.infer(e.Then)
			return c.unitT, false
		}
```

Declara en `check_test.go` una constante `noElse` con el texto de la nota y añade `.WithNote(noElse)` al E0401 esperado de las cuatro filas que ahora fallan.

El golden, `tests/errors/ch08/if_without_else_value.ki`, lleva un id nuevo, T08.49:

```text
// test: T08.49
// run: check
// expect-exit: 1
// expect-stderr:
// | error[E0401]: mismatched types
// |   --> tests/errors/ch08/if_without_else_value.ki:13:16
// |    |
// | 13 |   let x: Int = if true { 1 }
// |    |                ^^^^^^^^^^^^^ expected `Int`, found `()`
// |    |          --- expected because of this annotation
// |    = note: this `if` has no `else`, so it is `()`
fn main() {
  let x: Int = if true { 1 }
  print(x)
}
```

Escríbelo con las tres primeras líneas y `build/bin/ki test --update tests/errors/ch08/if_without_else_value.ki` dos veces, como en el capítulo 7: la primera graba el bloque y empuja el programa hacia abajo, la segunda la línea 13. Con la solución, `go test ./...` y `build/bin/ki test --root . tests` quedan en verde.
:::

:::exercise
**2. `a`, `b`, `c`, estables en un mensaje.** Comprueba con un test que un mensaje nombra sus variables por orden de aparición, no de creación, y mira qué mata.
:::

:::solution
En `check_test.go`:

```go
	t.Run("names_stable_in_one_message", func(t *testing.T) {
		// The same message whichever function was checked first: the names
		// follow the text of the label, not the order of the variables.
		for _, src := range []string{
			"fn one(x) -> Int { 1 }\nfn two(x, y) -> Int { 1 }\nfn main() { let f = if true { one } else { two } }",
			"fn two(x, y) -> Int { 1 }\nfn one(x) -> Int { 1 }\nfn main() { let f = if true { one } else { two } }",
		} {
			p := testsupport.Analyze(t, src)
			wantDiags(t, src, p, secondary(
				mismatch(spanIn(t, p, src, "else { two", "two"), "expected `(a) -> Int`, found `(b, c) -> Int`"),
				spanIn(t, p, src, "{ one }", "one"), "the other branch has type `(a) -> Int`"))
		}
	})
```

Pasa sobre `ch08-s4`. Si el `mismatch` de `unify.go` llamara a `Show` dos veces en lugar de a `showAll`, el primer programa daría `found (a, b) -> Int`; si `show` nombrara las variables por su id, el segundo daría `expected (c) -> Int, found (a, b) -> Int`, porque las variables de `two` se crean antes.
:::

:::exercise
**3. `Unify` es simétrico cuando tiene éxito.** Comprueba con un test que `Unify(x, y)` y `Unify(y, x)` aciertan o fallan juntos y, si aciertan, dejan los mismos tipos.
:::

:::solution
En `unify_internal_test.go`, con una tabla nueva para cada orden, para que los dos partan de tipos sin tocar:

```go
	t.Run("symmetric_on_success", func(t *testing.T) {
		type shape struct {
			name  string
			build func(e *unifyEnv, a, b TypeID) TypeID
		}
		shapes := []shape{
			{"Int", func(e *unifyEnv, a, b TypeID) TypeID { return e.intT }},
			{"Bool", func(e *unifyEnv, a, b TypeID) TypeID { return e.boolT }},
			{"()", func(e *unifyEnv, a, b TypeID) TypeID { return e.unitT }},
			{"{error}", func(e *unifyEnv, a, b TypeID) TypeID { return e.tbl.Error() }},
			{"a", func(e *unifyEnv, a, b TypeID) TypeID { return a }},
			{"(a) -> Int", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(e.intT, a) }},
			{"(Int) -> b", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(b, e.intT) }},
			{"(a, a) -> b", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(b, a, a) }},
			{"(Bool, b) -> Bool", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(e.boolT, e.boolT, b) }},
			{"() -> a", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(a) }},
			{"((a) -> Int) -> b", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(b, e.fn(e.intT, a)) }},
		}
		for _, x := range shapes {
			for _, y := range shapes {
				var ok [2]bool
				var shown [2][]string
				for i := range 2 {
					e := newUnifyEnv()
					tx := x.build(e, e.fresh(), e.fresh()) // x has two variables
					ty := y.build(e, e.fresh(), e.fresh()) // and y two others
					if i == 0 {
						ok[i], _ = e.unify(tx, ty, annotated)
					} else {
						ok[i], _ = e.unify(ty, tx, annotated)
					}
					shown[i] = e.tbl.showAll(tx, ty)
				}
				row := x.name + " ~ " + y.name
				switch {
				case ok[0] != ok[1]:
					t.Errorf("%s: Unify gave %v one way and %v the other", row, ok[0], ok[1])
				case ok[0] && !slices.Equal(shown[0], shown[1]):
					t.Errorf("%s: the types are %q one way and %q the other", row, shown[0], shown[1])
				case ok[0] && shown[0][0] != shown[0][1] && x.name != "{error}" && y.name != "{error}":
					t.Errorf("%s: after Unify the two sides are %q, want one type", row, shown[0])
				}
			}
		}
	})
```

`showAll` da a los dos lados una sola numeración, así que la comparación no depende de qué variable se ligó a cuál. El tipo de error unifica sin hacer iguales los dos lados, y por eso la última condición lo exceptúa.

Pasa sobre `ch08-s4`. Si quitas el caso `fn.Kind == TypeVariable` de `Unify`, falla con `Int ~ a: Unify gave false one way and true the other`.
:::

:::exercise
**4. Lo que vale el hash-consing.** Mide `Fun` con y sin hash-consing sobre 10⁵ firmas.
:::

:::solution
En `types_internal_test.go`, «sin hash-consing» es un `Fun` que siempre añade un nodo:

```go
// newBenchTable returns a table with the three builtin types and seven
// variables, and those ten types. Every call gives the same ids.
func newBenchTable() (*TypeTable, []TypeID) {
	tbl := NewTypeTable()
	ts := []TypeID{tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit)}
	for range 7 {
		ts = append(ts, tbl.FreshVar(Origin{}))
	}
	return tbl, ts
}

// funNoCons is Fun without the map: every call pushes a node.
func (t *TypeTable) funNoCons(params []TypeID, ret TypeID, effects RowID) TypeID {
	return t.types.Push(Type{Kind: TypeFun, Params: slices.Clone(params), Ret: ret, Effects: effects})
}

func BenchmarkFun(b *testing.B) {
	// 10^5 signatures (two parameters and a result) of which 1000 are
	// different, as a program asks for the same few types again and again.
	const n, distinct = 100_000, 1000
	_, ts := newBenchTable()
	sigs := make([][3]TypeID, n)
	for i := range sigs {
		k := i % distinct
		sigs[i] = [3]TypeID{ts[k%10], ts[k/10%10], ts[k/100%10]}
	}
	for _, consed := range []bool{true, false} {
		name := "no_hash_cons"
		if consed {
			name = "hash_consed"
		}
		b.Run(name, func(b *testing.B) {
			var nodes int
			for b.Loop() {
				tbl, _ := newBenchTable()
				for _, s := range sigs {
					if consed {
						tbl.Fun(s[:2], s[2], tbl.EmptyRow())
					} else {
						tbl.funNoCons(s[:2], s[2], tbl.EmptyRow())
					}
				}
				nodes = tbl.types.Len()
			}
			b.ReportMetric(float64(nodes), "nodes")
		})
	}
}
```

`b.ReportMetric` añade una columna a la salida.

Una medida, con `go test ./internal/sema -run '^$' -bench BenchmarkFun -benchmem`: con hash-consing, unos 4,3 ms, 1,09 MB y 1 011 nodos por cada 10⁵ llamadas; sin él, unos 8 ms, 28,6 MB y 100 011 nodos.

Las reservas casi empatan (101 041 frente a 100 038): con hash-consing, casi todas son la clave, el `string` que `encodeIDs` construye en cada llamada, también cuando la función ya existe.
:::

:::exercise
**5. La clave de `Fun`.** ¿Valdría como `params` de `funKey` escribir los ids en decimal separados por comas (`"1,12"`)? ¿Por qué cuatro bytes por id?
:::

:::solution
Valdría: con la coma, `[1, 12]` y `[11, 2]` dan `"1,12"` y `"11,2"`. Pero hay que convertir con `strconv`, y con dos ids de cuatro cifras la clave ya es más larga. Cuatro bytes fijos no necesitan separador: el ancho dice dónde acaba cada id.
:::

:::exercise
**6. `pending=`, que caduca.** ¿Por qué `pending=` hace fallar el test cuando el ejemplo ya da su código, en vez de no ejecutarlo?
:::

:::solution
Porque una palabra que no molesta se queda para siempre: el ejemplo seguiría sin comprobarse aunque el código ya existiera. Al fallar en cuanto el comprobador funciona, la sesión que lo hace alcanzable tiene que quitarla, y el ejemplo pasa a la regla normal.
:::
