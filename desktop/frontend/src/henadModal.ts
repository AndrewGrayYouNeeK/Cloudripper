// The warning window shown before Henad is turned on. DOM only, no Wails
// calls, so it can be previewed on its own. Confirm stays disabled until the
// user types the phrase exactly (see henadGate.ts).
import { confirmEnabled } from "./henadGate";

export type HenadWarning = {
    title: string;
    lines: string[];
    phrase: string;
    markSrc?: string;
};

/** Resolves with the typed phrase on Confirm, or null on Cancel / Escape. */
export function openHenadWarning(warning: HenadWarning): Promise<string | null> {
    return new Promise((resolve) => {
        const previous = document.activeElement as HTMLElement | null;
        const overlay = document.createElement("div");
        overlay.className = "modal-overlay";
        const dialog = document.createElement("div");
        dialog.className = "modal";
        dialog.setAttribute("role", "alertdialog");
        dialog.setAttribute("aria-modal", "true");
        dialog.setAttribute("aria-labelledby", "henad-warning-title");
        dialog.setAttribute("aria-describedby", "henad-warning-body");

        const head = document.createElement("div");
        head.className = "modal-head";
        if (warning.markSrc) {
            const img = document.createElement("img");
            img.src = warning.markSrc;
            img.alt = "";
            img.className = "henad-mark";
            head.append(img);
        }
        const title = document.createElement("h2");
        title.id = "henad-warning-title";
        title.textContent = "⚠ " + warning.title;
        head.append(title);

        const body = document.createElement("ul");
        body.id = "henad-warning-body";
        body.className = "modal-list";
        for (const line of warning.lines) {
            const li = document.createElement("li");
            li.textContent = line;
            body.append(li);
        }

        const label = document.createElement("label");
        label.htmlFor = "henad-phrase";
        label.className = "modal-label";
        label.textContent = `Type "${warning.phrase}" to turn on Henad`;
        const input = document.createElement("input");
        input.type = "text";
        input.id = "henad-phrase";
        input.autocomplete = "off";
        input.spellcheck = false;
        input.placeholder = warning.phrase;

        const actions = document.createElement("div");
        actions.className = "modal-actions";
        const cancel = document.createElement("button");
        cancel.type = "button";
        cancel.textContent = "Cancel";
        const confirm = document.createElement("button");
        confirm.type = "button";
        confirm.className = "primary";
        confirm.id = "henad-confirm";
        confirm.textContent = "Confirm";
        confirm.disabled = true;
        actions.append(cancel, confirm);

        dialog.append(head, body, label, input, actions);
        overlay.append(dialog);
        document.body.append(overlay);

        const finish = (value: string | null) => {
            overlay.remove();
            document.removeEventListener("keydown", onKey, true);
            previous?.focus();
            resolve(value);
        };
        const update = () => {
            confirm.disabled = !confirmEnabled(input.value, false);
        };
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                finish(null);
            } else if (event.key === "Enter" && document.activeElement === input && !confirm.disabled) {
                event.preventDefault();
                finish(input.value);
            } else if (event.key === "Tab") {
                // Keep focus inside the window.
                const order = [input, cancel, confirm].filter((n) => !n.disabled);
                const i = order.indexOf(document.activeElement as HTMLInputElement & HTMLButtonElement);
                event.preventDefault();
                const next = event.shiftKey ? order[(i - 1 + order.length) % order.length] : order[(i + 1) % order.length];
                next.focus();
            }
            event.stopPropagation();
        };
        input.addEventListener("input", update);
        cancel.addEventListener("click", () => finish(null));
        confirm.addEventListener("click", () => {
            if (!confirm.disabled) {
                finish(input.value);
            }
        });
        document.addEventListener("keydown", onKey, true);
        input.focus();
    });
}
