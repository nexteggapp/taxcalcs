// Formatting for readers who are not programmers: money, names, and the
// rules' expressions written as arithmetic.
import { Expr, FigureSet, Rule, figureSets, figuresByField, rules, slug } from './model';

export function escape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/** "1234.5" or "1234.50" as "$1,234.50". */
export function money(v: string): string {
  const n = Number(v);
  if (!Number.isFinite(n)) return v;
  const s = Math.abs(n).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  return (n < 0 ? '−$' : '$') + s;
}

/** A derivation's value, as a reader would write it. */
export function value(v: string | undefined): string {
  if (v === undefined || v === '') return '';
  if (v === 'true') return 'Yes';
  if (v === 'false') return 'No';
  if (/^-?\d+\.\d\d$/.test(v)) return money(v);
  if (/^-?\d+\.0$/.test(v)) return v.replace(/\.0$/, '');
  if (v.startsWith('<')) return '';
  // An enum value: 'nexteggapp.taxprotos.us.federal.v1/FILING_STATUS_SINGLE.
  const e = v.match(/^'(?:.*\/)?(?:FILING_STATUS_|DEDUCTION_METHOD_ELECTION_)?([A-Z0-9_]+)$/);
  if (e) return e[1].toLowerCase().replace(/_/g, ' ');
  return v;
}

/** personal_exemption_credit, ca-agi-limitation → "personal exemption credit", "agi limitation". */
export function humanize(name: string): string {
  return name
    .replace(/^(ca|state|fed|compute)-/, '')
    .replace(/[?]$/, '')
    .replace(/[_-]+/g, ' ')
    .replace(/\bagi\b/gi, 'AGI')
    .replace(/\bisr\b/gi, 'ISR')
    .replace(/\bbp\b/g, '(basis points)')
    .trim();
}

const titles: Record<string, string> = {
  'ca-form-540': 'Form 540, California Resident Income Tax Return',
  'ca-agi-limitation': 'AGI Limitation Worksheet',
  'ca-schedule-ca': 'Schedule CA (540)',
  'ca-renters-credit': "Nonrefundable Renter's Credit",
  'ca-qualifies-for-renters-credit?': "Qualifying for the renter's credit",
  'ca-within-renters-agi-limit?': "Within the renter's credit AGI limit",
  'ca-behavioral-health-tax': 'Behavioral Health Services Tax',
  'ca-shared-responsibility-penalty': 'Individual Shared Responsibility Penalty',
  'ca-penalty': 'Line 92, the penalty owed',
  'ca-owes-penalty?': 'Owing the shared responsibility penalty',
  'ca-claimable?': 'Being claimable as a dependent',
  'ca-standard-for-filer': 'Standard deduction',
  'ca-rate-schedule-tax': 'Tax from the rate schedules',
  'ca-itemized-limitation': 'Limitation on itemized deductions',
  'ca-isr-filing-threshold': 'Filing threshold for the penalty',
  'prepare-california-return': 'Preparing the California return',
  'ca-schedule': 'Tax rate schedules X, Y and Z',
  'ca-standard-deduction': 'Standard deduction chart',
  'ca-personal-count': 'Personal exemptions',
  'state-dependent-count': 'Dependent exemptions',
  CaliforniaYear: "California's figures for a year",
  'fed-form-1040': 'Form 1040, U.S. Individual Income Tax Return',
  'compute-schedule-2': 'Schedule 2 (Form 1040), Additional Taxes',
  'compute-schedule-3': 'Schedule 3 (Form 1040), Additional Credits and Payments',
  'compute-qdcgt-worksheet': 'Qualified Dividends and Capital Gain Tax Worksheet',
  'compute-ss-benefits-worksheet': 'Social Security Benefits Worksheet',
  'compute-standard-deduction-worksheet': 'Standard Deduction Worksheet',
  'compute-ctc-odc-actc': 'Schedule 8812, Credits for Qualifying Children and Other Dependents',
  'compute-earned-income-credit': 'Earned income credit',
  'compute-bracket-tax': 'Tax from the tax rate schedules',
  'tax-on-taxable-income': 'Line 16, the tax',
  'base-standard-deduction': 'Standard deduction chart',
  'additional-standard-deduction-per-box': 'Additional standard deduction for age or blindness',
  'count-age-and-blindness-boxes': 'Boxes checked for age and blindness',
  'compute-withheld-from-w2': 'Tax withheld on Forms W-2',
  'compute-withheld-from-1099': 'Tax withheld on Forms 1099',
  'compute-withheld-from-other-forms': 'Tax withheld on other forms',
  'compute-federal-estimated-payments': 'Estimated tax payments',
  'compute-form-2210': 'Form 2210, Underpayment of Estimated Tax',
  'compute-schedule-1': 'Schedule 1 (Form 1040), Additional Income and Adjustments',
  'compute-schedule-a': 'Schedule A (Form 1040), Itemized Deductions',
  'compute-schedule-b': 'Schedule B (Form 1040), Interest and Ordinary Dividends',
  'compute-schedule-1a': 'Schedule 1-A (Form 1040), Additional Deductions',
  'compute-student-loan-interest-worksheet': 'Student Loan Interest Deduction Worksheet',
  'compute-educator-expenses': 'Educator expenses',
  'cap-gains-zero-threshold': 'The top of the 0% capital gains band',
  'prepare-federal-return': 'Preparing the federal return',
  FederalYear: 'The federal figures for a year',
};

export function title(r: Rule | string): string {
  const name = typeof r === 'string' ? r : r.name;
  const t = titles[name] ?? humanize(name);
  return t.charAt(0).toUpperCase() + t.slice(1);
}

export function figureSetTitle(s: FigureSet): string {
  if (s.figures.length === 1) {
    const sched = s.name.match(/schedule-([xyz])(\d?)-(\d{4})/);
    if (sched) return `${sched[3]} tax rate Schedule ${sched[1].toUpperCase()}${sched[2] ? '-' + sched[2] : ''}`;
  }
  return `${s.type === 'FederalYear' ? 'Federal' : 'California'} figures for ${s.year}`;
}

/** The first sentence of a doc comment. */
export function summary(doc: string): string {
  const para = doc.split('\n\n')[0];
  const m = para.match(/^.*?[.](\s|$)/);
  return (m ? m[0] : para).trim();
}

/** The context an expression is read in: what its symbols name. */
export interface Context {
  /** The symbols lines are bound to, and their labels: line31 → "line 31". */
  lines: Map<string, string>;
  /** The parameters holding a year's figures, and their struct type. */
  figureParams: Map<string, string>;
  /** Where a line's link goes, given its number. */
  lineHref?: (n: string) => string;
}

export function contextFor(rule: Rule): Context {
  const lines = new Map<string, string>();
  for (const p of rule.parts ?? []) {
    for (const it of p.items) {
      if (it.kind === 'line' && it.number) lines.set(it.name, `line ${it.number}`);
    }
  }
  const figureParams = new Map<string, string>();
  for (const p of rule.params ?? []) {
    if (p.type.endsWith('Year')) figureParams.set(p.name, p.type);
  }
  return { lines, figureParams };
}

const infix: Record<string, [string, number]> = {
  or: [' or ', 1],
  'any-of': [' or ', 1],
  and: [' and ', 2],
  'all-of': [' and ', 2],
  '<': [' &lt; ', 3],
  '<=': [' ≤ ', 3],
  '>': [' &gt; ', 3],
  '>=': [' ≥ ', 3],
  is: [' is ', 3],
  '!=': [' is not ', 3],
  '+': [' + ', 4],
  '-': [' − ', 4],
  '*': [' × ', 5],
  '/': [' ÷ ', 5],
};

/** An expression written as arithmetic, as HTML with links. */
export function render(e: Expr, ctx: Context, prec = 0): string {
  switch (e.k) {
    case 'money':
      return `<span class="num">${money(e.v!)}</span>`;
    case 'int':
      return `<span class="num">${e.v}</span>`;
    case 'float':
      return `<span class="num">${e.v!.replace(/\.0$/, '')}</span>`;
    case 'str':
      return `“${escape(e.v!)}”`;
    case 'bool':
      return e.v === 'true' ? 'yes' : 'no';
    case 'kw':
      return escape(humanize(e.v!));
    case 'quote':
      return quoted(e.a![0]);
    case 'sym':
      return symbol(e.v!, ctx);
  }
  const [head, ...args] = e.a ?? [];
  const h = head?.k === 'sym' ? head.v! : '';
  if (h in infix && args.length >= 2) {
    const [op, p] = infix[h];
    const s = args.map((a, i) => render(a, ctx, i === 0 ? p : p + 0.5)).join(op);
    return p < prec ? `(${s})` : s;
  }
  const r = (x: Expr) => render(x, ctx, 0);
  switch (h) {
    case 'not':
      return `not ${render(args[0], ctx, 6)}`;
    case 'clamp-zero':
      return `max(0, ${r(args[0])})`;
    case 'min':
    case 'max':
      return `${h}(${args.map(r).join(', ')})`;
    case 'int-to-float':
      return render(args[0], ctx, prec);
    case 'round-cents':
      return `${render(args[0], ctx, 6)}, to the cent`;
    case 'round-dollar':
      return `${render(args[0], ctx, 6)}, to the dollar`;
    case 'ceil-float':
      return `⌈${r(args[0])}⌉`;
    case 'if':
      return `<span class="cond">if ${r(args[0])}: ${r(args[1])}; otherwise ${r(args[2])}</span>`;
    case 'has?':
      return `${field(args[0], args[1], ctx)} is answered`;
    case '.':
      return field(args[0], args[1], ctx);
    case 'lookup-or':
      return `${r(args[1])} in ${r(args[0])}`;
  }
  if (rules.has(h)) {
    return `${link(`/rules/${slug(h)}`, title(h), 'rule')}(${args.map(r).join(', ')})`;
  }
  return `<code>${escape(h)}</code>(${args.map(r).join(', ')})`;
}

function quoted(e: Expr): string {
  if (e.k === 'sym') {
    const v = e.v!.replace(/^.*\//, '').replace(/^FILING_STATUS_/, '');
    return escape(v.toLowerCase().replace(/_/g, ' '));
  }
  return escape(e.v ?? '');
}

function symbol(name: string, ctx: Context): string {
  const line = ctx.lines.get(name);
  if (line) {
    const n = line.replace('line ', '');
    return `<a class="lineref" href="${ctx.lineHref ? ctx.lineHref(n) : `#line-${n}`}">${line}</a>`;
  }
  if (rules.has(name)) return link(`/rules/${slug(name)}`, title(name), 'rule');
  if (figureSets.has(name)) return link(`/figures/${encodeURIComponent(name)}`, figureSetTitle(figureSets.get(name)!), 'figure');
  if (name === 'true') return 'yes';
  if (name === 'false') return 'no';
  return `<var>${escape(humanize(name))}</var>`;
}

function field(obj: Expr, path: Expr, ctx: Context): string {
  const p = path.k === 'quote' ? path.a![0].v ?? '' : path.v ?? '';
  if (obj.k === 'sym') {
    const type = ctx.figureParams.get(obj.v!);
    if (type) {
      const ref = figuresByField.get(`${type}.${p}`);
      if (ref) {
        return link(
          `/figures/${encodeURIComponent(ref.set.name)}#${p}`,
          `${humanize(p)} <span class="figval">${value(ref.figure.value) || ref.figure.value}</span>`,
          'figure',
        );
      }
    }
    const owner: Record<string, string> = {
      'fed-ret': 'federal return',
      hh: 'household',
      'ca-in': 'California answers',
      inputs: 'inputs',
      'sched-ca': 'Schedule CA',
    };
    const who = owner[obj.v!] ?? humanize(obj.v!);
    return `<span class="input">${escape(who)}: ${escape(p.split('.').map(humanize).join(' › '))}</span>`;
  }
  return `${render(obj, ctx, 6)} › ${escape(humanize(p))}`;
}

// The href is relative to the base, which the Links directive routes.
function link(href: string, html: string, cls: string): string {
  return `<a class="${cls}" href="${href.replace(/^\//, '')}">${html}</a>`;
}
