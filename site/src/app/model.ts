// The documentation model //docsmodel generates, and the indexes the pages
// look things up in.
import data from '../model/model.json';

export interface Region {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
}

export interface Locator {
  page?: number;
  region?: Region;
  line?: string;
  section?: string;
}

export interface Anchor {
  id: string;
  title: string;
  at?: Locator;
  implementedBy?: string;
  notModeled?: string;
  informational?: boolean;
}

export interface Copy {
  url: string;
  retrieved: string;
  sha256: string;
  mediaType?: string;
  path?: string;
}

export interface Source {
  id: string;
  publisher: { jurisdiction: string; agency: string; reuse?: string; reuseNote?: string };
  kind: string;
  title: string;
  designation: string;
  revision?: string;
  taxYears: number[];
  copies?: Copy[];
  series?: string;
  anchors?: Anchor[];
  anchorScheme?: { unit: string; urlTemplate: string };
}

export interface Expr {
  k: 'list' | 'sym' | 'kw' | 'str' | 'int' | 'float' | 'money' | 'bool' | 'quote';
  v?: string;
  a?: Expr[];
}

export interface AnchorRef {
  source: string;
  anchor: string;
}

export interface Item {
  kind: 'line' | 'attach' | 'bind' | 'set' | 'result';
  number?: string;
  name: string;
  caption?: string;
  field?: string;
  expr: Expr;
  anchors?: AnchorRef[];
}

export interface Part {
  caption: string;
  items: Item[];
}

export interface Cite {
  code?: string;
  section?: string;
  sub?: string;
  text?: string;
  source?: string;
  url?: string;
}

export interface Param {
  name: string;
  type: string;
  doc?: string;
}

export interface Rule {
  name: string;
  kind: 'def' | 'predicate' | 'form' | 'worksheet' | 'struct';
  file: string;
  line: number;
  doc: string;
  params?: Param[];
  returns?: string;
  cites?: Cite[];
  body?: Expr;
  parts?: Part[];
  calls?: string[];
  figures?: string[];
  source: string;
  anchors?: AnchorRef[];
}

export interface Figure {
  field: string;
  doc?: string;
  value: string;
  rows?: string[][];
  source: string;
  anchor: string;
}

export interface FigureSet {
  name: string;
  type: string;
  year: number;
  doc: string;
  file: string;
  line: number;
  figures: Figure[];
  source: string;
}

export interface DerivationNode {
  kind: 'call' | 'line' | 'check' | 'root';
  name?: string;
  line?: string;
  caption?: string;
  args?: { name: string; value: string }[];
  value: string;
  /** A check's condition, to be written out like a formula. */
  expr?: Expr;
  children?: DerivationNode[];
}

export interface Household {
  id: string;
  /** The form rule the household is followed through. */
  form: string;
  label: string;
  description: string;
  facts: { label: string; value: string }[];
  totalTax: string;
  refund: string;
  owed: string;
  derivation: DerivationNode;
  notes?: string[];
}

/** A real message, or the parts of one, in protobuf text format. */
export interface Example {
  type: string;
  from: string;
  paths?: string[];
  text: string;
}

/** A protobuf type, named for linking to its page. */
export interface TypeEntry {
  slug: string;
  short: string;
  kind: 'message' | 'enum';
  summary?: string;
}

/** A protobuf type's page: its fields or values, from its .proto file. */
export interface ProtoType extends TypeEntry {
  name: string;
  package: string;
  file: string;
  doc?: string;
  fields?: {
    name: string;
    number: number;
    type: string;
    ref?: string;
    repeated?: boolean;
    oneof?: string;
    doc?: string;
    readBy?: string[];
  }[];
  values?: { name: string; number: number; doc?: string }[];
  usedBy?: string[];
}

export interface Model {
  jurisdiction: string;
  year: number;
  sources: Source[];
  rules: Rule[];
  figureSets: FigureSet[];
  households: Household[];
  examples: Record<string, Example>;
  typeIndex: TypeEntry[];
  problems: string[];
}

export const model = data as unknown as Model;

export const sources = new Map(model.sources.map((s) => [s.id, s]));
export const rules = new Map(model.rules.map((r) => [r.name, r]));
export const typesBySlug = new Map(model.typeIndex.map((t) => [t.slug, t]));

/**
 * The type a name in prose means: a slug, such as returns.v1.ReturnInputs,
 * or a short name, such as ReturnInputs, preferring the return contract's
 * own type when two packages share a name.
 */
export function typeNamed(name: string): TypeEntry | undefined {
  return (
    typesBySlug.get(name) ??
    typesBySlug.get('returns.v1.' + name) ??
    model.typeIndex.find((t) => t.short === name)
  );
}

/**
 * A rule's name as a path segment. A test's name ends in "?", which a URL
 * reads as a query, so its slug ends in "-test" instead.
 */
export function slug(name: string): string {
  return name.endsWith('?') ? name.slice(0, -1) + '-test' : name;
}
export const rulesBySlug = new Map(model.rules.map((r) => [slug(r.name), r]));
export const figureSets = new Map(model.figureSets.map((f) => [f.name, f]));

/** The forms a sample household is followed through. */
export const followed = new Set(model.households.map((h) => h.form));

/** The rules that call each rule. */
export const callers = new Map<string, string[]>();
for (const r of model.rules) {
  for (const c of r.calls ?? []) {
    callers.set(c, [...(callers.get(c) ?? []), r.name]);
  }
}

/** A figure, by its struct type and field, as rules read it: "CaliforniaYear.isr_adult". */
export interface FigureRef {
  set: FigureSet;
  figure: Figure;
}
export const figuresByField = new Map<string, FigureRef>();
for (const set of model.figureSets) {
  for (const figure of set.figures) {
    figuresByField.set(`${set.type}.${figure.field}`, { set, figure });
    if (set.figures.length === 1) figuresByField.set(set.name, { set, figure });
  }
}

/** The rules that read each figure, keyed as figuresByField is. */
export const readers = new Map<string, string[]>();
for (const r of model.rules) {
  for (const f of r.figures ?? []) {
    readers.set(f, [...(readers.get(f) ?? []), r.name]);
  }
}

/** The figures printed at each anchor, as "source#anchor". */
export const figuresAt = new Map<string, FigureRef[]>();
for (const set of model.figureSets) {
  for (const figure of set.figures) {
    const key = `${figure.source}#${figure.anchor}`;
    figuresAt.set(key, [...(figuresAt.get(key) ?? []), { set, figure }]);
  }
}

/** The rules citing each section of law, as "source#section". */
export const citing = new Map<string, Rule[]>();
for (const r of model.rules) {
  for (const c of r.cites ?? []) {
    if (!c.source || !c.section) continue;
    const key = `${c.source}#${c.section}`;
    if (!(citing.get(key) ?? []).includes(r)) citing.set(key, [...(citing.get(key) ?? []), r]);
  }
}

export function anchor(ref: AnchorRef): Anchor | undefined {
  return sources.get(ref.source)?.anchors?.find((a) => a.id === ref.anchor);
}

/** Where a rule or a form's line is implemented, read the other way: what implements an anchor. */
export function implementer(a: Anchor): { rule?: Rule; line?: Item } {
  if (!a.implementedBy) return {};
  const [name, line] = a.implementedBy.split(':');
  const rule = rules.get(name);
  if (!rule || !line) return { rule };
  for (const p of rule.parts ?? []) {
    for (const it of p.items) {
      if (it.kind === 'line' && it.number === line) return { rule, line: it };
    }
  }
  return { rule };
}

export function pdfPath(src: Source): string | undefined {
  const path = src.copies?.find((c) => c.path)?.path;
  // Relative to the base, as every path the site writes is: the site is
  // served under /taxcalcs/.
  return path ? `pdfs/${path}` : undefined;
}

export const kindLabels: Record<string, string> = {
  KIND_STATUTE: 'Statute',
  KIND_REGULATION: 'Regulation',
  KIND_GUIDANCE: 'Guidance',
  KIND_FORM: 'Form',
  KIND_INSTRUCTIONS: 'Instructions',
  KIND_PUBLICATION: 'Publication',
  KIND_ANNOUNCEMENT: 'Announcement',
  KIND_WEB_PAGE: 'Web page',
  KIND_DATASET: 'Dataset',
  KIND_CASE: 'Case',
};

export const ruleKindLabels: Record<string, string> = {
  def: 'Calculation',
  predicate: 'Test',
  form: 'Form',
  worksheet: 'Worksheet',
  struct: 'Figures',
};
