# ASR Hermes на omarchy: исследование без изменений

Дата: 2026-10-11. Проверено через существующий SSH alias `omarchy`, пользователь `coder`, home `/home/coder`. На remote выполнялись только чтение файлов, поиск и системные запросы. Inference, загрузки, установки, перезапуски и изменение настроек не выполнялись. Секретные файлы `.env`, auth и токены не читались. Отчёт — единственная локальная запись этого исследования.

## Найденный кастомный ASR

Это пользовательский CLI adapter для **Parakeet TDT 0.6B v3 Q8 GGUF**, а не отдельная авторская нейросеть. Конфигурация Hermes:

- `/home/coder/.hermes/config.yaml:346`: STT включён.
- `/home/coder/.hermes/config.yaml:349`: выбран `provider: parakeet`.
- `/home/coder/.hermes/config.yaml:364`: именованный provider `parakeet`, `type: command`.
- `/home/coder/.hermes/config.yaml:366`: `/home/coder/.local/bin/hermes-parakeet-stt --model {model} {input_path}`.
- `/home/coder/.hermes/config.yaml:367`: `/home/coder/.local/share/nemo-speech/models/parakeet-tdt-0.6b-v3.q8_0.gguf`.
- `/home/coder/.hermes/config.yaml:368`: `format: txt`; строка 369 — timeout 120 секунд.

Файл модели присутствует, размер **713 975 456 байт (≈681 MiB)**, дата файла 7 августа. Это проверка наличия, не checksum или проверка качества модели.

Wrapper `/home/coder/.local/bin/hermes-parakeet-stt`:

- строки 6–11: принимает `--model`, игнорирует `--language` с комментарием об auto-detection Parakeet v3;
- строки 17–19: создаёт временную папку и регистрирует EXIT trap;
- строки 21–22: через ffmpeg преобразует входной файл в mono WAV, 16 kHz, PCM16;
- строки 24–25: `exec /home/coder/.local/bin/nemo-speech --quiet transcribe "$wav" --model "$model" --device cpu`.

Команда возвращает plaintext через stdout. Сам wrapper не запускает дополнительную LLM, не добавляет punctuation/ITN companion models и не отправляет сетевые запросы.

**Нюанс для будущего reuse:** `exec` заменяет shell процесс, поэтому зарегистрированный shell EXIT trap не выполнится после нормального завершения nemo-speech. Очистку этой временной директории нельзя считать гарантированной. В этом исследовании ничего не исправлялось и временные файлы не удалялись.

## Runtime и интерфейс

`/home/coder/.local/bin/nemo-speech` — symlink на `/home/coder/.local/share/nemo-speech/bin/nemo-speech`. `file` определяет native x86-64 ELF executable. `ldd` показывает локальные `libnemo_speech_asr.so`, `libggml.so.0`, `libggml-cpu.so.0`, `libggml-base.so.0`, системные libc/libstdc++/libgomp. Вызов Hermes явно CPU.

Установленная документация `/home/coder/.local/share/nemo-speech/share/doc/nemo-speech/README.md:1` называет runtime **NeMo-Speech.cpp**, строка 3 — native C++ runtime на ggml. Read-only вызов `nemo-speech --version` возвращает **nemo-speech 1.0.0**. Исходный checkout, точный upstream URL и commit бинарника этим ограниченным поиском не установлены; документация рядом с бинарником не доказывает его точный commit. `share/licenses/nemo-speech/LICENSE:1` и `NOTICE:2` содержат copyright NVIDIA 2026; лицензии конкретных companion моделей необходимо проверять отдельно.

Provider dispatcher `/home/coder/.hermes/hermes-agent/tools/transcription_command.py:68` принимает audio path, создаёт временную директорию и безопасно подставляет placeholders (строки 93–101). Извлечение transcript предпочитает непустой файл, затем stdout (строки 59–65). Ошибки и timeout превращаются в error result (103–112). Это подтверждает интерфейс, подходящий для reuse без HTTP server.

Сейчас ASR вызывается **по запросу отдельным процессом**. Процесс `nemo-speech` не обнаружен в `ps -eo comm`; отдельный ASR service не найден среди user services. Проверка не доказывает отсутствие любого HTTP сервера в системе: владельцы всех открытых портов не изучались.

Установленные docs описывают альтернативный `nemo-speech serve`, localhost:8080 и OpenAI-compatible audio API subset (`README.md:59–73`). Это **возможность runtime**, не подтверждение работающего endpoint. Docs `docs/asr/models.md:20–34` описывают Parakeet v3 как multilingual, self-punctuating, full-utterance-only; streaming для него rejected. Telegram voice notes целиком совместимы с такой схемой. Возможности текущего бинарника не проверялись inference или запуском сервера.

## Голосовые и форматирование в Hermes

Работает `hermes-gateway.service`: `ActiveState=active`, `SubState=running`, рабочая директория `/home/coder/.hermes`, unit `/home/coder/.config/systemd/user/hermes-gateway.service`. Не читались Environment/ExecStart, чтобы не вывести секреты. Checked source HEAD Hermes: `39faafb616`.

Telegram adapter `/home/coder/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py:6881–6887` кеширует voice как `.ogg`, audio как `.mp3`, затем передаёт event в gateway (6897). Gateway `/home/coder/.hermes/hermes-agent/gateway/run_inbound.py:2049–2073` вызывает настроенный ASR в `asyncio.to_thread`, при failure пробует local fallback; берёт `result['transcript']`, отклоняет пустой transcript и передаёт обычный текст в кавычках. Enrichment всех clips — строки 2075–2117; echo — 1525–1548.

В конфигурации есть `format_transcripts: true` (`config.yaml:348`), но поиск `rg -n --hidden "format_transcripts"` по текущему Python исходнику Hermes не обнаружил потребителя этого ключа. Поэтому **наличие этого флага не доказывает работу форматировщика**. Проверенный wrapper и участок gateway напрямую используют transcript. Parakeet сам ставит пунктуацию согласно installed docs, но редактирование абзацев, списков, самокоррекций и речевого мусора отдельной микромоделью не найдено.

Runtime docs `docs/cli.md:49–63` предлагают дополнительные VAD, punctuation (`--pnc-model`), ITN (`--itn-model-dir`), NMT. Эти аргументы отсутствуют в wrapper; в папке GGUF models найден только Parakeet. `docs/asr/models.md:180–189` описывают PnC как NeMo BERT token classifier, восстановление регистра и `. , ?`, конвертацию local `.nemo` в GGUF. `docs/asr/configuration.md:254–278` описывают ITN как Sparrowhawk `.far` grammars, перевод spoken-form в written-form, требующий сборки `NEMO_SPEECH_WITH_NORM=ON`. Наличие этой опции в установленной сборке не установлено. Punctuation/ITN ограниченнее желаемого Fluid Voice подобного форматирования: runtime документация сама по себе не подтверждает качество русского и английского форматирования.

## Ресурсы omarchy и локальные модели

Команды `lscpu`, `free -h`, `lspci`:

- Intel Core i5-1235U, x86_64, 10 cores / 12 threads, до 4.4 GHz; AVX2 доступен.
- RAM 7.5 GiB; на момент проверки 3.4 GiB used, 4.1 GiB available. Это снимок, не гарантированный бюджет сервиса.
- Swap 23 GiB, использовано около 2 GiB. Swap не следует считать памятью для низкой задержки.
- Intel Alder Lake-UP3 GT2 / Iris Xe integrated graphics; дискретная NVIDIA GPU не обнаружена. Установлены Vulkan Intel/Mesa пакеты, но GPU inference не проверялся.

`command -v ollama`, `llama-server`, `llama-cli` не нашли команды; `ps -eo comm` не нашёл llama/ollama; соответствующие user services не найдены. Ограниченный поиск GGUF в `/home/coder/.local/share`, `Downloads`, `Projects` обнаружил только Parakeet. Это не полный аудит всех возможных runtimes/контейнеров; содержимое контейнеров не исследовалось.

В Hugging Face cache есть каталоги Systran faster-whisper `base`, `base.en`, `small`, `small.en`, `tiny`; `istupakov/parakeet-tdt-0.6b-v3-onnx`, `istupakov/silero-vad-onnx`; ONNX embeddings Granite 30M English и mxbai-edge-colbert 17M. Наличие cache directory не доказывает полный checkpoint. Отдельно `/home/coder/.local/share/com.pais.handy/models/parakeet-tdt-0.6b-v3-int8` содержит ONNX модель; есть hyprwhspr runtime и установлен whispering-bin. Эти desktop ASR не являются подтверждённым Hermes backend.

Для отдельного форматировщика разумен небольшой quantized model CPU класса примерно 0.5–1.7B с ограниченным context и последовательным выполнением после ASR. Это оценка по памяти, **не измеренная скорость и не выбор модели**. Русскую грамматику, сохранение имён/чисел, запрет выполнения продиктованных инструкций и отсутствие потери смысла нужно оценить отдельно. Совместный постоянный resident ASR + LLM уменьшает свободную RAM; CPU нагрузка Hermes/Secretary уже присутствует. Начать с имеющегося Parakeet, сохраняя raw transcript и отдельно formatted transcript; при failure formatter отдавать raw.

## Команды исследования и пределы

Все remote команды выполнялись через `ssh omarchy`. Использовались `ls`, `rg --files`/`rg -n` с ограниченными каталогами и расширениями, `nl -ba`/`sed` для указанных участков, `lscpu`, `free -h`, `lspci`, `command -v`, `pacman -Q`, `ps -eo comm`, `systemctl --user list-units`/`show` только безопасных свойств, `ss -ltn`, `file`, `ldd`, `stat`, `readlink -f`, `git log -1`, `nemo-speech --version` (без загрузки модели).

Часть поиска в `.cache`/`.local/share/containers` дала Permission denied. Позднее контейнерные каталоги исключались; обход ограничений не выполнялся. Не читались `.env`, auth файлы, user histories, sessions/transcripts, полный config.yaml, полный systemd unit, environment или process command lines. Не запускались inference, benchmark, HTTP/ASR server, установщик, downloader или рестарт. Реальное качество, задержка, fallback работоспособность и бинарная совместимость установленной модели пока не проверены.
