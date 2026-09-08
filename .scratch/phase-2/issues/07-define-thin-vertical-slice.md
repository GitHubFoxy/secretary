# Define the first thin vertical slice

Type: grilling
Status: resolved

## Question

Какой один end-to-end scenario является acceptance proof независимого Secretary до добавления второго client, remote Node и sandbox?

Нужно выбрать минимальный набор product behaviour, который обязан работать вместе, и явно отложить всё остальное.

## Answer

Acceptance proof:

```text
web owner login
→ Secretary создаёт local Codex Worker без Project
→ acknowledgement с Worker link
→ Worker observer получает live activity
→ owner отправляет Steering и нажимает Stop
→ terminal Result verbatim появляется в Personal Conversation
```

Первый slice не включает `create_project`, repository registry, clone control-plane, второй client, remote Node, sandbox, account linking, activity replay или queue для нескольких Workers.
