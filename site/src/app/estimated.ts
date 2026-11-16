// Estimated tax: each jurisdiction's rules in brief, from the table in
// docs/estimated-tax.md, and the rules, forms, figures and sources its
// estimated tax file holds.
import text from '../model/estimated-tax.md';
import { FigureSet, Rule, Source, model, rules, sources } from './model';

/** The table's section of docs/estimated-tax.md. */
const section = (() => {
  const start = text.indexOf('## Rules by jurisdiction');
  const end = text.indexOf('\n## ', start + 1);
  return start < 0 ? '' : text.slice(start, end < 0 ? undefined : end);
})();

function cells(row: string): string[] {
  return row
    .trim()
    .replace(/^\||\|$/g, '')
    .split('|')
    .map((c) => c.trim());
}

const tableLines = section.split('\n').filter((l) => l.startsWith('|'));

/** The year the rules cover, from the section's heading. */
export const estimatedYear = Number(section.match(/(\d{4})/)?.[1] ?? 2026);

/** The paragraph above the table, which says what its columns mean. */
export const estimatedNote = section
  .slice(section.indexOf('\n'), section.indexOf('\n|'))
  .trim()
  .replace(/\s*\n\s*/g, ' ');

/** The table's columns, after the jurisdiction's. */
export const estimatedColumns = tableLines.length ? cells(tableLines[0]).slice(1) : [];

/** Each jurisdiction's row, by lower-case code ("us" for federal). */
export const estimatedRows = new Map<string, string[]>(
  tableLines.slice(2).map((l) => {
    const c = cells(l);
    return [c[0].toLowerCase(), c.slice(1)];
  }),
);

/** The states with no individual income tax, whose rules share one file. */
const noIncomeTax = new Set(['ak', 'fl', 'nh', 'nv', 'sd', 'tn', 'tx', 'wy']);

/** The file a jurisdiction's estimated tax rules are in. */
export function estimatedFile(code: string): string {
  if (code === 'us') return 'returns/rules/federal/10_estimated_us.tlisp';
  if (noIncomeTax.has(code)) return 'returns/rules/states/notax/02_estimated.tlisp';
  return `returns/rules/states/${code}/03_estimated.tlisp`;
}

/** What the site shows of one jurisdiction's estimated tax. */
export interface EstimatedDoc {
  /** Its row of the table, cell by column, if it has one. */
  row?: string[];
  /** Its rules, in file order, without the structs. */
  rules: Rule[];
  /** The rule that gives its answer. */
  answer?: Rule;
  /** The underpayment forms its answer fills. */
  forms: Rule[];
  /** Whether it fills Form 2210 as its own form prints it. */
  pattern: boolean;
  /** Its published figures. */
  figureSets: FigureSet[];
  /** Every source its rules and figures cite. */
  sources: Source[];
  /** The law its rules cite: code, section, and the source the site has for it. */
  law: { code: string; section: string; source?: string }[];
}

/** A rule that returns a form message itself, not one of its parts: "ca540/FormFtb5805". */
function fillsForm(r: Rule): boolean {
  return /^[a-z0-9]+\/[A-Za-z0-9]+$/.test(r.returns ?? '') && !r.returns!.startsWith('ret/');
}

export function estimatedFor(code: string): EstimatedDoc {
  const file = estimatedFile(code);
  const inFile = model.rules.filter((r) => r.file === file && r.kind !== 'struct');
  const calls = new Set(inFile.flatMap((r) => r.calls ?? []));
  const pattern = calls.has('est-pattern');
  const forms = inFile.filter(fillsForm);
  if (calls.has('est-form-2210') && !forms.length) {
    const f = rules.get(pattern ? 'est-pattern' : 'est-form-2210');
    if (f) forms.push(f);
  }
  const figureSets = model.figureSets.filter((f) => f.file === file);

  const ids = new Set<string>();
  const law = new Map<string, { code: string; section: string; source?: string }>();
  for (const r of inFile) {
    for (const c of r.cites ?? []) {
      if (c.code && c.section) law.set(`${c.code} ${c.section}`, { code: c.code, section: c.section, source: c.source });
      else if (c.source) ids.add(c.source);
    }
    for (const a of r.anchors ?? []) ids.add(a.source);
  }
  for (const s of figureSets) for (const f of s.figures) ids.add(f.source);

  return {
    row: estimatedRows.get(code),
    rules: inFile,
    answer: inFile.find((r) => r.name.endsWith('estimated-answer')),
    forms,
    pattern,
    figureSets,
    sources: [...ids].map((id) => sources.get(id)).filter((s): s is Source => !!s),
    law: [...law.values()].sort((a, b) => a.section.localeCompare(b.section, 'en', { numeric: true })),
  };
}
