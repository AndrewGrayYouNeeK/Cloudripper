// Run with `npm test` (node --test, Node 22.6+ strips the types).
import { test } from "node:test";
import assert from "node:assert/strict";
import { NOUS_PHRASE, confirmEnabled, phraseMatches } from "./nousGate.ts";

test("the exact phrase, any case, trimmed, enables Confirm", () => {
    for (const ok of [NOUS_PHRASE, "i understand", "  I UNDERSTAND  ", "I Understand\n"]) {
        assert.equal(phraseMatches(ok), true, ok);
        assert.equal(confirmEnabled(ok, false), true, ok);
    }
});

test("anything else keeps Confirm disabled", () => {
    for (const bad of ["", "y", "yes", "ok", "I understand.", "I  understand", "understand", "I understan"]) {
        assert.equal(phraseMatches(bad), false, bad);
        assert.equal(confirmEnabled(bad, false), false, bad);
    }
});

test("Confirm stays disabled while a request is running", () => {
    assert.equal(confirmEnabled(NOUS_PHRASE, true), false);
});
