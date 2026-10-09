# Defer Project management from the core

Type: grilling
Status: resolved

## Question

Должны ли `create_project`, repository registry, checkout lease и worktree lifecycle входить в Secretary core или первый thin slice?

## Answer

Нет. Первый core не знает Project, Project checkout или `create_project`.

Worker с full access может сам выполнить `git clone` или `git worktree add` по обычному Task instruction. Core сохраняет только Task, Worker binding, Node и runtime session. Это достаточно для первого slice.

Если позже понадобится надёжно переиспользовать repository между Workers и Nodes, управлять lease, isolated worktree и Git credentials, будет спроектирован отдельный Projects capability. Generic plugin system ради этого сейчас не создаётся.
