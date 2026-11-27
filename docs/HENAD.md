# Henad: offline AI insights

<img src="henad-logo.jpg" alt="Henad" width="360">

*The architecture of understanding.*

Henad is an optional assistant in CloudRipper. It explains cost spikes, suggests savings, and answers questions about your inventory. It runs **only** on the [Ollama](https://ollama.com) model on your own computer. **Nothing is sent off the machine**, and it is **off by default**.

## What it reads, and where it goes

| | |
|---|---|
| **Reads** | The CloudRipper report you ask about: resource names, IDs, types, regions, status, sizes, estimated and actual costs, and savings recommendations. For spikes, it also reads recent cost-watch samples (`cloudripper watch --state`). It never reads cloud credentials. |
| **Sends to** | Ollama on this computer only: `http://localhost:11434` (`CLOUDRIPPER_HENAD_OLLAMA` can change the port, and must still be `localhost`, `127.0.0.1` or `::1`). |
| **Never** | Any cloud AI service, the CloudRipper Pro server, a third party, or telemetry. |

How that is enforced in code (`internal/henad`):

- The client refuses any Ollama URL whose host is not a loopback address.
- It ignores `HTTP_PROXY` / `HTTPS_PROXY`, so a proxy cannot carry the request off the machine.
- It does not follow redirects.
- When Henad is off, CloudRipper checks that before it reads or scans anything for Henad.

## Turning it on

Henad stays off until you read a warning and **type `I understand`** (case doesn't matter; spaces around it are ignored). Anything else cancels.

### Desktop app

Open the **Henad** screen and flip the switch. A warning window explains what you are turning on, that it uses the local Ollama model, what it reads, and that nothing is sent off the machine. **Confirm** stays disabled until the text field says `I understand`. **Cancel** or Esc leaves Henad off. The app checks the phrase again on the Go side, so the window alone cannot turn it on.

### CLI

```console
$ cloudripper henad enable

  ⚠  Turn on Henad?

  • You're turning on Henad, an AI assistant that explains cost spikes, suggests savings, and answers questions about your cloud inventory.
  • Henad uses the Ollama model on this computer (http://localhost:11434). CloudRipper only talks to Ollama on localhost; it never calls a cloud AI service.
  • What it reads: the CloudRipper report you ask about (...) and recent cost-watch samples. It does not read your cloud credentials.
  • Nothing is sent off this machine. No CloudRipper server, no third party, no telemetry.
  • You can turn it off at any time: "cloudripper henad disable", or the Henad switch in the desktop app.

  Type "I understand" to turn on Henad (anything else cancels): I understand

  Henad is on. It uses local Ollama only; nothing leaves this machine.
```

For scripts, `cloudripper henad enable --i-understand` turns it on without the prompt (the warning still prints). The global `--yes` flag is **not** consent for Henad. Without a terminal and without `--i-understand`, `enable` refuses. `--model <name>` picks an Ollama model; otherwise Henad uses the first installed chat model.

The choice is saved to `<user config dir>/cloudripper/henad.json` (for example `~/.config/cloudripper/henad.json` on Linux, `~/Library/Application Support/cloudripper/henad.json` on macOS, `%AppData%\cloudripper\henad.json` on Windows) with owner-only permissions. `CLOUDRIPPER_HENAD_CONFIG` overrides the path.

## Turning it off

`cloudripper henad disable`, or flip the switch off in the desktop app.

## Using it

```bash
ollama pull llama3.2                  # once, any chat model works
cloudripper henad status               # on/off, Ollama running, which model
cloudripper henad savings              # biggest savings first
cloudripper henad spikes --watch-state ~/.cloudripper-watch.json
cloudripper henad ask "Which resources cost the most?"
cloudripper henad ask "What is idle?" --demo   # the fictional sample, no cloud calls
```

`ask`, `savings` and `spikes` scan your clouds the same way the desktop app's live view does (add `--all-regions` for every AWS region), or use the fictional sample with `--demo`. `--json` prints the answer as JSON. In the desktop app, the Henad screen has **Explain cost spikes**, **Suggest savings**, and a question box, and the "Use my cloud accounts" checkbox switches between the sample and a live scan.

If Ollama isn't running, Henad says so (`Start the Ollama app or run ollama serve`). If no chat model is installed, it says `ollama pull llama3.2`.

Answers come from a small local model and can be wrong. They never change anything: `optimize --apply` still asks before it touches a resource.
