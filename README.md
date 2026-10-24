# kilang-ref

Reference implementation of Ki, a functional language with algebraic effects written in Go (standard library only), companion to the Spanish book *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*.

## Qué es este repositorio

Es la implementación de referencia del libro: el compilador de Ki, su runtime, sus tests y la prosa de cada capítulo. La historia de `main` es lineal y está marcada con etiquetas anotadas:

- `chNN-sK`: el final de la sesión K del capítulo NN (por ejemplo, `ch01-s1`), en verde, con sus tests;
- `chNN`: el capítulo NN completo, con su código, sus tests, sus tarjetas de test y su prosa;
- `mN`: un hito; `m1` es el momento en que `ki run fib.ki` imprime `6765`.

De esas etiquetas salen todos los listados del libro. La herramienta `bookx` (`cmd/bookx`, solo en este repositorio) los lee con `git show`, reconstruye cada etiqueta con `git archive` y comprueba que sus tests pasan antes de publicar un capítulo.

Tu código vive en tu propio repositorio, que empiezas vacío en el capítulo 1 (en el libro, `~/kilang`, publicado como `aldezex/kilang`). Este solo sirve para compararte con la referencia o para recuperarte de un atasco. Contiene todas las soluciones: si no quieres verlas antes de tiempo, no lo mires; el libro ya las pliega.

## El módulo

El módulo es `github.com/aldezex/kilang`, el mismo que el de tu repositorio: `kilang-ref` es el nombre del repositorio, no del módulo. Así cualquier fichero de la referencia encaja en tu árbol sin cambiar un import.

El código usa Go 1.27.3 y solo la biblioteca estándar: ni `golang.org/x/*`, ni cgo, ni módulos de terceros.

## Cómo compararte con la referencia

Trae sus etiquetas a tu repositorio con un prefijo, para que no choquen con las tuyas:

```sh
git remote add --no-tags ref https://github.com/aldezex/kilang-ref.git
git fetch ref 'refs/tags/*:refs/tags/ref/*'
```

Después puedes comparar tu árbol con cualquier sesión:

```sh
git diff ref/ch05-s2 -- internal/syntax/
```

Y si te atascas o tu versión se desvía demasiado, copia el fichero de la etiqueta y sigue:

```sh
git restore --source=ref/ch05-s2 -- internal/syntax/lexer.go
```

## Estructura

La estructura crece con el libro: cada directorio aparece en el capítulo que lo necesita.

```text
cmd/ki/              la CLI `ki`
cmd/bookx/           (solo aquí) extractor y verificador de listados; genera el libro web
internal/            el compilador: base, diag, syntax, sema, core, interp, kitest, driver…
kirt/                el runtime de Ki, escrito solo en Go (nace en el capítulo 10 con kirt/out)
stdlib/              el preludio y la biblioteca estándar de Ki, incrustados en `ki` por embed.go
tests/               programas Ki de prueba: golden/chNN/*.ki y errors/chNN/*.ki
book/                (solo aquí) la prosa de cada capítulo y sus tarjetas de test
ci/                  ci-local.sh, session.sh, retag.sh, retag-all.sh y check-tags.sh
docs/                toolchain.md; research/ (los informes del diseño); superpowers/ (spec, planes y proceso)
.github/workflows/   la CI de GitHub
```

## Licencia

MIT. Consulta [LICENSE](LICENSE).
