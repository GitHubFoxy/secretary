# Preserve Task after Dispatch failure

Type: grilling
Status: resolved

## Question

Что происходит с Task, если Secretary уже решил его создать, но Node недоступен или Codex не стартует до accepted Dispatch?

## Answer

Task сохраняется в Personal Conversation со status `dispatch_failed`. Worker binding не создаётся. Server не повторяет Dispatch автоматически; Secretary может принять решение о новом Dispatch позднее.
