const json = (obj, status = 200) =>
  new Response(JSON.stringify(obj), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", "cache-control": "no-store" },
  });

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/api/waitlist") {
      if (request.method !== "POST") return json({ ok: false, error: "Method not allowed" }, 405);
      let body;
      try {
        const ct = request.headers.get("content-type") || "";
        if (ct.includes("application/json")) body = await request.json();
        else body = Object.fromEntries(await request.formData());
      } catch {
        return json({ ok: false, error: "Invalid request body" }, 400);
      }
      if (body && body.company) return json({ ok: true }); // honeypot
      const email = String((body && body.email) || "").trim().toLowerCase();
      if (email.length > 254 || !EMAIL_RE.test(email)) {
        return json({ ok: false, error: "Please enter a valid email address." }, 400);
      }
      const key = `email:${email}`;
      const existing = await env.WAITLIST.get(key);
      if (existing) return json({ ok: true, duplicate: true, message: "You're already on the list." });
      await env.WAITLIST.put(key, JSON.stringify({ email, joinedAt: new Date().toISOString() }));
      return json({ ok: true, message: "You're on the list!" }, 201);
    }
    return env.ASSETS ? env.ASSETS.fetch(request) : new Response("Not found", { status: 404 });
  },
};
