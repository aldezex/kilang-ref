---
chapter: 11
title: Datos: tipos algebraicos, records y tuplas
part: II
hours: 11
milestone: valores `Shape`, records con update, `Float` impreso con la regla de `kirt/fmtnum` (`1.0`, `0.1`, `1000000.0`, `1e21`, `NaN`)
---

## Objetivo

Ki gana datos: sumas, records, alias, tuplas y `Float`. Al terminar, esto corre:

```console
$ cat shape.ki
type Shape {
  Circle(r: Float)
  Rect(w: Float, h: Float)
}
type Point { x: Float, y: Float }
fn main() {
  let s = Rect(2.0, 0.5)
  let p = Point { x: 1.0, y: 0.1 }
  let q = Point { y: 1e21, ..p }
  printFloat(q.x)
  printFloat(q.y)
  printFloat(1000000.0)
}
$ build/bin/ki run shape.ki
1.0
1e21
1000000.0
$ build/bin/ki emit core shape.ki
fn Shape() =
  ()
fn Circle(r) =
  Circle(r)
fn Rect(w, h) =
  Rect(w, h)
fn Point() =
  ()
fn main() =
  let s = Rect(2.0, 0.5);
  let p = Point(1.0, 0.1);
  let q = let y = 1e21;
    let base = p;
    Point(base.0, y);
  let _ = printFloat(q.0);
  let _1 = printFloat(q.1);
  printFloat(1000000.0)
```

`fn Shape() = ()` es un relleno (sesión 2).

## Teoría

### Tipos nominales: suma, record y alias

Un `type` tiene tres formas; las distingue el `=` tras el nombre o el primer token tras la `{`:

- **Suma:** variantes, cada una con su constructor: `Circle(r: Float)`, `None`.
- **Record:** campos con nombre: `{ x: Float, y: Float }`.
- **Alias:** otro nombre para un tipo: `type Meters = Float`.

Sumas y records son **nominales**: con `type A { x: Int }` y `type B { x: Int }`, pasar un `A` donde va un `B` es E0401. Un alias no crea un tipo: `Meters` es `Float`.

:::from Rust
Son `enum`, `struct` y `type Meters = f64;`, y dos `struct` iguales tampoco se mezclan.
:::

:::from TS
TS es estructural: dos tipos con la misma forma se intercambian. Ki mira el nombre.
:::

### Un constructor es una función; un record, no

`Circle` es una función `(Float) -> Shape`: se llama o se pasa como valor. Una variante sin campos, como `None`, ya es un valor.

Un record no tiene constructor: su literal nombra cada campo, en cualquier orden, así que no hay posición que llamar, y `Point` en una expresión es E0306. Con *punning*, `Point { x, y }` abrevia `Point { x: x, y: y }`.

Una tupla, `(1, true)` de tipo `(Int, Bool)`, se lee por posición: `t.0`. En Core todo dato es un `Con`, el constructor con los valores de sus campos en orden, y leer un campo es un `Proj` por posición.

Los tipos tienen su espacio de nombres, y los constructores, el de los valores: un tipo y una variante pueden llamarse igual.

### Hash-consing de los tipos declarados

`TypeData` se construye una vez, como los tipos función del capítulo 8, con la declaración y sus argumentos de tipo (ninguno hasta los genéricos) como clave: dos usos de `Shape` dan un id, y dos declaraciones iguales, dos.

### El pase `declare`

`declare`, un pase nuevo entre `resolve` y `check`, hace la tabla de tipos:

- **No va en `resolve`:** hace tipos, no nombres, y lee cada campo con `LookupType` sobre los nombres que `resolve` ya terminó.
- **Va antes de `check`:** las anotaciones nombran alias, y el comprobador necesita el tipo de cada constructor.

Hace tres barridos: tipos y constructores, alias expandidos, y tipos de los campos; así el orden no importa. Un ciclo de alias (`type A = B`, `type B = A`) es E0801.

### El formato de `Float`

Un `Float` debe imprimirse igual en los tres backends, así que todos lo escribirán con `kirt/fmtnum.Float`:

- **Dígitos:** los más cortos que se leen de vuelta con los mismos bits, de `strconv.FormatFloat(f, 'e', -1, 64)`.
- **Forma fija** con `1e-6 <= |f| < 1e21`, los umbrales de `Number.prototype.toString` de JavaScript; fuera, exponencial sin `+` ni ceros de relleno: `1e21`, `1.5e-7`.
- **`.0`** si la forma fija no tiene punto (`1.0`), para que un `Float` no se lea como un `Int`.
- **Especiales con nombre:** `NaN`, `inf`, `-inf`, `-0.0`.

`'g'` no sirve: escribe `1e+06` para un millón, `1e+21` y `+Inf`. Añadirle `.0` a ciegas daría `1e+06.0`.

### FMA: un redondeo de menos

La especificación de Go permite **fusionar** `x*y + z` en una instrucción (FMA) que redondea una vez, incluso entre sentencias (`t = x*y; r = t + z`). El resultado puede cambiar en el último bit o, si la resta cancela, del todo: de 0 a 2⁻¹⁰⁴. Lo impide una conversión explícita: `float64(x*y) + z` redondea el producto.

Ninguna opción apaga la fusión, salvo `-d=fmahash`, de depuración. `GOAMD64` elige el nivel de CPU x86-64, y desde `v3` Go usa FMA: un prototipo, `Naive(x, y, z) = x*y + z`, da `VFMADD231SD` con `v3`, `MULSD` y `ADDSD` con `v1`, el valor por defecto, y `FMADDD` en arm64.

Por eso cada primitiva de `Float` hace una sola operación, y un canario (un test que vigila un hecho de Go del que Ki depende), C10, pregunta al compilador qué hizo:

- **arm64, estático:** lee el ensamblador que imprime `-gcflags=-S` y exige que no haya ninguna instrucción fusionada.
- **amd64, ejecutando:** el mismo test interno, compilado con `v1` y con `v3`, imprime los mismos bits.

El prototipo, que sí fusiona, es el control: prueba que el detector de arm64 ve una fusión y que `v3` funde de verdad.

### Layout de structs con `reflect`

Go alinea cada campo a la alineación de su tipo (en un entero o un `float64`, su tamaño) y rellena los huecos. `reflect.TypeOf(v)` da un `reflect.Type`: `Size()` y `Align()` miden el struct, y `Field(i).Offset` dice dónde empieza cada campo.

| Struct | `Size` | Offsets |
|---|---|---|
| `struct{ A bool; B int64; C bool }` | 24 | 0, 8, 16 |
| `struct{ B int64; A bool; C bool }` | 16 | 0, 8, 9 |

Ordenar los campos de mayor a menor alineación ahorra relleno.

### Plano o en cajas

`interp.ConV` es `{Ctor core.CtorID; Fields []Value}`. Cada campo es un `Value`, una interfaz de 16 bytes, y guardar en ella un `ConV` o un `FloatV` calculado reserva memoria aparte: es la **caja** (*boxing*).

Un `[]Value` con 1 000 `FloatV` calculados hace 1 001 reservas; un `[]float64`, una. Ki paga la caja a cambio de un `Show` y una máquina para todas las formas: el oráculo no compite en velocidad.

### `new(expr)`

En Go 1.27, `new` acepta una expresión (desde 1.26): `new(int64(300))` da un `*int64` que apunta a 300. Hace lo que el `ptr` genérico del capítulo 3; este capítulo no lo usa.

## Tests primero

{{cards}}

Por cada tarjeta: escribe el test, míralo fallar, implementa lo justo para el verde; solo entonces despliega su referencia. Las sesiones cambian tests anteriores; si uno tuyo falla, compáralo con `ref/ch11-sK`.

## Implementación

### Sesión 1: lo que trae la Parte I, `Float` y `kirt/fmtnum` (≈ 3 h)

#### Lo que trae la Parte I

Trae primero lo que la referencia corrigió tras `ch10-s4`, con sus tests:

1. **Las casillas cuentan:** `KI_STACK_MB` limita también las de entornos y argumentos en espera, a 16 bytes cada una.
2. **Lo que se saca, se borra.** Cada corte de `stack` y de `envs` limpia lo que deja, como avisaba la fe de erratas del capítulo 10.

{{diff ch10-s4..ch11-s1 internal/interp/machine_test.go}}

{{diff ch10-s4..ch11-s1 internal/interp/machine_internal_test.go}}

3. **El pase `render`.** Escribir los diagnósticos es un pase: un panic ahí es un ICE con 70, que dice cuántos diagnósticos no muestra.

{{diff ch10-s4..ch11-s1 internal/driver/session.go}}

{{diff ch10-s4..ch11-s1 internal/driver/ice.go}}

{{diff ch10-s4..ch11-s1 internal/driver/ice_test.go}}

{{diff ch10-s4..ch11-s1 internal/driver/cli.go}}

{{diff ch10-s4..ch11-s1 internal/driver/check.go}}

{{diff ch10-s4..ch11-s1 internal/driver/emit.go}}

{{diff ch10-s4..ch11-s1 internal/driver/run.go}}

4. **Un ayudante por idea**, en lugar de copias en `kitest`, `driver` e `interp`: `base.Cause` quita el `*fs.PathError` de un error, y `out.TryFlush` vacía un `io.Writer` si puede.

{{diff ch10-s4..ch11-s1 internal/base/load.go}}

{{diff ch10-s4..ch11-s1 kirt/out/writer.go}}

{{diff ch10-s4..ch11-s1 internal/kitest/runner.go}}

{{diff ch10-s4..ch11-s1 internal/kitest/update.go}}

{{diff ch10-s4..ch11-s1 internal/driver/test.go}}

5. **`testsupport.Compile` pasa Lint:** un Core roto falla en todo test que compila.

{{diff ch10-s4..ch11-s1 internal/testsupport/compile.go}}

6. **Solo comentarios:** marcan la API de la Parte I que nadie llama aún. Tu código no cambia.

{{diff ch10-s4..ch11-s1 internal/base/arena.go}}

{{diff ch10-s4..ch11-s1 internal/base/line_table.go}}

{{diff ch10-s4..ch11-s1 internal/base/source.go}}

{{diff ch10-s4..ch11-s1 internal/diag/diagnostic.go}}

{{diff ch10-s4..ch11-s1 internal/diag/sink.go}}

{{diff ch10-s4..ch11-s1 internal/driver/version.go}}

`ci.yml` quita el `Acquire::Retries=3` inútil (fe de erratas del capítulo 1), `README.md` nombra `docs/errors/`, y tu repo no necesita `ci/mutate`. Los diffs de `machine.go`, `prims.go` y `reason.go` van abajo, con lo de `Float`.

#### Los tests, en rojo

1. **Escribe T11.01–T11.33**, menos T11.15, que espera a la sesión 2. La interfaz:

```go
package fmtnum // kirt/fmtnum: a leaf of kirt, standard library only
func Float(f float64) string

package syntax
// ExprFloat comes after ExprError; Expr gains: Float float64

package sema
const BuiltinFloat Builtin = BuiltinUnit + 1

package core
// LitFloat comes after Match; Expr gains: Float float64
const (
	PrimAddFloat Prim = PrimNotBool + 1 + iota
	PrimSubFloat
	PrimMulFloat
	PrimDivFloat
	PrimNegFloat
	PrimEqFloat
	PrimNeFloat
	PrimLtFloat
	PrimLeFloat
	PrimGtFloat
	PrimGeFloat
)
func (b *Builder) LitFloat(sp base.Span, v float64) ExprID

package interp
type FloatV struct{ V float64 }
type externFn func(m *Machine, at base.Span, args []Value) (Value, *Halt) // a non-nil Halt stops the machine
```

2. **Míralos fallar.** `kirt/fmtnum` aún no tiene código:

```console doctest="red"
$ go test ./kirt/fmtnum
github.com/aldezex/kilang/kirt/fmtnum: no non-test Go files in …
FAIL	github.com/aldezex/kilang/kirt/fmtnum [build failed]
FAIL
```

#### El formato

{{file ch11-s1 kirt/fmtnum/fmtnum.go}}

{{reftest T11.03}}

Si escribieras `exp > 21` en lugar de `exp > 20`, `1e21` saldría `1000000000000000000000.0` y el test lo ve.

#### `Float` en el compilador

1. **El parser** lee el token `Float`, que el lexer ya daba; `emit ast` lo escribe con `fmtnum`, y `emit tokens` sigue con `'g'`. El lexer anota los `Int` que rechazó (E0105, E0107), y el parser hace de cada uno un nodo de error.

{{diff ch10-s4..ch11-s1 internal/syntax/token.go}}

{{diff ch10-s4..ch11-s1 internal/syntax/numbers.go}}

{{diff ch10-s4..ch11-s1 internal/syntax/ast.go}}

{{diff ch10-s4..ch11-s1 internal/syntax/parser_expr.go}}

{{diff ch10-s4..ch11-s1 internal/syntax/ast_printer.go}}

2. **Los operadores** son fijos por tipo, y decide el operando izquierdo: `Int` o `Float` para la aritmética, también `Bool` en `==`, y solo `Int` en `%`. No hay conversión implícita: `toFloat` pasa de `Int` a `Float`, y `truncate`, al revés, hacia cero; `sqrt` da la raíz. Un literal entero que un `Float` representa exacto trae, donde va un `Float`, la ayuda «write the number as a `Float`: `1.0`» y su edición.
3. **Un operando sin tipo aún espera.** En `fn sq(x) -> Float { x * x }`, juzgar `x` enseguida lo fijaría a `Int` si el cuerpo va antes que su llamada, así que el operador espera al final de los cuerpos y el veredicto no depende del orden.

{{diff ch10-s4..ch11-s1 internal/sema/check.go}}

{{diff ch10-s4..ch11-s1 internal/sema/types.go}}

{{diff ch10-s4..ch11-s1 internal/sema/unify.go}}

{{diff ch10-s4..ch11-s1 internal/sema/resolve.go}}

{{diff ch10-s4..ch11-s1 internal/sema/reason.go}}

{{reftest T11.13}}

4. **La elaboración** elige la primitiva por el tipo del operando izquierdo, e `Int` para una variable que sigue libre.

{{diff ch10-s4..ch11-s1 internal/core/core.go}}

{{diff ch10-s4..ch11-s1 internal/core/builder.go}}

{{diff ch10-s4..ch11-s1 internal/core/elaborate.go}}

{{diff ch10-s4..ch11-s1 internal/core/lint.go}}

{{diff ch10-s4..ch11-s1 internal/core/print.go}}

5. **El oráculo** sigue IEEE: `1.0 / 0.0` es `inf`, sin panic. Un extern recibe el span de la llamada, porque `truncate` hace un panic con `NaN`, un infinito o un valor fuera de `Int`: sale con 101 y `Float out of range`. Su límite superior es `f >= 1<<63`; `float64(math.MaxInt64)` redondea a 2⁶³.

{{diff ch10-s4..ch11-s1 internal/interp/value.go}}

{{diff ch10-s4..ch11-s1 internal/interp/prims.go}}

{{diff ch10-s4..ch11-s1 internal/interp/externs.go}}

{{diff ch10-s4..ch11-s1 internal/interp/machine.go}}

{{diff ch10-s4..ch11-s1 stdlib/prelude.ki}}

6. **El canario C10.** `kirt` no importa `internal/interp`, así que el corpus vive en un test interno de `interp`, y el canario lo compila y lo ejecuta. Necesita el prototipo: copia `docs/research/proto/c10-floats` de `ref/ch11-s1`.

{{file ch11-s1 internal/interp/c10_internal_test.go}}

{{file ch11-s1 kirt/canary/c10_test.go}}

7. **El catálogo** cambia tres entradas, y dos golden fijan el hito y el panic:

{{diff ch10-s4..ch11-s1 docs/errors/E0109.md}}

{{diff ch10-s4..ch11-s1 docs/errors/E0406.md}}

{{diff ch10-s4..ch11-s1 docs/errors/E0401.md}}

{{file ch11-s1 tests/golden/ch11/floats.ki}}

{{file ch11-s1 tests/golden/ch11/truncate_panic.ki}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch11-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch11-s1`; compárate con `ref/ch11-s1`.

### Sesión 2: declaraciones `type` (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T11.15 y T11.34–T11.65**, menos T11.47, T11.49, T11.53–T11.55 y T11.58–T11.61, que esperan a la sesión 3. La interfaz:

```go
package syntax
// ItemType comes after ItemError
type TypeForm uint8
const (
	TypeFormSum TypeForm = iota
	TypeFormRecord
	TypeFormAlias
)
type Variant struct {
	Name           base.Symbol
	Span, NameSpan base.Span
	Fields         base.ListRange // over Module.FieldPool
}
type Field struct {
	Name           base.Symbol
	Span, NameSpan base.Span
	Type           TypeExprID
	HasName        bool // false: positional, as in `Pair(Int, Shape)`
}
// Item gains: Form TypeForm; Variants, Fields base.ListRange; Alias TypeExprID; ErrorOf ItemKind
// Module gains: VariantPool []Variant; FieldPool []Field
func (m *Module) Variants(r base.ListRange) []Variant
func (m *Module) Fields(r base.ListRange) []Field

package sema
const (
	DefType DefKind = DefExternFn + 1 + iota
	DefCtor
)
func (rp *ResolvedProgram) LookupType(mod ModuleIdx, name base.Symbol) (Res, bool)
```

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/syntax
# github.com/aldezex/kilang/internal/syntax_test [github.com/aldezex/kilang/internal/syntax.test]
internal/syntax/ast_printer_test.go:253:7: b.m.FieldPool undefined (type syntax.Module has no field or method FieldPool)
…
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
FAIL
```

#### El parser

{{file ch11-s2 internal/syntax/parser_type.go}}

{{diff ch11-s1..ch11-s2 internal/syntax/ast.go}}

{{diff ch11-s1..ch11-s2 internal/syntax/parser.go}}

{{diff ch11-s1..ch11-s2 internal/syntax/ast_printer.go}}

{{diff ch11-s1..ch11-s2 internal/syntax/recover.go}}

- **La forma** se decide con un token: tras la `{`, `Upper` es una suma; `lower`, un record.
- **Un `type` roto** es un `ItemError` que guarda su nombre y sus variantes, con `ErrorOf` diciendo qué palabra lo empezó: así sus usos no añaden errores.

#### Los nombres

{{diff ch11-s1..ch11-s2 internal/sema/resolve.go}}

{{diff ch11-s1..ch11-s2 internal/sema/suggest.go}}

{{diff ch11-s1..ch11-s2 internal/sema/check.go}}

{{diff ch11-s1..ch11-s2 internal/core/elaborate.go}}

- **Repetir** un tipo o un constructor en su espacio es E0302.
- **Un tipo declarado tapa un builtin,** como una función del módulo tapa la del preludio.
- **Rellenos en Core:** un tipo y un constructor elaboran a una `Fun` con cuerpo `()`, para que `FunID` siga siendo `DefID`. La sesión 3 sustituye los de los constructores.

{{reftest T11.56}}

**Fe de erratas.** El E0406 del capítulo 8 dice ahora «not a type in this scope», sin la nota que listaba los tipos de Ki, y sugiere también tipos declarados.

{{diff ch11-s1..ch11-s2 docs/errors/E0302.md}}

{{diff ch11-s1..ch11-s2 docs/errors/E0406.md}}

{{file ch11-s2 tests/golden/ch11/type_declarations.ki}}

{{file ch11-s2 tests/errors/ch11/broken_type.ki}}

{{file ch11-s2 tests/errors/ch11/type_redefined.ki}}

{{file ch11-s2 tests/errors/ch11/unknown_type_render.ki}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch11-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch11-s2`; compárate con `ref/ch11-s2`.

### Sesión 3: declaraciones comprobadas, constructores en Core y en el oráculo (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe los que esperaban y T11.66–T11.98**, menos T11.58 y T11.70, que esperan a la 4. La interfaz:

```go
package sema
type TypeDefID uint32
type CtorID uint32
const TypeData TypeKind = TypeErr + 1 // Type gains: Def TypeDefID; Args []TypeID
type TypeDef struct {
	Name   base.Symbol
	Span   base.Span
	Module ModuleIdx
	Item   syntax.ItemID
	Form   syntax.TypeForm
	Ctors  []CtorID
	Fields []FieldDef
	Alias  TypeID
}
type FieldDef struct {
	Name base.Symbol
	Span base.Span
	Type TypeID
}
type Ctor struct {
	Name   base.Symbol
	Span   base.Span
	Owner  TypeDefID
	Tag    uint32
	Args   []TypeID
	Fields []base.Symbol // nil when positional
}
type Decls struct {
	Types *TypeTable
	Defs  base.IdVec[TypeDefID, TypeDef]
	Ctors base.IdVec[CtorID, Ctor]
	// unexported maps from DefID
}
func (d *Decls) TypeDefOf(def DefID) (TypeDefID, bool)
func (d *Decls) CtorOf(def DefID) (CtorID, bool)
func (d *Decls) Meaning(td TypeDefID) TypeID
func (d *Decls) CtorType(c CtorID) TypeID
func (d *Decls) FieldSpan(mods []*syntax.Module, c CtorID, i int) base.Span
func (t *TypeTable) Data(def TypeDefID, args []TypeID) TypeID
func Declare(mods []*syntax.Module, res *ResolvedProgram, in *base.Interner, sink *diag.Sink) *Decls
func Check(mods []*syntax.Module, res *ResolvedProgram, decls *Decls, in *base.Interner, sink *diag.Sink) *TypedProgram // TypedProgram gains Decls
const ReasonCtorArg ReasonKind = ReasonOperand + 1

package core
type CtorID uint32
type Ctor struct {
	Name   base.Symbol
	Tag    uint32
	Arity  uint32
	Tuple  bool
	Fields []base.Symbol
}
// Con comes after LitFloat: Ctor, Args. Expr gains: Ctor CtorID. Program gains: Ctors base.IdVec[CtorID, Ctor]
func (b *Builder) Con(sp base.Span, c CtorID, args []ExprID) ExprID

package interp
type ConV struct {
	Ctor   core.CtorID
	Fields []Value
}
func Show(v Value, p *core.Program, in *base.Interner) string
```

`Show` recibe el programa y el interner: un `ConV` guarda el id de su constructor, no su nombre.

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/sema
# github.com/aldezex/kilang/internal/sema [github.com/aldezex/kilang/internal/sema.test]
internal/sema/types_internal_test.go:784:16: tbl.Data undefined (type *TypeTable has no field or method Data)
…
FAIL	github.com/aldezex/kilang/internal/sema [build failed]
FAIL
```

#### `declare`

{{file ch11-s3 internal/sema/declare.go}}

{{diff ch11-s2..ch11-s3 internal/sema/types.go}}

{{diff ch11-s2..ch11-s3 internal/sema/unify.go}}

{{diff ch11-s2..ch11-s3 internal/sema/check.go}}

{{diff ch11-s2..ch11-s3 internal/sema/reason.go}}

{{diff ch11-s2..ch11-s3 internal/sema/resolve.go}}

{{diff ch11-s2..ch11-s3 internal/sema/suggest.go}}

{{diff ch11-s2..ch11-s3 internal/driver/pipeline.go}}

{{diff ch11-s2..ch11-s3 internal/driver/session.go}}

1. **Expandir alias** sigue la cadena con un bucle y una pila propia: 10 000 alias no añaden marcos de Go. Un ciclo es un solo E0801, con una etiqueta en cada alias.
2. **Un nombre en mayúscula** que nada declara es E0306, que sugiere solo entre constructores.
3. **`paramSpan`**, que sitúa la etiqueta de un argumento que no encaja, apunta en un constructor al campo de su variante, porque no hay `Params`.

{{reftest T11.75}}

#### Constructores en Core y en el oráculo

{{diff ch11-s2..ch11-s3 internal/core/core.go}}

{{diff ch11-s2..ch11-s3 internal/core/builder.go}}

{{diff ch11-s2..ch11-s3 internal/core/elaborate.go}}

{{diff ch11-s2..ch11-s3 internal/core/lint.go}}

{{diff ch11-s2..ch11-s3 internal/core/print.go}}

{{diff ch11-s2..ch11-s3 internal/interp/value.go}}

{{diff ch11-s2..ch11-s3 internal/interp/machine.go}}

1. **`Circle(1.0)`** elabora a un `Con`, y `Circle` sin llamar, a su envoltorio, `fn Circle(r) = Circle(r)`. Lint comprueba la aridad de cada `Con`.
2. **`conFrame`** junta los argumentos como un `argFrame`, y su espacio cuenta como casillas.

{{diff ch11-s2..ch11-s3 docs/errors/E0302.md}}

{{file ch11-s3 docs/errors/E0306.md}}

{{file ch11-s3 docs/errors/E0801.md}}

{{diff ch11-s2..ch11-s3 docs/errors/E0401.md}}

{{diff ch11-s2..ch11-s3 docs/errors/E0402.md}}

{{diff ch11-s2..ch11-s3 docs/errors/E0403.md}}

{{file ch11-s3 tests/golden/ch11/constructors.ki}}

{{file ch11-s3 tests/golden/ch11/constructors_core.ki}}

{{file ch11-s3 tests/errors/ch11/unknown_constructor_render.ki}}

{{file ch11-s3 tests/errors/ch11/alias_cycle_render.ki}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch11-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch11-s3`; compárate con `ref/ch11-s3`.

### Sesión 4: records, acceso a campos, actualización y tuplas (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T11.58, T11.70 y T11.99–T11.159.** La interfaz:

```go
package syntax
const (
	ExprField      ExprKind = ExprFloat + 1 + iota // Target, Name, NameSpan: `p.x`
	ExprTupleIndex                                 // Target, Index, Name, OpSpan: `t.0`
	ExprRecord                                     // Name, NameSpan, Fields, Base, HasBase, OpSpan: `Point { x: 1.0, ..p }`
	ExprTuple                                      // Args: `(1, true)`
)
type FieldInit struct {
	Name           base.Symbol
	Span           base.Span
	Value          ExprID
	Pun            bool
	Removal        base.Span // the field and its comma, for the edit of E0411
	RemovalOneLine bool
}
// Expr gains: Target ExprID; Index uint32; NameSpan base.Span; Fields base.ListRange; Base ExprID; HasBase bool
const TypeTuple TypeExprKind = TypeError + 1 // Elems base.ListRange over Module.TypePool
// Module gains: FieldInitPool []FieldInit; TypePool []TypeExprID
func (m *Module) FieldInits(r base.ListRange) []FieldInit
func (m *Module) TypeList(r base.ListRange) []TypeExprID

package sema
const TypeTuple TypeKind = TypeData + 1 // Params holds the elements
func (t *TypeTable) Tuple(elems []TypeID) TypeID
func (d *Decls) FieldIndex(td TypeDefID, name base.Symbol) (int, bool)
const ReasonField ReasonKind = ReasonCtorArg + 1

package core
// Proj comes after Con: Target, Index. Expr gains: Target ExprID; Index uint32. Ctor gains: Record bool
func (b *Builder) Proj(sp base.Span, target ExprID, index uint32) ExprID
```

2. **Míralos fallar.** No compila:

```console doctest="red"
$ go test ./internal/syntax
# github.com/aldezex/kilang/internal/syntax_test [github.com/aldezex/kilang/internal/syntax.test]
internal/syntax/ast_printer_data_test.go:15:41: undefined: syntax.ExprField
…
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
FAIL
```

#### El parser

{{diff ch11-s3..ch11-s4 internal/syntax/token.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/numbers.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/ast.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/parser.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/parser_expr.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/parser_stmt.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/parser_type.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/recover.go}}

{{diff ch11-s3..ch11-s4 internal/syntax/ast_printer.go}}

1. **La regla de Rust:** en la cabeza de un `if`, `Nombre {` es un nombre seguido del bloque; dentro de un delimitador vuelve el literal. Lo que solo puede ser un literal (`{ x: …`) pide paréntesis, con un E0201 y su edición.
2. **`n.0.1`** llega como `Dot` y `Float(0.1)`: el parser parte el número en dos índices. Tras un `.`, un número que el lexer rechazó es solo su error.
3. **Un tipo tupla** es recursivo, entre `enter` y `leave`: anidado 10 000 veces es un E0204, no un desbordamiento.

{{reftest T11.102}}

`condition` activa `noRecord` mientras lee la cabeza; sin eso, `Origin { 1 }` se leería como un literal, y este test lo ve.

#### Comprobar y ejecutar

{{file ch11-s4 internal/sema/check_data.go}}

{{diff ch11-s3..ch11-s4 internal/sema/check.go}}

{{diff ch11-s3..ch11-s4 internal/sema/types.go}}

{{diff ch11-s3..ch11-s4 internal/sema/declare.go}}

{{diff ch11-s3..ch11-s4 internal/sema/unify.go}}

{{diff ch11-s3..ch11-s4 internal/sema/reason.go}}

{{diff ch11-s3..ch11-s4 internal/sema/resolve.go}}

1. **Un acceso exige conocer ya el tipo del receptor.** Si nada ha fijado aún el de un parámetro sin anotar, `p.x` es E0412. Al contrario que un operador, no espera: el veredicto puede depender del orden de las funciones.
2. **`Unify` baja por las tuplas** elemento a elemento; si no, `(a, Int)` nunca unificaría.
3. **Un error, un mensaje:** un campo desconocido da al acceso el tipo de error, que no se queja más.

{{diff ch11-s3..ch11-s4 internal/core/core.go}}

{{diff ch11-s3..ch11-s4 internal/core/builder.go}}

{{diff ch11-s3..ch11-s4 internal/core/elaborate.go}}

{{diff ch11-s3..ch11-s4 internal/core/lint.go}}

{{diff ch11-s3..ch11-s4 internal/core/print.go}}

{{diff ch11-s3..ch11-s4 internal/interp/value.go}}

{{diff ch11-s3..ch11-s4 internal/interp/machine.go}}

4. **Una tupla** usa el constructor de su aridad.
5. **El update construye otro valor:** liga los valores en el orden escrito, luego la base a `base`, una sola vez, y toma con `Proj` los campos que faltan. El oráculo nunca escribe en un `ConV`.

{{reftest T11.146}}

{{diff ch11-s3..ch11-s4 docs/errors/E0201.md}}

{{diff ch11-s3..ch11-s4 docs/errors/E0204.md}}

{{diff ch11-s3..ch11-s4 docs/errors/E0205.md}}

{{diff ch11-s3..ch11-s4 docs/errors/E0302.md}}

{{diff ch11-s3..ch11-s4 docs/errors/E0406.md}}

{{diff ch11-s3..ch11-s4 docs/errors/E0801.md}}

{{file ch11-s4 docs/errors/E0409.md}}

{{file ch11-s4 docs/errors/E0410.md}}

{{file ch11-s4 docs/errors/E0411.md}}

{{file ch11-s4 docs/errors/E0412.md}}

{{file ch11-s4 docs/errors/E0413.md}}

{{file ch11-s4 tests/golden/ch11/records.ki}}

{{file ch11-s4 tests/golden/ch11/records_core.ki}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch11-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch11-s4`; compárate con `ref/ch11-s4`.

## Ejecútalo

Con `ki` construido, el hito, un panic y dos errores:

```console
$ build/bin/ki run tests/golden/ch11/floats.ki
1.0
0.1
1000000.0
1e21
NaN
inf
-inf
0.30000000000000004
1.5e-7
-0.0
$ build/bin/ki run tests/golden/ch11/records.ki
1.0
2.0
3.0
2.0
7
2.0
$ build/bin/ki run tests/golden/ch11/truncate_panic.ki
3
panic: Float out of range at tests/golden/ch11/truncate_panic.ki:9:9
$ echo $?
101
$ build/bin/ki check tests/errors/ch11/unknown_field_render.ki
error[E0409]: no field `z` on `Point`
  --> tests/errors/ch11/unknown_field_render.ki:20:16
   |
17 | type Point { x: Float, y: Float }
   |      ----- `Point` is defined here
   ...
20 |   printFloat(p.z)
   |                ^ unknown field
   = help: did you mean `x`?
   |
20 -   printFloat(p.z)
20 +   printFloat(p.x)
$ build/bin/ki check tests/errors/ch11/not_record_render.ki
error[E0412]: `Shape` is not a record
  --> tests/errors/ch11/not_record_render.ki:17:16
   |
14 | type Shape { Circle(r: Float), Square(side: Float) }
   |      ----- `Shape` is defined here
   ...
17 |   printFloat(s.r)
   |                ^ has no fields to access
   = note: only a record has fields; a sum type is taken apart with `match`
```

## Ejercicios

:::exercise
**1. El tamaño de un `ConV`.** Fija con `reflect`, en un test interno, el tamaño, la alineación y el offset de cada campo de `interp.ConV`. ¿Cuánto es relleno?
:::

:::solution
En `internal/interp/layout_internal_test.go`, con un nombre que ningún otro test interno usa:

```go
package interp

import (
	"reflect"
	"testing"
)

func TestLayout(t *testing.T) {
	t.Run("conv_has_four_bytes_of_padding", func(t *testing.T) {
		ty := reflect.TypeOf(ConV{})
		if ty.Size() != 32 || ty.Align() != 8 {
			t.Fatalf("ConV: Size %d, Align %d, want 32 and 8", ty.Size(), ty.Align())
		}
		rows := []struct {
			name         string
			offset, size uintptr
		}{
			{"Ctor", 0, 4},
			{"Fields", 8, 24},
		}
		var used uintptr
		for i, row := range rows {
			f := ty.Field(i)
			if f.Name != row.name || f.Offset != row.offset || f.Type.Size() != row.size {
				t.Errorf("%s: field %d is %s at %d, %d bytes, want %s at %d, %d bytes", row.name, i, f.Name, f.Offset, f.Type.Size(), row.name, row.offset, row.size)
			}
			used += f.Type.Size()
		}
		if pad := ty.Size() - used; pad != 4 {
			t.Errorf("ConV has %d bytes of padding, want 4", pad)
		}
	})
}
```

Son 4 bytes: `Ctor` es un `uint32`, y el slice, de 24 bytes (puntero, longitud y capacidad), se alinea a 8. Cada `ConV` que entra en un `Value` reserva esos 32 bytes, y su array de campos, 16 bytes más por campo.
:::

:::exercise
**2. Un alias en una firma.** Con `type Meters = Float`, escribe `fn double(m: Meters) -> Meters` y pásale `1.5` y `sqrt(2.0)`. Luego haz de `Meters` un record de un campo y lee lo que dice `ki check`.
:::

:::solution
```text
type Meters = Float
fn double(m: Meters) -> Meters { m * 2.0 }
fn main() {
  printFloat(double(1.5))
  printFloat(double(sqrt(2.0)))
}
```

Imprime `3.0` y `2.8284271247461903`: el alias es `Float`, así que vale cualquier `Float`, y `*` lo acepta. Con `type Meters { v: Float }`, `ki check` da cinco E0401; con `main` reducido a `printFloat(double(1.5).v)`, son dos E0401: «expected `Int` or `Float`, found `Meters`» sobre la `m` de `m * 2.0`, y «expected `Meters`, found `Float`» sobre el `1.5`, con la etiqueta en el parámetro. Un record es un tipo nuevo; un alias, no.
:::

:::exercise
**3. `truncate` en el borde.** Ejecuta `truncate(9223372036854774784.0)` y `truncate(9223372036854775807.0)`, y explica por qué solo el segundo hace panic.
:::

:::solution
```text
fn main() {
  print(truncate(9223372036854774784.0))
  print(truncate(-9223372036854775808.0))
  print(truncate(9223372036854775807.0))
}
```

Imprime `9223372036854774784` y `-9223372036854775808`, y después `panic: Float out of range at …:4:9`, con 101. Un `float64` tiene 53 bits de mantisa: el literal `9223372036854775807.0` redondea a 2⁶³, que no cabe en un `Int`. El primero es el mayor `Float` por debajo de 2⁶³, y -2⁶³ es exacto.
:::

:::exercise
**4. Una tupla como resultado.** Escribe `fn divmod(a: Int, b: Int) -> (Int, Int)` y comprueba qué da con `-7` y `2`.
:::

:::solution
```text
fn divmod(a: Int, b: Int) -> (Int, Int) { (a / b, a % b) }
fn main() {
  let r = divmod(-7, 2)
  print(r.0)
  print(r.1)
}
```

Imprime `-3` y `-1`: `/` trunca hacia cero y `%` toma el signo del dividendo (capítulo 10). En Core, la tupla es un `Con` del constructor de aridad 2, y `r.0`, un `Proj`.
:::

## Preguntas de repaso

:::review
¿Por qué `Circle` es una función y `Point` no?
:::

:::answer
Una variante se escribe por posición, `Circle(1.0)`, como una llamada: tratarla como función de sus campos a su tipo deja pasarla como valor sin un caso aparte. Un literal de record nombra los campos en cualquier orden y admite `..base`, así que no hay una lista de argumentos que llamar.
:::

:::review
¿Por qué `declare` es un pase propio entre `resolve` y `check`?
:::

:::answer
`resolve` liga nombres, y `declare` hace tipos con ellos: lee el tipo de cada campo con `LookupType`, sobre la tabla de nombres ya terminada, en tres barridos, así que un campo puede nombrar un tipo declarado más abajo o uno que lo nombra de vuelta. Y `check` lee anotaciones que pueden ser alias, y llama a constructores: necesita la tabla hecha, con los alias expandidos.
:::

:::review
¿Por qué `fmtnum` no usa `'g'` y le añade `.0`?
:::

:::answer
Porque `'g'` decide su propia forma: pasa a exponente desde `1e6` y escribe `1e+06` y `+Inf`. Añadir `.0` daría `1e+06.0`. `fmtnum` toma de `strconv` solo los dígitos más cortos y escribe la forma con su regla, la misma en los tres backends.
:::

:::review
¿Por qué hace falta C10 si cada primitiva de `Float` hace una sola operación?
:::

:::answer
Porque la especificación permite fusionar incluso entre sentencias, y una operación por primitiva es una disciplina del código, no una promesa del compilador. C10 pregunta qué hizo de verdad: ninguna instrucción fusionada en arm64, y los mismos bits con `GOAMD64=v1` y `v3`. Su control, el prototipo, prueba que el detector de arm64 ve una fusión y que `v3` funde de verdad.
:::

:::review
¿Por qué un operador con un operando sin tipo espera, y un acceso a campo no?
:::

:::answer
Un operador acepta pocos tipos fijos, y juzgarlo cuando todos los cuerpos están comprobados da el mismo veredicto en cualquier orden. Un acceso necesita saber de qué record es para elegir el campo y su tipo en ese punto: la regla es conocer antes el tipo del receptor. El precio es que el orden de las funciones puede cambiar el veredicto.
:::

:::review
¿Por qué el update liga la base en un `let`?
:::

:::answer
Porque la base es una expresión, que puede imprimir o hacer panic: leída en cada `Proj`, correría una vez por campo que falta. Ligada, corre una vez y después de los valores escritos, en el orden del texto. Y el resultado es un `Con` nuevo, así que la base sigue intacta.
:::

:::review
¿Por qué `if x == Origin { 1 } else { 2 }` no lee un literal?
:::

:::answer
Porque la `{` tras la condición es el bloque del `if`: leer `Origin { 1 }` como literal dejaría el `if` sin bloque. En la cabeza, fuera de todo delimitador, `Nombre {` es un nombre, como en Rust; entre paréntesis vuelve a ser un literal.
:::

## Resumen

- Un `type` es suma, record o alias; sumas y records son nominales.
- Un constructor con campos es una función; un record, un literal.
- `TypeData` se hash-consea con la declaración en la clave.
- `declare`, entre `resolve` y `check`, no depende del orden del fichero.
- `kirt/fmtnum` escribe un `Float` con los dígitos más cortos y los umbrales de JavaScript.
- Go puede fusionar `x*y + z`; `float64()` lo impide, y C10 lo vigila.
- `reflect` mide tamaño, alineación y offsets; un `Value` en caja cuesta una reserva.
- Un record es un `Con`, un campo un `Proj`, y el update un valor nuevo con la base leída una vez.
- En la cabeza de un `if`, `Nombre {` no empieza un literal.

## Go en el trabajo

**«Acepta interfaces, devuelve structs».** *Go Code Review Comments* lo explica: la interfaz es del paquete que la usa, y el que implementa devuelve tipos concretos, que pueden ganar métodos sin romper a nadie. Así, `sema.Declare` devuelve `*Decls`.

**Slice de structs o de interfaces.** `image.RGBA` guarda sus píxeles en un `Pix []uint8` plano. `At` devuelve la interfaz `color.Color`, para cumplir `image.Image`; `RGBAAt`, el struct `color.RGBA`.

**`reflect` en serializadores.** `encoding/json` recorre los campos con `reflect` y guarda un codificador por `reflect.Type` en un `sync.Map`: inspeccionar un tipo es caro, y se hace una vez.
