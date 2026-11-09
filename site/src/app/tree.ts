// A form as a reader sees it: its parts and lines, with the schedules,
// worksheets, tests and tables each line relies on hung beneath it. The
// helpers in between, which no publication names, are left out.
import { Expr, Rule, Source, model, rules, sources } from './model';

export interface TreeNode {
  rule: Rule;
  /** The form lines that rely on it. */
  lines: string[];
  children: TreeNode[];
}

export interface TreePart {
  caption: string;
  /** The first and last line numbers in the part. */
  from?: string;
  to?: string;
  nodes: TreeNode[];
}

/** A rule a reader would recognize: one a publication names, or a worksheet. */
function notable(r: Rule): boolean {
  return (r.anchors?.length ?? 0) > 0 || r.kind === 'worksheet';
}

function refs(e: Expr | undefined, out: Set<string>): Set<string> {
  if (!e) return out;
  if (e.k === 'sym' && rules.has(e.v!)) out.add(e.v!);
  for (const a of e.a ?? []) refs(a, out);
  return out;
}

/** The notable rules reached from a rule, looking through the helpers. */
function reach(name: string, seen: Set<string>): TreeNode[] {
  if (seen.has(name)) return [];
  seen.add(name);
  const r = rules.get(name);
  if (!r) return [];
  if (notable(r)) {
    const below = new Set(seen);
    return [{ rule: r, lines: [], children: (r.calls ?? []).flatMap((c) => reach(c, below)) }];
  }
  return (r.calls ?? []).flatMap((c) => reach(c, seen));
}

export function formTree(formName: string): TreePart[] {
  const form = rules.get(formName);
  if (!form) return [];
  return (form.parts ?? [])
    .filter((p) => p.caption)
    .map((p) => {
      const lines = p.items.filter((i) => i.kind === 'line');
      const nodes = new Map<string, TreeNode>();
      for (const it of p.items) {
        // A schedule the form attaches is shown whether or not a source names it yet.
        if (it.kind === 'attach') {
          for (const name of refs(it.expr, new Set())) {
            const r = rules.get(name);
            if (r && !notable(r) && !nodes.has(name)) nodes.set(name, { rule: r, lines: [], children: [] });
          }
        }
        for (const name of refs(it.expr, new Set())) {
          for (const n of reach(name, new Set([formName]))) {
            const have = nodes.get(n.rule.name);
            const target = have ?? n;
            if (it.kind === 'line' && it.number && !target.lines.includes(it.number)) target.lines.push(it.number);
            if (!have) nodes.set(n.rule.name, n);
          }
        }
      }
      return { caption: p.caption, from: lines[0]?.number, to: lines.at(-1)?.number, nodes: [...nodes.values()] };
    });
}

/** A jurisdiction's sources for a year. */
export function sourcesFor(jurisdiction: string, year: number): Source[] {
  return model.sources.filter(
    (s) => s.publisher.jurisdiction.toLowerCase() === jurisdiction && s.taxYears.includes(year),
  );
}

export interface Coverage {
  computed: number;
  total: number;
  leftOut: number;
}

/** How many of a form's lines the rules compute, from its source's anchors. */
export function formCoverage(sourceId: string): Coverage {
  const a = sources.get(sourceId)?.anchors ?? [];
  const computed = a.filter((x) => x.implementedBy).length;
  const leftOut = a.filter((x) => x.notModeled).length;
  return { computed, leftOut, total: computed + leftOut };
}

/** Everything a set of sources says the rules leave out, and why. */
export function leftOut(srcs: Source[]): { source: Source; title: string; reason: string; anchor: string }[] {
  // The same part can be left out of both a form and its instructions; list it once.
  const seen = new Set<string>();
  return srcs
    .flatMap((s) =>
      (s.anchors ?? []).filter((a) => a.notModeled).map((a) => ({ source: s, title: a.title, reason: a.notModeled!, anchor: a.id })),
    )
    .filter((l) => !seen.has(l.title) && !!seen.add(l.title));
}

/** The site-wide counts the home page leads with. */
export function totals() {
  const anchors = model.sources.flatMap((s) => s.anchors ?? []);
  return {
    linesComputed: anchors.filter((a) => a.implementedBy?.includes(':')).length,
    sources: model.sources.length,
    figures: model.figureSets.reduce((n, f) => n + f.figures.length, 0),
    leftOut: anchors.filter((a) => a.notModeled).length,
  };
}
