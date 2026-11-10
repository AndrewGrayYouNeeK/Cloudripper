const json = (obj, status = 200) =>
  new Response(JSON.stringify(obj), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", "cache-control": "no-store" },
  });

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;
const EMAIL_MAX = 254;
const NOTE_MAX = 200;
const WANTS = ["scheduled scans", "multi-account", "history", "team invites", "other"];

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/api/waitlist") {
      if (request.method !== "POST") return json({ ok: false, error: "Method not allowed" }, 405);
      let body;
      try {
        body = await readBody(request);
      } catch {
        return json({ ok: false, error: "Invalid request body" }, 400);
      }
      if (body && body.company) return json({ ok: true }); // honeypot
      const email = String((body && body.email) || "").trim().toLowerCase();
      if (email.length > EMAIL_MAX || !EMAIL_RE.test(email)) {
        return json({ ok: false, error: "Please enter a valid email address." }, 400);
      }
      const wants = parseWants(body && body.wants);
      if (wants.error) return json({ ok: false, error: wants.error }, 400);
      const note = parseNote(body);
      if (note.error) return json({ ok: false, error: note.error }, 400);

      const key = `email:${email}`;
      const existing = await env.WAITLIST.get(key);
      if (existing) return json({ ok: true, duplicate: true, message: "You're already on the list." });

      const record = { email, joinedAt: new Date().toISOString() };
      if (wants.values.length) record.interests = wants.values;
      if (note.value) record.interestNote = note.value;
      await env.WAITLIST.put(key, JSON.stringify(record));
      return json({ ok: true, message: "You're on the list!" }, 201);
    }
    return env.ASSETS ? env.ASSETS.fetch(request) : new Response("Not found", { status: 404 });
  },
};

async function readBody(request) {
  const ct = request.headers.get("content-type") || "";
  if (ct.includes("application/json")) return await request.json();
  const fd = await request.formData();
  const body = {};
  for (const [key, value] of fd.entries()) {
    if (key === "wants") {
      if (!body.wants) body.wants = [];
      body.wants.push(String(value));
    } else if (body[key] === undefined) {
      body[key] = String(value);
    }
  }
  return body;
}

function parseWants(raw) {
  if (raw == null || raw === "") return { values: [] };
  let items = raw;
  if (typeof raw === "string") items = raw.split(",");
  if (!Array.isArray(items)) return { error: "Choices must be a list." };
  const values = [];
  for (const item of items) {
    if (typeof item !== "string" && typeof item !== "number") {
      return { error: "Choices must be text." };
    }
    const value = String(item).trim().toLowerCase();
    if (!value) continue;
    if (!WANTS.includes(value)) return { error: "Unknown choice. Pick from the list or leave it blank." };
    if (!values.includes(value)) values.push(value);
  }
  if (values.length > WANTS.length) return { error: "Too many choices." };
  return { values };
}

function parseNote(body) {
  const raw = body && body.wants_note != null ? body.wants_note : body && body.interestNote;
  if (raw == null || raw === "") return { value: "" };
  if (typeof raw !== "string") return { error: "Detail must be text." };
  const value = raw.trim();
  if ([...value].length > NOTE_MAX) return { error: "Keep the detail to 200 characters." };
  return { value };
}
