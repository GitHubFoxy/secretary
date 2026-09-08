# Define the minimal Task state machine

Type: grilling
Status: resolved

## Question

Какие minimal lifecycle states отделяют Task, Worker binding и Attempt в SQLite-backed Secretary server?

Нужно определить поведение terminal Result, `dispatch_failed`, `interrupted`, Follow-up и Task closure без event sourcing.

## Answer

Task имеет lifecycle `dispatching`, `dispatch_failed`, `open`, `closing`, `closed`.

`dispatching` начинается до Node command. Accepted Dispatch создаёт Worker binding и переводит Task в `open`. Неудача до accepted Dispatch переводит её в `dispatch_failed` без binding. Secretary может повторить Dispatch той же Task, сохранив её identity; accepted retry создаёт первый binding.

Attempt принадлежит Worker binding и имеет lifecycle `starting`, `active`, затем terminal `succeeded`, `failed`, `canceled` или `interrupted`. Terminal Result завершает только Attempt. Task остаётся `open`, а Follow-up создаёт следующую Attempt через тот же binding.

Task closure из idle Task сразу переводит её в `closed`. Для active Attempt Task сначала становится `closing`, Node получает Cancel, terminal Result сохраняется, затем Task становится `closed`. История и archived Worker binding не удаляются.
