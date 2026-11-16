import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Rule, kindLabels, slug, sources } from '../model';
import { byCode } from '../jurisdictions';
import { figureSetTitle, summary, title } from '../format';
import { estimatedColumns, estimatedFor, estimatedNote, estimatedRows, estimatedYear } from '../estimated';
import { Crumbs } from '../crumbs';
import { pageTitle } from '../page-title';

/** Federal, then the states and DC by name. */
const order = [...estimatedRows.keys()].sort((a, b) => (a === 'us' ? -1 : b === 'us' ? 1 : nameOf(a).localeCompare(nameOf(b))));

function nameOf(code: string): string {
  return code === 'us' ? 'Federal' : (byCode.get(code)?.name ?? code.toUpperCase());
}

/**
 * A form's title. Its rule's doc names the form as printed before the first
 * comma or "from": "Form CT-2210, from the figures…". Failing that, the
 * designation of the document its lines come from, or its rule's name.
 */
function formTitle(r: Rule): string {
  if (r.name === 'est-pattern') return 'Form 2210, as the state prints it';
  if (r.name === 'est-form-2210') return 'Form 2210';
  const named = r.doc.split(/,| from /)[0].replace(/^The /, '').trim();
  if (/\d/.test(named) && named.length < 40) return named;
  const src = sources.get(r.anchors?.[0]?.source ?? r.cites?.find((c) => !c.code && c.source)?.source ?? '');
  return src?.designation ?? title(r);
}

/** Every jurisdiction's estimated tax rules, side by side. */
@Component({
  selector: 'app-estimated-index',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="[{ label: 'Jurisdictions', link: ['/'] }, { label: 'Estimated tax' }]" />
    <p class="eyebrow">Tax year {{ year }}</p>
    <h1>Estimated tax</h1>
    <p class="lede">
      Tax is due as income is earned. When withholding doesn't cover it, the IRS and most states expect payments
      during the year, and charge interest or a penalty on any that come late.
    </p>
    <p class="doc">
      For tax year {{ year }}, taxcalcs works out what a household owes on each due date and what it would be
      charged for paying late, federally and in every state. The table compares the rules. Each name opens that
      jurisdiction's page, with the underpayment form taxcalcs fills in and the rules behind it.
    </p>
    <p class="muted small">{{ note }}</p>
    <div class="table-wrap">
      <table class="figtable est-table">
        <thead>
          <tr>
            <th>Jurisdiction</th>
            @for (c of columns; track c) {
              <th>{{ c }}</th>
            }
          </tr>
        </thead>
        <tbody>
          @for (code of order; track code) {
            <tr>
              <td><a [routerLink]="['/estimated-tax', code]">{{ nameOf(code) }}</a></td>
              @for (c of rows.get(code); track $index) {
                <td>{{ c }}</td>
              }
            </tr>
          }
        </tbody>
      </table>
    </div>
  `,
})
export class EstimatedIndex {
  protected readonly year = estimatedYear;
  protected readonly note = estimatedNote;
  protected readonly columns = estimatedColumns;
  protected readonly rows = estimatedRows;
  protected readonly order = order;
  protected readonly nameOf = nameOf;

  constructor() {
    pageTitle(() => 'Estimated tax');
  }
}

/** One jurisdiction's estimated tax: its rules in brief, its form, and the rules, figures and law behind them. */
@Component({
  selector: 'app-estimated',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="crumbs()" />
    <p class="eyebrow">Estimated tax · tax year {{ year }}</p>
    <h1>{{ name() }} estimated tax</h1>

    @if (doc(); as d) {
      <div class="jgrid">
        <div>
          @if (d.row && d.row.slice(1).every((c) => !c)) {
            <p class="lede">{{ d.row[0] }}.</p>
          } @else if (d.row; as row) {
            <h2>The rules in brief</h2>
            <dl class="glance est-brief">
              @for (c of columns; track c; let i = $index) {
                @if (row[i]) {
                  <div><dt>{{ c }}</dt><dd>{{ row[i] }}</dd></div>
                }
              }
            </dl>
          }

          @if (d.forms.length) {
            <h2>{{ d.forms.length === 1 ? 'The form it fills' : 'The forms it fills' }}</h2>
            <div class="forms">
              @for (f of d.forms; track f.name) {
                <div class="form-card">
                  <div class="form-top"><strong>{{ formTitle(f) }}</strong></div>
                  <div class="muted small">{{ summary(f.doc) }}</div>
                  <div class="form-acts">
                    <a [routerLink]="['/rules', slug(f.name)]">Lines and rules</a>
                  </div>
                </div>
              }
            </div>
          } @else {
            <p class="muted">Nothing to fill in: the answer explains why no payments are needed.</p>
          }

          <h2>The rules</h2>
          <p class="muted small">
            Every rule in
            <a [href]="repo + file()" rel="noopener"><code>{{ file() }}</code></a>, in the order written.
          </p>
          <ul class="index">
            @for (r of d.rules; track r.name) {
              <li>
                <a [routerLink]="['/rules', slug(r.name)]">{{ title(r) }}</a>
                @if (r.doc) {
                  <div class="muted small">{{ summary(r.doc) }}</div>
                }
              </li>
            }
          </ul>
        </div>

        <aside>
          @if (d.figureSets.length) {
            <h2>Figures</h2>
            <ul class="srclist">
              @for (s of d.figureSets; track s.name) {
                <li>
                  <a [routerLink]="['/figures', s.name]">{{ figureSetTitle(s) }}</a>
                  <div class="muted small">{{ s.figures.length }} figures, each with the page that prints it</div>
                </li>
              }
            </ul>
          }
          @if (d.law.length) {
            <h2>Law</h2>
            <ul class="srclist">
              @for (l of d.law; track l.code + l.section) {
                <li>
                  @if (l.source) {
                    <a [routerLink]="['/law', l.source, l.section]">{{ l.code }} § {{ l.section }}</a>
                  } @else {
                    {{ l.code }} § {{ l.section }}
                  }
                </li>
              }
            </ul>
          }
          @if (d.sources.length) {
            <h2>Sources</h2>
            <ul class="srclist">
              @for (s of d.sources; track s.id) {
                <li>
                  <a [routerLink]="['/sources', s.id]">{{ s.designation }}</a>
                  <div class="muted small">{{ kindLabels[s.kind] }} · {{ s.title }}</div>
                </li>
              }
            </ul>
          }
          <p class="small">
            <a [routerLink]="['/', '2025', code()]">{{ name() }}'s 2025 return →</a><br />
            <a routerLink="/estimated-tax">Every jurisdiction's estimated tax →</a>
          </p>
        </aside>
      </div>
    }
  `,
})
export class EstimatedPage {
  /** The jurisdiction's code, from the route. */
  readonly code = input.required<string>();

  protected readonly year = estimatedYear;
  protected readonly columns = estimatedColumns;
  protected readonly repo = 'https://github.com/nexteggapp/taxcalcs/blob/main/';
  protected readonly slug = slug;
  protected readonly title = title;
  protected readonly summary = summary;
  protected readonly formTitle = formTitle;
  protected readonly figureSetTitle = figureSetTitle;
  protected readonly kindLabels = kindLabels;

  protected readonly name = computed(() => nameOf(this.code()));
  protected readonly doc = computed(() => estimatedFor(this.code()));
  protected readonly file = computed(() => this.doc().rules[0]?.file ?? '');
  protected readonly crumbs = computed(() => [
    { label: 'Jurisdictions', link: ['/'] },
    { label: 'Estimated tax', link: ['/estimated-tax'] },
    { label: this.name() },
  ]);

  constructor() {
    pageTitle(() => `${this.name()} estimated tax`);
  }
}
