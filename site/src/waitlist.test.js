import assert from "node:assert/strict";
import test from "node:test";
import worker from "./index.js";

function kv(initial = {}) {
  const store = new Map(Object.entries(initial));
  return {
    store,
    async get(key) {
      return store.has(key) ? store.get(key) : null;
    },
    async put(key, value) {
      store.set(key, value);
    },
  };
}

function envWith(store) {
  return { WAITLIST: store, ASSETS: { async fetch() { return new Response("asset", { status: 200 }); } } };
}

async function post(env, body, contentType = "application/json") {
  const payload = contentType.includes("json") ? JSON.stringify(body) : body;
  const response = await worker.fetch(
    new Request("https://cloudripper.youneek.dev/api/waitlist", {
      method: "POST",
      headers: { "content-type": contentType },
      body: payload,
    }),
    env,
  );
  return { status: response.status, json: await response.json() };
}

test("stores email and joinedAt, and omits empty interests", async () => {
  const store = kv();
  const { status, json } = await post(envWith(store), { email: "Ada@Example.com" });
  assert.equal(status, 201);
  assert.equal(json.ok, true);
  const saved = JSON.parse(store.store.get("email:ada@example.com"));
  assert.deepEqual(Object.keys(saved), ["email", "joinedAt"]);
  assert.equal(saved.email, "ada@example.com");
  assert.equal(Number.isNaN(Date.parse(saved.joinedAt)), false);
});

test("stores selected interests and a bounded note", async () => {
  const store = kv();
  const note = "a".repeat(200);
  const { status } = await post(envWith(store), {
    email: "a@example.com",
    wants: ["History", "history", "other"],
    wants_note: note,
  });
  assert.equal(status, 201);
  const saved = JSON.parse(store.store.get("email:a@example.com"));
  assert.deepEqual(saved.interests, ["history", "other"]);
  assert.equal(saved.interestNote, note);
  assert.equal(saved.email, "a@example.com");
});

test("accepts repeated form fields and leaves an old record untouched", async () => {
  const prior = JSON.stringify({ email: "a@example.com", joinedAt: "2020-01-01T00:00:00.000Z" });
  const store = kv({ "email:a@example.com": prior });
  const body = new URLSearchParams();
  body.append("email", "a@example.com");
  body.append("wants", "scheduled scans");
  body.append("wants", "team invites");
  const { status, json } = await post(envWith(store), body, "application/x-www-form-urlencoded");
  assert.equal(status, 200);
  assert.equal(json.duplicate, true);
  assert.equal(store.store.get("email:a@example.com"), prior);
});

test("rejects unknown choices, long notes, and bad emails", async () => {
  const store = kv();
  const unknown = await post(envWith(store), { email: "a@example.com", wants: ["savings"] });
  assert.equal(unknown.status, 400);
  const longNote = await post(envWith(store), { email: "a@example.com", wants_note: "x".repeat(201) });
  assert.equal(longNote.status, 400);
  const badEmail = await post(envWith(store), { email: "not-an-email" });
  assert.equal(badEmail.status, 400);
  const longEmail = await post(envWith(store), { email: `${"a".repeat(250)}@b.co` });
  assert.equal(longEmail.status, 400);
  assert.equal(store.store.size, 0);
});

test("honeypot stores nothing and GET is not allowed", async () => {
  const store = kv();
  const trapped = await post(envWith(store), { email: "bot@example.com", company: "acme" });
  assert.equal(trapped.status, 200);
  assert.equal(trapped.json.ok, true);
  assert.equal(store.store.size, 0);
  const got = await worker.fetch(new Request("https://cloudripper.youneek.dev/api/waitlist"), envWith(store));
  assert.equal(got.status, 405);
});
