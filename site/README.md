# Landing page

Source for https://cloudripper.youneek.dev, a Cloudflare Worker (`cloudripper-site`) that serves the static files in `public/` and stores waitlist signups in a Workers KV namespace.

```bash
cd site
npm install
npm test                 # worker and page checks; does not deploy
npx wrangler deploy      # needs CLOUDFLARE_API_TOKEN in the environment
```

`wrangler.jsonc` holds the account and KV namespace IDs. Those are identifiers, not credentials. Never commit an API token.
