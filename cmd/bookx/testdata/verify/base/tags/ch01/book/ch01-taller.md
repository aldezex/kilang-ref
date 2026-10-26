---
chapter: 1
title: El taller
part: I
hours: 8
milestone: ki --version
---

## Objetivo

Dejas el taller listo.

## Tests primero

{{cards}}

{{reftest T01.01}}

## Implementación

### Sesión 1: el comando (≈ 3 h)

{{file ch01-s1 go.mod}}

{{listing ch01-s1 driver.main}}

{{listing ch01-s1 driver.usage}}

{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}

:::exercise
Cambia el código de salida.
:::

:::solution
Devuelve 1.
:::
