# Nous: offline AI insights

<img src="nous-logo.jpg" alt="Nous" width="360">

*The architecture of understanding.*

Nous is an optional assistant in CloudRipper. It explains cost spikes, suggests savings, and answers questions about your inventory. It runs **only** on the [Ollama](https://ollama.com) model on your own computer. **Nothing is sent off the machine**, and it is **off by default**.

## What it reads, and where it goes

| | |
|---|---|
| **Reads** | The CloudRipper report you ask about: resource names, IDs, types, regions, status, sizes, estimated and actual costs, and savings recommendations. For spikes, it also reads recent cost-watch samples (`cloudripper watch --state`). It never reads cloud credentials. |
| **Sends to** | Ollama on this computer only: `http://localhost:11434` (`CLOUDRIPPER_NOUS_OLLAMA` can change the port, and must still be `localhost`, `127.0.0.1` or `::1`). |
| **Never** | Any cloud AI service, the CloudRipper Pro server, a third party, or telemetry. |

How that is enforced in code (`internal/nous`):

- The client refuses any Ollama URL whose host is not a loopback address.
- It ignores `HTTP_PROXY` / `HTTPS_PROXY`, so a proxy cannot carry the request off the machine.
- It does not follow redirects.
- When Nous is off, CloudRipper checks that before it reads or scans anything for Nous.

## Turning it on

Nous stays off until you read a warning and **type `I understand`** (case doesn't matter; spaces around it are ignored). Anything else cancels.

### Desktop app

Open the **Nous** screen and flip the switch. A warning window explains what you are turning on, that it uses the local Ollama model, what it reads, and that nothing is sent off the machine. **Confirm** stays disabled until the text field says `I understand`. **Cancel** or Esc leaves Nous off. The app checks the phrase again on the Go side, so the window alone cannot turn it on.

### CLI

```console
$ cloudripper nous enable

  ⚠  Turn on Nous?

  • You're turning on Nous, an AI assistant that explains cost spikes, suggests savings, and answers questions about your cloud inventory.
  • Nous uses the Ollama model on this computer (http://localhost:11434). CloudRipper only talks to Ollama on localhost; it never calls a cloud AI service.
  • What it reads: the CloudRipper report you ask about (...) and recent cost-watch samples. It does not read your cloud credentials.
  • Nothing is sent off this machine. No CloudRipper server, no third party, no telemetry.
  • You can turn it off at any time: "cloudripper nous disable", or the Nous switch in the desktop app.

  Type "I understand" to turn on Nous (anything else cancels): I understand

  Nous is on. It uses local Ollama only; nothing leaves this machine.
```

For scripts, `cloudripper nous enable --i-understand` turns it on without the prompt (the warning still prints). The global `--yes` flag is **not** consent for Nous. Without a terminal and without `--i-understand`, `enable` refuses. `--model <name>` picks an Ollama model; otherwise Nous uses the first installed chat model.

The choice is saved to `<user config dir>/cloudripper/nous.json` (for example `~/.config/cloudripper/nous.json` on Linux, `~/Library/Application Support/cloudripper/nous.json` on macOS, `%AppData%\cloudripper\nous.json` on Windows) with owner-only permissions. `CLOUDRIPPER_NOUS_CONFIG` overrides the path.

## Turning it off

`cloudripper nous disable`, or flip the switch off in the desktop app.

## Using it

```bash
ollama pull llama3.2                  # once, any chat model works
cloudripper nous status               # on/off, Ollama running, which model
cloudripper nous savings              # biggest savings first
cloudripper nous spikes --watch-state ~/.cloudripper-watch.json
cloudripper nous ask "Which resources cost the most?"
cloudripper nous ask "What is idle?" --demo   # the fictional sample, no cloud calls
```

`ask`, `savings` and `spikes` scan your clouds the same way the desktop app's live view does (add `--all-regions` for every AWS region), or use the fictional sample with `--demo`. `--json` prints the answer as JSON. In the desktop app, the Nous screen has **Explain cost spikes**, **Suggest savings**, and a question box, and the "Use my cloud accounts" checkbox switches between the sample and a live scan.

If Ollama isn't running, Nous says so (`Start the Ollama app or run ollama serve`). If no chat model is installed, it says `ollama pull llama3.2`.

Answers come from a small local model and can be wrong. They never change anything: `optimize --apply` still asks before it touches a resource.
