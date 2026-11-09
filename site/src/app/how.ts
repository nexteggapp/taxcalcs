import { ChangeDetectionStrategy, Component, computed, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Derivation } from './derivation';
import { title } from './format';
import { highlightLisp } from './markdown';
import { DerivationNode, figureSets, model, rules, slug } from './model';
import { Prose } from './prose';

// The section mixes the two kinds of text on purpose. What each kind of thing
// is, is written by hand, with [terms] and `Types` linked. Everything shown of
// it is generated: the examples are real messages from the sample households
// and the real source of the rules, and the lists come from the model, so they
// stay true as the rules change.

/** Something shown of a concept. */
type Show =
  | { proto: string }
  | { rule: string; lines: number }
  | { figures: string; lines: number }
  | { derivation: { line: string } };

interface Instance {
  label: string;
  route: string[];
}

interface Concept {
  key: string;
  name: string;
  what: string[];
  show: Show[];
  /** The instances, generated from the model. */
  list?: () => { items: Instance[]; total: number; all?: { text: string; route: string; query?: Record<string, string> } };
  more?: { text: string; route: string };
}

interface Group {
  title: string;
  note: string;
  concepts: Concept[];
}

const ofKind = (kind: string, all: string) => () => {
  const rs = model.rules.filter((r) => r.kind === kind);
  return {
    items: rs.slice(0, 6).map((r) => ({ label: title(r), route: ['/rules', slug(r.name)] })),
    total: rs.length,
    all: { text: all.replace('N', String(rs.length)), route: '/rules', query: { kind } },
  };
};

const groups: Group[] = [
  {
    title: 'What you give it',
    note: 'Facts, never conclusions',
    concepts: [
      {
        key: 'inputs',
        name: 'Return inputs',
        what: [
          'One `ReturnInputs` message holds everything taxcalcs is told about a household for one [tax year]: the federal inputs, and an entry for each state [return] to prepare. The federal return is always prepared, because every state return starts from it.',
          'A year whose [figures] are not in the build is an error. taxcalcs never borrows a nearby year’s numbers.',
        ],
        show: [{ proto: 'inputs' }],
        more: { text: 'The ReturnInputs type', route: '/types/returns.v1.ReturnInputs' },
      },
      {
        key: 'facts',
        name: 'Household facts',
        what: [
          'The people in the household and what is true of them, in a `Household`: ages, blindness, who someone else can claim as a dependent; and each state’s own [facts], such as rent paid.',
          'Only facts are inputs. Whether a child is a qualifying child, or whether head of household is allowed, are determinations, and the [rules] make them from the facts.',
        ],
        show: [{ proto: 'household' }],
        list: () => ({
          items: model.households.map((h) => ({ label: h.label, route: ['/walkthrough', 'ca-form-540'] })),
          total: model.households.length,
        }),
      },
      {
        key: 'documents',
        name: 'Information returns',
        what: [
          'The [information returns] a household receives from employers, banks and brokers, as received, in their [taxprotos] schemas, under `Documents`. Each is wrapped with the person it belongs to.',
        ],
        show: [{ proto: 'w2' }],
        more: { text: 'The Documents type', route: '/types/returns.v1.Documents' },
      },
      {
        key: 'elections',
        name: 'Elections',
        what: [
          '[Elections] are the choices the law leaves to the filer, such as itemizing deductions, given in `Elections` and in each state’s inputs.',
          'If the filer has made a choice, taxcalcs uses it. If not, it works out each eligible option and uses the one with the lower tax. Either way the [return] reports the choice as a `ResolvedElection`, with each option’s result and the law that makes it a choice, so an app can show the comparison or ask.',
        ],
        show: [{ proto: 'elections' }, { proto: 'resolved' }],
      },
    ],
  },
  {
    title: 'What it knows',
    note: 'The law, written as data and rules',
    concepts: [
      {
        key: 'sources',
        name: 'Sources and anchors',
        what: [
          'A [source] is one published document for one [tax year]: a form, its instructions, or a code of law as it stood that year. The 2026 instructions are a different source from the 2025 ones, even where the text is the same. A PDF copy of each is kept in the repository, checked against its sha256, so these pages can show the exact page.',
          'An [anchor] is one place in a source, with the page and region it is printed in. [Rules] and [figures] point at anchors, never at a whole document. Sources are listed in the [source catalog], as text-format [protocol buffers].',
        ],
        show: [{ proto: 'source' }],
        list: () => ({
          items: model.sources.map((s) => ({ label: s.title, route: ['/sources', s.id] })),
          total: model.sources.length,
          all: { text: `All ${model.sources.length} sources`, route: '/sources' },
        }),
      },
      {
        key: 'figures',
        name: 'Figures',
        what: [
          'The numbers a year’s documents publish: rate schedules, thresholds, standard deductions, credit amounts. A [figure set] holds one year’s [figures] for a [jurisdiction], and each figure names the [source] and [anchor] that prints it.',
          'Rules take the figure set as an argument and never contain a published number, so a new year is a new figure set, read from new sources, with the same rules.',
        ],
        show: [{ figures: 'ca-year-2025', lines: 8 }],
        list: () => ({
          items: model.figureSets.map((f) => ({ label: f.name, route: ['/figures', f.name] })),
          total: model.figureSets.length,
        }),
      },
      {
        key: 'forms',
        name: 'Forms',
        what: [
          'A [form] rule, declared with `defform`, computes a whole tax form and returns it filled in, as a `Form540` for California’s Form 540. It is written in the form’s printed order, grouped into the form’s parts. Each [line] has its number and caption, the expression that computes it, and the field of the output form it fills.',
          'Later lines use earlier ones by name, so line 15 is written (- line13 line14), as the form says it. A form also attaches the schedules and [worksheets] it uses. Lines taxcalcs does not compute yet are [not modeled], and listed on the form’s page with the reason.',
        ],
        show: [{ rule: 'ca-form-540', lines: 22 }],
        list: ofKind('form', 'All N forms'),
      },
      {
        key: 'worksheets',
        name: 'Worksheets',
        what: [
          'A [worksheet] rule, declared with `defworksheet`, is written the same way, as numbered or lettered [lines], but returns one value instead of a form. It is for the worksheets printed in the instructions and kept with the filer’s records, rather than filed.',
        ],
        show: [{ rule: 'ca-agi-limitation', lines: 14 }],
        list: ofKind('worksheet', 'All N worksheets'),
      },
      {
        key: 'tests',
        name: 'Tests',
        what: [
          'A [test] answers a yes-or-no question the law asks, such as whether someone else can claim the filer as a dependent, or whether the household qualifies for the renter’s credit. Its name ends in a question mark.',
          'Tests combine their conditions with all-of and any-of, so a [derivation] shows each condition met (✓) or not (✗), not only the answer.',
        ],
        show: [{ rule: 'ca-claimable?', lines: 10 }],
        list: ofKind('predicate', 'All N tests'),
      },
      {
        key: 'calculations',
        name: 'Calculations',
        what: [
          'Everything else is a [calculation], declared with def: a typed function that computes one value, such as the tax from a rate schedule or a count of exemptions. A form’s line usually calls one.',
          'Each calculation cites the law it carries out, and the comment above it is its documentation on these pages.',
        ],
        show: [{ rule: 'ca-behavioral-health-tax', lines: 10 }],
        list: ofKind('def', 'All N calculations'),
      },
      {
        key: 'money',
        name: 'Money',
        what: [
          '[Money] is exact. In [taxlisp] an amount is an exact rational number of dollars, written #usd"5706.00". In a `Money` message it is a whole number of cents, minor, with its currency; the examples on these pages add the dollars as a comment.',
          'Rates and ratios are floating-point numbers; amounts never are. An amount is rounded only where a form or the law says to round.',
        ],
        show: [{ proto: 'money' }],
      },
    ],
  },
  {
    title: 'What it gives back',
    note: 'Returns, and how each figure was reached',
    concepts: [
      {
        key: 'returns',
        name: 'Returns',
        what: [
          'A `ReturnOutputs` holds the federal [return] and one for each state, each with its forms filled in, the [worksheets] behind them, a summary of tax, payments and the refund or amount owed, and a [return status].',
        ],
        show: [{ proto: 'return' }],
        more: { text: 'Follow a household through Form 540', route: '/walkthrough/ca-form-540' },
      },
      {
        key: 'derivations',
        name: 'Derivations',
        what: [
          'Every figure can come with its [derivation]: each [calculation] called with its inputs and result, each [line] of each worksheet, and each condition of each [test], down to the published [figures].',
          'The evaluator records it as the rules run, as a [trace], so it is the computation itself, not a description of it. This is line 32 of Form 540 for the first sample household:',
        ],
        show: [{ derivation: { line: '32' } }],
        more: { text: 'Every line, for every sample household', route: '/walkthrough/ca-form-540' },
      },
      {
        key: 'questions',
        name: 'Questions',
        what: [
          'What taxcalcs still needs to know. `DescribeInputs` takes the inputs collected so far and lists, in priority order, the [field paths] that are still relevant or required, each as an `InputFieldDescriptor` with the [question] to ask.',
        ],
        show: [{ proto: 'describe' }],
      },
      {
        key: 'diagnostics',
        name: 'Diagnostics',
        what: [
          '[Diagnostics] are notes on a [return], each with a code, a severity, a message, the input fields it concerns and the law it rests on. A credit or tax that is [not modeled] is a warning and leaves the return incomplete, so nothing is silently left out.',
        ],
        show: [{ proto: 'diagnostics' }],
      },
    ],
  },
];

interface Step {
  name: string;
  what: string;
  rpc?: string;
}

const steps: Step[] = [
  {
    name: 'Collect',
    what: 'The app records what it knows about the household as `ReturnInputs`: the filer’s answers, the [information returns] they have, last year’s figures. It does not need everything at once.',
  },
  {
    name: 'Ask',
    what: '`DescribeInputs` says which fields are still needed, in priority order, each with its [question]. The app asks, and adds the answers to the inputs.',
    rpc: 'rpc DescribeInputs(DescribeInputsRequest) returns (DescribeInputsResponse);',
  },
  {
    name: 'Prepare',
    what: '`Prepare` runs the federal [rules], then each state’s from the finished federal return, and returns the `ReturnOutputs`. Nothing is kept between calls, so the app prepares again whenever an input changes.',
    rpc: 'rpc Prepare(PrepareRequest) returns (PrepareResponse);',
  },
  {
    name: 'Review',
    what: 'The app reads each [return status] and its [diagnostics], and shows the [elections] taxcalcs made so the filer can confirm or change them. A change goes back into the inputs.',
  },
  {
    name: 'Explain',
    what: 'The app shows the result: the tax, and the refund or amount owed. Any figure can open its [working], and from there the line of the form and the page of the [source] it comes from.',
  },
];

/** Closes the parentheses an excerpt cut off, after an ellipsis. */
function excerpt(src: string, lines: number): string {
  const all = src.split('\n');
  if (all.length <= lines) return src;
  const kept = all.slice(0, lines).join('\n').trimEnd();
  let depth = 0;
  let inString = false;
  for (let i = 0; i < kept.length; i++) {
    const c = kept[i];
    if (inString) {
      if (c === '\\') i++;
      else if (c === '"') inString = false;
    } else if (c === '"') inString = true;
    else if (c === ';') while (i < kept.length && kept[i] !== '\n') i++;
    else if (c === '(' || c === '[') depth++;
    else if (c === ')' || c === ']') depth--;
  }
  return kept + '\n' + '  '.repeat(depth) + '…' + ')'.repeat(depth);
}

function lineNode(n: DerivationNode, line: string): DerivationNode | undefined {
  if (n.kind === 'line' && n.line === line) return n;
  for (const c of n.children ?? []) {
    const f = lineNode(c, line);
    if (f) return f;
  }
  return undefined;
}

/** The home page's explanation of how taxcalcs works. */
@Component({
  selector: 'app-how',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Prose, Derivation],
  template: `
    <section class="how" aria-labelledby="how-title">
      <h2 id="how-title">How taxcalcs works</h2>
      <p class="doc">
        <app-prose
          text="taxcalcs is a library of [pure functions]. It is given a household’s [facts] for a [tax year] as a \`ReturnInputs\` [protocol buffer], applies that year’s law to them, and returns the completed [returns] as a \`ReturnOutputs\`, with the [working] behind every figure. It keeps no state, so the same facts always give the same returns."
        />
      </p>
      <p class="doc">
        <app-prose
          text="The law is kept as data: the published [sources], the [figures] read from them, and [rules] written in [taxlisp] that cite both. These are the kinds of things taxcalcs works with. Choose one to see what it is."
        />
      </p>

      <div class="how-cols">
        @for (g of groups; track g.title) {
          <div class="how-col">
            <h3>{{ g.title }}</h3>
            <p class="muted small">{{ g.note }}</p>
            @for (c of g.concepts; track c.key) {
              <button
                type="button"
                class="how-chip"
                [class.on]="c.key === concept().key"
                [attr.aria-pressed]="c.key === concept().key"
                (click)="key.set(c.key)"
              >
                {{ c.name }}
              </button>
            }
          </div>
        }
      </div>

      @let c = concept();
      <div class="how-detail" aria-live="polite">
        <h3>{{ c.name }}</h3>
        @for (p of c.what; track $index) {
          <p><app-prose [text]="p" /></p>
        }
        @for (s of c.show; track $index) {
          @if (asProto(s); as e) {
            <figure class="example">
              <pre class="code"><code>{{ e.text }}</code></pre>
              <figcaption class="muted small">
                <a [routerLink]="['/types', typeSlug(e.type)]"><code>{{ shortType(e.type) }}</code></a>
                in protobuf text format, from {{ e.from }}{{ e.paths?.length ? '; other fields left out' : '' }}
              </figcaption>
            </figure>
          }
          @if (asSource(s); as x) {
            <figure class="example">
              <pre class="code"><code [innerHTML]="x.html"></code></pre>
              <figcaption class="muted small">
                From <a [routerLink]="x.route">{{ x.file }}</a>, line {{ x.line }}
              </figcaption>
            </figure>
          }
          @if (asDerivation(s); as d) {
            <div class="example">
              <app-derivation [nodes]="[d]" [open]="true" rule="ca-form-540" />
            </div>
          }
        }
        @if (c.list) {
          @let l = c.list();
          <p class="how-list small">
            <span class="muted">{{ l.total }} in these pages:</span>
            @for (i of l.items; track i.label; let last = $last) {
              <a [routerLink]="i.route">{{ i.label }}</a>{{ last ? '' : ' · ' }}
            }
            @if (l.total > l.items.length) {
              <span class="muted"> and {{ l.total - l.items.length }} more</span>
            }
          </p>
          @if (l.all && l.total > l.items.length) {
            @let a = l.all;
            <a class="button" [routerLink]="a.route" [queryParams]="a.query">{{ a.text }} →</a>
          }
        }
        @if (c.more; as m) {
          <a [routerLink]="m.route">{{ m.text }} →</a>
        }
      </div>

      <h3 class="how-sub">How an app uses it</h3>
      <p class="doc">
        <app-prose
          text="An app calls taxcalcs as a Go library, or through the ReturnPreparer gRPC service; a Rust port runs the same [rules]. A typical app goes round these steps until the [return status] is complete:"
        />
      </p>
      <ol class="how-steps">
        @for (s of steps; track s.name; let i = $index) {
          <li>
            <button type="button" [class.on]="i === step()" [attr.aria-pressed]="i === step()" (click)="step.set(i)">
              <span class="how-num">{{ i + 1 }}</span> {{ s.name }}
            </button>
          </li>
        }
      </ol>
      @let s = steps[step()];
      <div class="how-detail" aria-live="polite">
        <p><app-prose [text]="s.what" /></p>
        @if (s.rpc) {
          <figure class="example">
            <pre class="code"><code>{{ s.rpc }}</code></pre>
            <figcaption class="muted small">From the ReturnPreparer service, in returns/v1/service.proto</figcaption>
          </figure>
        }
      </div>
      <p class="how-aside small">
        Estimating a year that is still in progress, from pay stubs so far and plans for the rest, is a separate layer
        built on this library. <a routerLink="/estimates">How the tax-year estimator works →</a>
      </p>

      <h3 class="how-sub">taxlisp</h3>
      <p class="doc">
        <app-prose
          text="The [rules] are written in [taxlisp], a small Lisp made for tax law. It is statically typed and purely functional, [money] in it is exact, and every program is also a [protocol buffer] syntax tree, so tools can read and check rules without parsing text. [Forms] and [worksheets] are declared line by line, as they are printed, and every rule can cite the law. The Go engine and the Rust port run the same rule files, and the [trace] a run records is the [derivation] these pages show."
        />
      </p>
      <p>
        <a class="cta" routerLink="/taxlisp">Read the taxlisp specification →</a>
        <a class="button" routerLink="/glossary">Glossary</a>
        <a class="button" routerLink="/types">Types</a>
      </p>
    </section>
  `,
})
export class How {
  protected readonly groups = groups;
  protected readonly steps = steps;
  protected readonly year = model.year;
  protected readonly key = signal('forms');
  protected readonly concept = computed(() => groups.flatMap((g) => g.concepts).find((c) => c.key === this.key())!);
  protected readonly step = signal(0);

  protected asProto(s: Show) {
    return 'proto' in s ? model.examples[s.proto] : undefined;
  }
  protected asSource(s: Show) {
    if ('rule' in s) {
      const r = rules.get(s.rule)!;
      return { html: highlightLisp(excerpt(r.source, s.lines)), file: r.file, line: r.line, route: ['/rules', slug(r.name)] };
    }
    if ('figures' in s) {
      const f = figureSets.get(s.figures)!;
      return { html: highlightLisp(excerpt(f.source, s.lines)), file: f.file, line: f.line, route: ['/figures', f.name] };
    }
    return undefined;
  }
  protected asDerivation(s: Show) {
    return 'derivation' in s ? lineNode(model.households[0].derivation, s.derivation.line) : undefined;
  }
  protected typeSlug(full: string): string {
    return full.replace(/^nexteggapp\.taxcalcs\./, '').replace(/^nexteggapp\./, '');
  }
  protected shortType(full: string): string {
    return full.slice(full.lastIndexOf('.') + 1);
  }
}
