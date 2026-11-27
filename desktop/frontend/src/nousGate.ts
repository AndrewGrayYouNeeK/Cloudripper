// The typed confirmation that turns Nous on. Kept free of DOM and Wails
// imports so `npm test` can run it in Node. The Go side checks the same
// phrase again (internal/nous.PhraseMatches).

export const NOUS_PHRASE = "I understand";

/** True when text is "I understand", ignoring case and surrounding spaces. */
export function phraseMatches(text: string): boolean {
    return text.trim().toLowerCase() === NOUS_PHRASE.toLowerCase();
}

/** Whether the modal's Confirm button may be pressed. */
export function confirmEnabled(text: string, busy: boolean): boolean {
    return !busy && phraseMatches(text);
}
