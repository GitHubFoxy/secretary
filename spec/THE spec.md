**Secretary решает проблему потери рабочего контекста при переключении между каналами и задачами.**

Он сохраняет одну общую переписку и может передать отдельное поручение  Worker (не постоянный, но может быть переиспользован, может быть постоянным, зависит от того как пользователь настроил скиллы для харнесса.). Пока Worker занят, человек продолжает разговор с Secretary; результат возвращается в ту же переписку. Не нужно вручную переносить контекст между чатами, сессиями и машинами или разыскивать результат в отдельных логах и инструментах.

I want to create a secretary* product. Users should be able to connect their chatGPT sub or Anthropic sub, and use Claude code and Codex directly so they wont get banned, kinda like t3 chat is doing that.
I want Secretary to be able to launch new session on diffrent machines. For example, Blender and 3d, currently ChatGPT leading in that space, and MacOS support helping with that, which means that for 3d stuff spawn Codex on MacOS (Codex currently has best Computer Use paired with MacOS its unmatched). Next example, for develment and programming, currently Opus 5.5 is the best, pair it with Linux machine, you get the best result. (In order to use Opus 5.5 with sub, it must be used inside Claude Code, pair it with Fast Linux filesystem, instead of MacOS which is slow (apfs) for most agentic workflows (git, npm, etc) PROOF: https://www.youtube.com/watch?v=4wVNFaFDIn8). 
Those are main Harnesses. Additional harnesses:
Third Example, simple queries, there we might use fx, its built in rust, its just a little bit faster. Browser usage for example, using it with axi skill (/Users/beruseruko/.config/opencode/skills/chrome-devtools-axi-local) It is intresting to use/try

Pi, OpenCode
I use pi all the time, its my main choice as harness. A lot of extensions, hooks a lot of customizionts.
OpenCode can Provide free models, and a lot of good features too. 

I think that Codex and CC (Claude code i will call it CC from now on) are Native Harnesses. So we will focus on those. But some benchmarks are still pointing out that Pi is better (Couse its so minimal, system prompt is 2,655 chars)
FX also feels nice, i am currently testing it out, its whole point is being embedded, need more testing to be able to tell does it fit in the product or not, nice to give user a choice tho. 
OpenCode is a good choice too.

What is secretary*? 
Its any of those harnesses. With profile.md\AGENTS.md and MCP (we decided that MCP will give all harnesses the same tools). We should do a proper research on what each harness capabilities are.  Can it do custom tools, and MCPs

So lets say i own a Codex (ChatGPT from now on) sub, and i own CC sub. 
### In Theory:

>codex gives me much more usage but dumber models. CC gives smarted, but less usage. 
>Secretary may work on CC (smarter, better undertands user INTENT (very important))

[[secretary sees worker report]]
[[secretary does not respond to worker report]]

Secretary is able to launch workers with read only capabilities 
its achieved via alias that leads to claude --tools read web, значит чтокогда стоит режим только чтение, агент физически не сможет изменить файлы. так же можно добавить проверку на каждый тул кол, это уже делает codex and CC smart accept i think 
У пользователя остается возможность открыть Worker thread в оригинальном харнессе через ghostty emulator lib 
Но также мы показываем основной функционал харнессов, как это сделано в t3code

# Перехват Workerа 

Как сравиться с рассинхроном между секретарем и Воркером?


Решение: два режима воркера, Managed и Native. См. [[Режимы воркера Managed и Native]]. Прежний подход (отслеживать сообщения пользователя в сессии воркера) отклонён, краткое описание в той же заметке.

# Skills

Now we need to talk about skills, since they are a big part of current agentinc developmang\engineering\ai usage in general. 
Единное место для управления скиллами для всех харнессов или отдельно.
[[skills]]

# Memory 

[[memory]]

---

# Installation and Authentication (First impression of the product)

Данное приложение можно использовать как из облака так и локально.
На данные момент ведется разработка именно локальной версии. 

Можно установить как на одну машину, локально
Так и на локальный сервер, который находится в одно wifi сети. ( Secretary и узлы могут работать в одной локальной сети )
А так же Secretary может работать например на VPS и быть подключен к вашим устройствам. (  Node enrollment / pairing — пользователь явно подключает машину к Secretary, а не полагается на одно лишь обнаружение устройства в сети. )

What setups could be done:
Secretary could be isntalled on:
Cloud: secretary.example
Local Machine
VPS
Phone

Same for Workers:
Cloud
Local
VPS
Phone

Both Worker and Secretary could be installed on 1 machine

EXAMPLE URLS!
macOS: https://secretary.example/install.sh
Linux: https://secretary.example/install.sh
Windows: https://secretary.example/install.ps1

OR

 Скачать Secretary для Mac
 https://secretary.example/download/Secretary.dmg

 Скачать Secretary для Windows
 https://secretary.example/download/Secretary-Setup.exe

 Скачать Secretary для Linux
 https://secretary.example/download/secretary.AppImage

GUI installation 


>Before installing do a proper checks of env

What harnesses are installed, what harness would user prefer to use\install, select with checks and click install, then 
Sing in With ChatGPT official btn or for Anthropic.
ask do we start on login and are we in the background? 




bash: secretary setup

# Migration
How to migrate from Cloud To Local?
# Available FrontEnds

## Fully customizable like deepseek harness Web interface, use from pc, or phone. 

## Phone App Android or IOS

## TUI

## PC GUI Mac, Windows, Linux
## Messangers
Telegram, Max?, WhatsApp, Slack, Discord.

# Notes:

Пользователь может выбрать на какой машине какой харнесс запустить.

Secretary uses Claude and Codex just like t3Code whice is legal. 
**T3 Code работает через официальный Claude Code и Claude Agent SDK, используя твою авторизацию по подписке.** В исходниках он вызывает SDK-функцию `query()` и передаёт путь к установленному Claude Code через `pathToClaudeCodeExecutable`. T3 выступает интерфейсом, а агентную работу выполняет Claude Code. [Исходный код интеграции](https://github.com/pingdotgg/t3code/blob/main/apps/server/src/orchestration-v2/Adapters/ClaudeAdapterV2.ts).

Схема выглядит так:

`T3 Code → Claude Agent SDK → Claude Code → Anthropic`

Ты входишь через `claude auth login`, а T3 использует конфигурацию и авторизацию Claude Code. Расход учитывается в лимитах твоей подписки. [Документация T3](https://github.com/pingdotgg/t3code/blob/main/docs/user/providers-claude.md).

**Почему это не приводит к бану само по себе:** сейчас Anthropic прямо разрешает использовать Agent SDK, `claude -p` и сторонние приложения с лимитами подписки. Это подтверждено в справке, обновлённой 7 октября 2026 года. [Официальное разъяснение Anthropic](https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan).

То есть здесь нет необходимости обходить ограничения или маскировать собственный API-клиент под Claude Code. Но это и не безусловная гарантия от блокировок: разрешён именно описанный способ использования, а правила Anthropic менялись.

I dont have antropic sub right now, but i do have API Token. So will test against that.
Главный риск API vs SUB был уточнен, обе компании разрешают такое использование и не идут против Terms of Usage


Secretary is for 1 person. Secretary Thread is the same for all channels ( There is 1 unified thread ). Worker должен просто решить поставленную задачу, для удобства, в названии сессии воркера SCRTR указываем имя сессии например: Разработка Проекта А: Убрать комментарии из кода.
Секретарь достаточно умный чтобы понять кому отправить follow-up 
К тому же он может перечислить какие Worker есть. MCP_LIST_WRKRS Если не понятно он уточнит.

Для разработки используется Git. Например если задача отцентровать див, то воркер склонирует репу, создаст worktree отцентрует, покажет результат только потом смерждит. (Это зависит от пользователя уже как он настроит окружение, скиллы, промты)

Workers threads могут существовать сколько угодно, можно их убрать в архив, чтобы не мазолили глаза /settle в телеграм в конкретный топик или по кнопке. Чтобы удалить тред можно попросить Секретаря. или написать просто /delete, после этого вас попросят подтврдить, после подстверждения тред удаляется.
В pi например очень легко взять открыть дерево и изменить промт. CC rewind, Codex Rewind. Такой функционал доступен пользователя при открытие отдельного Worker Thread. Worker thread является просто сессией конкретного Харнесса, все функции харнесса доступны пользователю. 

Не задавать легальные и юридические вопросы. Они не относятся к спеку.

