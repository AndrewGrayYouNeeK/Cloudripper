import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const html = readFileSync(new URL("../public/index.html", import.meta.url), "utf8");
const demoOutput = readFileSync(new URL("./demo-output.txt", import.meta.url), "utf8").replace(/\n$/, "");
// The captured `cloudripper demo` transcript says the figures are not real savings.
// That substring is banned only in marketing copy, where it would overclaim.
const marketing = html.replace(/<span data-demo-transcript>[\s\S]*?<\/span>/g, "");

test("landing copy matches the CLI", () => {
  for (const needle of [
    "AWS, GCP, Azure and Cloudflare",
    "Runs on your machine",
    "Your cloud data is never sent to us",
    "Billing Read",
    "unpriced",
    "scheduled scans",
    "multi-account",
    "team invites",
    "--apply",
    "CHAOS_ENDPOINT",
  ]) {
    assert.ok(html.includes(needle), `missing ${needle}`);
  }
  for (const banned of [
    "not estimates",
    "no data leaving your machine",
    "we'll email you once",
    "No spam, one email",
    "Real savings",
  ]) {
    assert.equal(marketing.toLowerCase().includes(banned.toLowerCase()), false, `still says ${banned}`);
  }
});

test("download walkthrough uses the v0.2.0 release and the demo command", () => {
  const files = [
    "cloudripper_0.2.0_darwin_amd64.tar.gz",
    "cloudripper_0.2.0_darwin_arm64.tar.gz",
    "cloudripper_0.2.0_linux_amd64.tar.gz",
    "cloudripper_0.2.0_linux_arm64.tar.gz",
    "cloudripper_0.2.0_windows_amd64.zip",
    "cloudripper_0.2.0_windows_arm64.zip",
    "checksums.txt",
  ];
  for (const file of files) {
    assert.ok(
      html.includes(`https://github.com/AndrewGrayYouNeeK/Cloudripper/releases/latest/download/${file}`),
      `missing ${file}`,
    );
  }
  assert.equal((html.match(/<button type="button" role="tab"/g) || []).length, 3);
  assert.equal((html.match(/role="tabpanel"/g) || []).length, 3);
  assert.ok(html.includes('role="tablist"'));
  assert.ok(html.includes('aria-label="Operating system"'));
  assert.ok(html.includes("navigator.userAgentData"));
  assert.ok(html.includes("xattr -d com.apple.quarantine ./cloudripper"));
  assert.ok(html.includes("chmod +x ./cloudripper"));
  assert.ok(html.includes("More info, then Run anyway"));
  assert.ok(html.includes("System Settings &gt; Privacy &amp; Security &gt; Open Anyway"));
  assert.ok(html.includes("sudo mv cloudripper /usr/local/bin/"));
  assert.ok(html.includes("Expand-Archive -Path .\\cloudripper_0.2.0_windows_amd64.zip -DestinationPath ."));
  assert.ok(html.includes("cloudripper demo"));
  assert.ok(html.includes("A desktop app is coming soon."));
  assert.ok(html.includes("CloudRipper is a command-line tool for computers."));
  assert.ok(html.includes('id="copy-link"'));
  assert.ok(html.includes('id="share-link"'));
  assert.ok(html.includes("prefers-reduced-motion"));
  assert.equal(html.includes("/actions/"), false);
  assert.equal(html.includes("github.com/AndrewGrayYouNeeK/Cloudripper/suites"), false);
  const transcript = html.match(/<span data-demo-transcript>([\s\S]*?)<\/span>/);
  assert.ok(transcript, "missing captured demo transcript");
  assert.equal(transcript[1], demoOutput);
  assert.ok(transcript[1].startsWith("FICTIONAL SAMPLE DATA."));
  assert.ok(transcript[1].includes("Found 16 fictional resources:"));
  assert.ok(transcript[1].endsWith("Recommendations only. The demo does not apply changes."));

  const windows = html.slice(html.indexOf('id="panel-windows"'), html.indexOf('id="panel-macos"'));
  const macos = html.slice(html.indexOf('id="panel-macos"'), html.indexOf('id="panel-linux"'));
  const linux = html.slice(html.indexOf('id="panel-linux"'), html.indexOf('class="desktop-soon"'));
  assert.equal(windows.includes("sudo mv"), false);
  assert.equal(windows.includes("chmod +x"), false);
  assert.ok(windows.includes("curl.exe -LO"));
  assert.equal(macos.includes("chmod +x"), false);
  assert.ok(macos.includes("xattr -d com.apple.quarantine ./cloudripper"));
  assert.ok(linux.includes("chmod +x ./cloudripper"));
  assert.equal(linux.includes("curl.exe"), false);
  for (const panel of [windows, macos, linux]) {
    assert.ok(panel.includes(">cloudripper demo<"));
  }
});
