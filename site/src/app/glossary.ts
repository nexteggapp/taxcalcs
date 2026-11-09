// The glossary, from docs/glossary.md, and the links from prose to its
// terms and to the protobuf types' pages.
import text from '../model/glossary.md';
import { escape, headingId, inline, Linker, renderMarkdown } from './markdown';
import { typeNamed } from './model';

export interface Term {
  term: string;
  id: string;
  /** The first paragraph, as plain text: the definition shown on hover. */
  short: string;
  /** The whole entry, as Markdown. */
  body: string;
}

const [intro, ...entries] = text.split(/^## /m);

export const glossaryIntro = intro.replace(/^# .*\n/, '').trim();

export const terms: Term[] = entries.map((e) => {
  const nl = e.indexOf('\n');
  const term = e.slice(0, nl).trim();
  const body = e.slice(nl + 1).trim();
  const first = body.split(/\n\s*\n/)[0];
  return {
    term,
    id: headingId(term),
    short: first.replace(/\s+/g, ' ').replace(/[[\]`]/g, ''),
    body,
  };
});

const byName = new Map(terms.map((t) => [t.term, t]));

/** The term a word in brackets means, allowing a plural and a capital. */
export function termNamed(word: string): Term | undefined {
  const w = word.toLowerCase();
  return byName.get(w) ?? byName.get(w.replace(/s$/, '')) ?? byName.get(w.replace(/es$/, ''));
}

function termLink(text: string, strict: boolean): string | undefined {
  const t = termNamed(text);
  if (!t) {
    if (strict) throw new Error(`No glossary entry for [${text}]`);
    return undefined;
  }
  return `<a class="term" href="glossary#${t.id}" data-def="${escape(t.short)}">${escape(text)}</a>`;
}

function typeLink(name: string): string | undefined {
  const t = typeNamed(name);
  return t ? `<a class="type-link" href="types/${t.slug}"><code>${escape(name)}</code></a>` : undefined;
}

/**
 * Links [terms] and `Types`. A strict linker refuses a term the glossary
 * does not define, so a page cannot be built with a broken link.
 */
export function linker(strict: boolean): Linker {
  return { term: (t) => termLink(t, strict), code: typeLink };
}

/** Renders one paragraph of prose, linking its terms and types. */
export function prose(s: string): string {
  return inline(s, linker(true));
}

/** Renders a glossary entry's body. */
export function renderTerm(t: Term): string {
  return renderMarkdown(t.body, linker(true)).html;
}
