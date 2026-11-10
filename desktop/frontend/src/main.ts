import { Events } from "@wailsio/runtime";
import { AppService } from "../bindings/github.com/AndrewGrayYouNeeK/Cloudripper/desktop";
import type {
    BillReport,
    LaterItem,
    ProviderStatus,
    Report,
    WatchState,
} from "../bindings/github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime/models";

type Screen = "demo" | "providers" | "inventory" | "spend" | "recommendations" | "bills" | "watch" | "later";

const panel = document.querySelector("#panel") as HTMLElement;
const statusLine = document.querySelector("#status") as HTMLElement;
const themeButton = document.querySelector("#theme") as HTMLButtonElement;
let screen: Screen = "demo";
let busy = false;
let live = false;
let allRegions = false;
let csvPath = "";
let watchMinutes = 15;
let watchPercent = 25;

function setStatus(text: string): void {
    statusLine.textContent = text;
}

function clear(node: HTMLElement): void {
    while (node.firstChild) {
        node.removeChild(node.firstChild);
    }
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, text?: string, className?: string): HTMLElementTagNameMap[K] {
    const node = document.createElement(tag);
    if (text !== undefined) {
        node.textContent = text;
    }
    if (className) {
        node.className = className;
    }
    return node;
}

function button(label: string, onClick: () => void, primary = false): HTMLButtonElement {
    const node = el("button", label);
    node.type = "button";
    if (primary) {
        node.className = "primary";
    }
    node.addEventListener("click", onClick);
    return node;
}

function list(items: string[] | null | undefined): HTMLElement {
    const ul = el("ul", undefined, "notes");
    for (const item of items ?? []) {
        ul.append(el("li", item));
    }
    return ul;
}

function table(headers: string[], rows: string[][]): HTMLElement {
    const wrap = el("div", undefined, "table-wrap");
    const tableEl = document.createElement("table");
    const thead = document.createElement("thead");
    const headRow = document.createElement("tr");
    for (const header of headers) {
        headRow.append(el("th", header));
    }
    thead.append(headRow);
    const tbody = document.createElement("tbody");
    if (rows.length === 0) {
        const tr = document.createElement("tr");
        const td = el("td", "No rows.");
        td.colSpan = headers.length;
        tr.append(td);
        tbody.append(tr);
    }
    for (const row of rows) {
        const tr = document.createElement("tr");
        for (const cell of row) {
            tr.append(el("td", cell));
        }
        tbody.append(tr);
    }
    tableEl.append(thead, tbody);
    wrap.append(tableEl);
    return wrap;
}

function banner(text: string | undefined): HTMLElement | null {
    if (!text) {
        return null;
    }
    const node = el("p", text, "banner");
    node.setAttribute("role", "note");
    return node;
}

function stats(pairs: Array<[string, string]>): HTMLElement {
    const wrap = el("div", undefined, "stats");
    for (const [label, value] of pairs) {
        const card = el("div", undefined, "stat");
        card.append(el("p", label), el("strong", value));
        wrap.append(card);
    }
    return wrap;
}

function renderReport(report: Report, showRecs: boolean): void {
    clear(panel);
    panel.append(el("h1", report.title, undefined));
    const note = banner(report.banner);
    if (note) {
        panel.append(note);
    }
    panel.append(stats([
        ["Estimated total", report.estimated_display],
        ["Actual spend", report.actual_display],
        ["Unpriced rows", String(report.unpriced_count)],
    ]));
    if (report.savings_note) {
        panel.append(el("p", report.savings_note, "note"));
    }
    if (report.empty_message) {
        panel.append(el("p", report.empty_message));
    }
    panel.append(el("h2", "Resources"));
    panel.append(table(
        ["Provider", "Type", "ID", "Name", "Region", "Status", "Spec", "USD", "Source"],
        (report.resources ?? []).map((row) => [
            row.provider, row.type, row.id, row.name, row.region, row.status, row.spec ?? "", row.cost_display, row.cost_source ?? "",
        ]),
    ));
    if (showRecs) {
        panel.append(el("h2", "Recommendations"));
        if (report.potential_savings_display) {
            panel.append(el("p", "Heuristic total: " + report.potential_savings_display, "note"));
        }
        panel.append(table(
            ["Action", "Provider", "Type", "ID", "Savings", "Spec", "Reason"],
            (report.recommendations ?? []).map((row) => [
                row.action, row.provider, row.type, row.id, row.savings_display, row.suggested_spec ?? "", row.reason,
            ]),
        ));
    }
    const warnings = report.warnings ?? [];
    const skipped = report.skipped ?? [];
    if (warnings.length > 0) {
        panel.append(el("h2", "Warnings"));
        panel.append(list(warnings));
    }
    if (skipped.length > 0) {
        panel.append(el("h2", "Not configured"));
        panel.append(list(skipped));
    }
}

function renderBills(report: BillReport): void {
    clear(panel);
    panel.append(el("h1", report.title));
    const note = banner(report.banner);
    if (note) {
        panel.append(note);
    }
    panel.append(el("p", report.note, "note"));
    if (report.empty_message) {
        panel.append(el("p", report.empty_message));
    }
    const headers = report.sample
        ? ["Sample", "Provider", "Account", "Amount", "Date", "Configured", "Status", "Source"]
        : ["Provider", "Account", "Amount", "Date", "Configured", "Status", "Source"];
    panel.append(table(headers, (report.findings ?? []).map((row) => {
        const cells = [row.provider, row.account ?? "", row.amount, row.date ?? "", row.configured, row.status, row.source];
        return report.sample ? ["fictional", ...cells] : cells;
    })));
}

function renderProviders(rows: ProviderStatus[]): void {
    clear(panel);
    panel.append(el("h1", "Provider status"));
    panel.append(el("p", "Status uses the same local credential chain and environment variables as the CLI. Secret values are not shown or stored.", "note"));
    const cards = el("div", undefined, "cards");
    for (const row of rows) {
        const card = el("article", undefined, "card");
        card.append(el("h3", row.name));
        const state = el("p", row.status);
        state.className = "status-" + row.status;
        card.append(state);
        if (row.identity) {
            card.append(el("p", row.identity));
        }
        card.append(el("p", row.detail, "note"));
        cards.append(card);
    }
    panel.append(cards);
}

function renderLater(items: LaterItem[]): void {
    clear(panel);
    panel.append(el("h1", "Coming later"));
    panel.append(el("p", "This version is read-only. These commands stay in the CLI and are not wired to any button here.", "note"));
    const cards = el("div", undefined, "cards");
    for (const item of items) {
        const card = el("article", undefined, "card");
        card.append(el("h3", item.name), el("p", item.detail));
        cards.append(card);
    }
    panel.append(cards);
}

function renderWatch(state: WatchState): void {
    clear(panel);
    panel.append(el("h1", "Watch"));
    panel.append(el("p", state.note, "note"));
    const tools = el("div", undefined, "toolbar");
    const minutes = document.createElement("input");
    minutes.type = "number";
    minutes.min = "1";
    minutes.step = "1";
    minutes.value = String(watchMinutes);
    minutes.id = "watch-minutes";
    minutes.addEventListener("change", () => {
        watchMinutes = Number(minutes.value);
    });
    const minutesLabel = el("label", "Interval (minutes)");
    minutesLabel.htmlFor = minutes.id;
    const percent = document.createElement("input");
    percent.type = "number";
    percent.min = "1";
    percent.step = "1";
    percent.value = String(watchPercent);
    percent.id = "watch-percent";
    percent.addEventListener("change", () => {
        watchPercent = Number(percent.value);
    });
    const percentLabel = el("label", "Threshold percent");
    percentLabel.htmlFor = percent.id;
    tools.append(
        minutesLabel, minutes, percentLabel, percent,
        button("Sample once", () => void runWatchOnce(), true),
        button(state.running ? "Running" : "Start", () => void runWatchStart(), !state.running),
        button("Stop", () => void runWatchStop()),
    );
    panel.append(tools);
    panel.append(stats([
        ["Slack", state.slack_configured ? "configured" : "not configured"],
        ["Email", state.email_configured ? "configured" : "not configured"],
        ["OS notifications", state.os_notifications ? "on" : "off"],
        ["Last total", state.last_total || "none yet"],
    ]));
    if (state.last_error) {
        panel.append(el("p", state.last_error));
    }
    panel.append(el("h2", "Alerts"));
    panel.append(table(
        ["When", "Scope", "Message"],
        (state.alerts ?? []).map((alert) => [alert.at, alert.scope, alert.message]),
    ));
    if ((state.log ?? []).length > 0) {
        panel.append(el("h2", "Log"));
        panel.append(list(state.log));
    }
}

function toolbar(exportScreen: string, includeLive: boolean): HTMLElement {
    const bar = el("div", undefined, "toolbar");
    if (includeLive) {
        const liveBox = document.createElement("input");
        liveBox.type = "checkbox";
        liveBox.checked = live;
        liveBox.id = "live-toggle";
        liveBox.addEventListener("change", () => {
            live = liveBox.checked;
            void showScreen(screen);
        });
        const liveLabel = el("label", "Use my cloud accounts");
        liveLabel.className = "check";
        liveLabel.htmlFor = liveBox.id;
        liveLabel.prepend(liveBox);
        const regionBox = document.createElement("input");
        regionBox.type = "checkbox";
        regionBox.checked = allRegions;
        regionBox.id = "regions-toggle";
        regionBox.disabled = !live;
        regionBox.addEventListener("change", () => {
            allRegions = regionBox.checked;
            void showScreen(screen);
        });
        const regionLabel = el("label", "All enabled AWS regions");
        regionLabel.className = "check";
        regionLabel.htmlFor = regionBox.id;
        regionLabel.prepend(regionBox);
        bar.append(liveLabel, regionLabel);
    }
    for (const format of ["html", "csv", "json"] as const) {
        bar.append(button("Export " + format.toUpperCase(), () => void exportView(exportScreen, format)));
    }
    return bar;
}

async function showScreen(next: Screen): Promise<void> {
    screen = next;
    for (const node of document.querySelectorAll<HTMLButtonElement>("[data-screen]")) {
        if (node.dataset.screen === next) {
            node.setAttribute("aria-current", "page");
        } else {
            node.removeAttribute("aria-current");
        }
    }
    setStatus("Loading…");
    busy = true;
    try {
        if (next === "demo") {
            const report = await AppService.Demo();
            renderReport(report, true);
            panel.prepend(toolbar("demo", false));
            setStatus("Fictional sample. No credentials and no network.");
            return;
        }
        if (next === "providers") {
            const rows = await AppService.Providers();
            renderProviders(rows ?? []);
            setStatus("Credential check finished.");
            return;
        }
        if (next === "inventory" || next === "spend" || next === "recommendations") {
            const report = next === "inventory"
                ? await AppService.Inventory(live, allRegions)
                : next === "spend"
                    ? await AppService.Spend(live, allRegions)
                    : await AppService.Recommendations(live, allRegions);
            renderReport(report, next === "recommendations");
            panel.prepend(toolbar(next, true));
            setStatus(live ? "Live read-only scan." : "Showing the fictional sample.");
            return;
        }
        if (next === "bills") {
            const report = await AppService.Bills(!live, live ? csvPath : "");
            renderBills(report);
            const bar = toolbar("bills", true);
            bar.append(button("Choose CSV", () => void chooseCSV()));
            panel.prepend(bar);
            setStatus(live ? (csvPath ? "Using the selected CSV." : "Using configured mail, if any.") : "Embedded fictional card export.");
            return;
        }
        if (next === "watch") {
            renderWatch(await AppService.WatchState());
            setStatus("Watch does not change cloud resources.");
            return;
        }
        const later = await AppService.ComingLater();
        renderLater(later ?? []);
        setStatus("");
    } catch (err) {
        clear(panel);
        panel.append(el("h1", "Something went wrong"), el("p", errorText(err)));
        setStatus("The last action failed.");
    } finally {
        busy = false;
    }
}

async function exportView(exportScreen: string, format: "html" | "csv" | "json"): Promise<void> {
    if (busy) {
        return;
    }
    setStatus("Choose where to save the file…");
    try {
        const path = await AppService.Export(exportScreen, format, live && exportScreen !== "demo", allRegions, csvPath);
        setStatus(path ? "Saved " + path : "Export cancelled.");
    } catch (err) {
        setStatus(errorText(err));
    }
}

async function chooseCSV(): Promise<void> {
    try {
        const path = await AppService.ChooseCSV();
        if (!path) {
            setStatus("CSV selection cancelled.");
            return;
        }
        csvPath = path;
        live = true;
        await showScreen("bills");
    } catch (err) {
        setStatus(errorText(err));
    }
}

async function runWatchOnce(): Promise<void> {
    setStatus("Taking one sample…");
    try {
        renderWatch(await AppService.WatchOnce(watchMinutes, watchPercent));
        setStatus("Sample finished.");
    } catch (err) {
        setStatus(errorText(err));
    }
}

async function runWatchStart(): Promise<void> {
    setStatus("Watch started.");
    try {
        renderWatch(await AppService.WatchStart(watchMinutes, watchPercent));
    } catch (err) {
        setStatus(errorText(err));
    }
}

async function runWatchStop(): Promise<void> {
    renderWatch(await AppService.WatchStop());
    setStatus("Watch stopped.");
}

function errorText(err: unknown): string {
    if (err instanceof Error) {
        return err.message;
    }
    return String(err);
}

function applyTheme(theme: string): void {
    if (theme === "light" || theme === "dark") {
        document.documentElement.dataset.theme = theme;
    } else {
        delete document.documentElement.dataset.theme;
    }
    const dark = theme === "dark" || (theme !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    themeButton.setAttribute("aria-pressed", dark ? "true" : "false");
    themeButton.textContent = dark ? "Dark theme" : "Light theme";
}

themeButton.addEventListener("click", () => {
    const current = document.documentElement.dataset.theme
        ?? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    const next = current === "dark" ? "light" : "dark";
    localStorage.setItem("cloudripper-theme", next);
    applyTheme(next);
});

const screenOrder: Screen[] = ["demo", "providers", "inventory", "spend", "recommendations", "bills", "watch", "later"];

for (const node of document.querySelectorAll<HTMLButtonElement>("[data-screen]")) {
    node.addEventListener("click", () => {
        const next = node.dataset.screen as Screen;
        void showScreen(next);
    });
}

document.addEventListener("keydown", (event) => {
    if (event.metaKey || event.ctrlKey || event.altKey) {
        return;
    }
    if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) {
        return;
    }
    const index = Number(event.key) - 1;
    if (index >= 0 && index < screenOrder.length) {
        event.preventDefault();
        void showScreen(screenOrder[index]);
    }
});

Events.On("watch:update", (event) => {
    if (screen !== "watch") {
        return;
    }
    const data = event.data as WatchState;
    renderWatch(data);
});

applyTheme(localStorage.getItem("cloudripper-theme") ?? "system");
void showScreen("demo");
