import { ChangeDetectionStrategy, Component, computed, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { kindLabels, model, ruleKindLabels, slug } from '../model';
import { summary, title } from '../format';
import { Crumbs } from '../crumbs';
import { pageTitle } from '../page-title';

/** Every source in the catalog, by jurisdiction and year. */
@Component({
  selector: 'app-sources-index',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="[{ label: 'Jurisdictions', link: ['/'] }, { label: 'Sources' }]" />
    <h1>Sources</h1>
    <p class="doc">
      The published texts the rules rely on, as retrieved and hashed. Each lists the parts the rules implement and
      the parts they leave out.
    </p>
    <div class="table-wrap"><table class="figtable">
      <thead><tr><th>Source</th><th>Kind</th><th>Publisher</th><th>Years</th><th>Parts</th></tr></thead>
      <tbody>
        @for (s of model.sources; track s.id) {
          <tr>
            <td><a [routerLink]="['/sources', s.id]">{{ s.title }}</a><div class="muted small">{{ s.designation }}</div></td>
            <td>{{ kindLabels[s.kind] }}</td>
            <td>{{ s.publisher.agency }}</td>
            <td>{{ s.taxYears.join(', ') }}</td>
            <td class="num">{{ s.anchors?.length ? s.anchors!.length : 'by section' }}</td>
          </tr>
        }
      </tbody>
    </table></div>
  `,
})
export class SourcesIndex {
  constructor() {
    pageTitle(() => 'Sources');
  }

  protected readonly model = model;
  protected readonly kindLabels = kindLabels;
}

/** Every rule, A to Z, with a filter. */
@Component({
  selector: 'app-rules-index',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="[{ label: 'Jurisdictions', link: ['/'] }, { label: 'Rules A–Z' }]" />
    <h1>Rules A–Z</h1>
    <p class="doc">Every calculation, test, worksheet and form the documented rules define, including the helpers the states share.</p>
    <p class="kinds">
      <a routerLink="/rules" [class.on]="!kind()">All</a>
      @for (k of kinds; track k) {
        <a routerLink="/rules" [queryParams]="{ kind: k }" [class.on]="kind() === k">{{ plural[k] }}</a>
      }
    </p>
    <input class="filter" type="search" placeholder="Filter by name or description" [value]="q()" (input)="q.set($any($event.target).value)" aria-label="Filter rules" />
    <ul class="az">
      @for (r of shown(); track r.name) {
        <li>
          <a [routerLink]="['/rules', slug(r.name)]">{{ title(r) }}</a>
          <span class="badge small">{{ ruleKindLabels[r.kind] }}</span>
          <div class="muted small">{{ summary(r.doc) }}</div>
        </li>
      } @empty {
        <li class="muted">No rule matches.</li>
      }
    </ul>
  `,
})
export class RulesIndex {
  /** The kind of rule to list, from ?kind=form; all kinds if unset. */
  readonly kind = input<string>();

  constructor() {
    pageTitle(() => 'Rules A–Z');
  }

  protected readonly slug = slug;
  protected readonly title = title;
  protected readonly summary = summary;
  protected readonly ruleKindLabels = ruleKindLabels;
  protected readonly q = signal('');
  protected readonly kinds = ['form', 'worksheet', 'predicate', 'def', 'struct'];
  protected readonly plural: Record<string, string> = {
    form: 'Forms',
    worksheet: 'Worksheets',
    predicate: 'Tests',
    def: 'Calculations',
    struct: 'Figure types',
  };
  private readonly all = [...model.rules].sort((a, b) => title(a).localeCompare(title(b)));
  protected readonly shown = computed(() => {
    const q = this.q().toLowerCase().trim();
    const kind = this.kind();
    const all = kind ? this.all.filter((r) => r.kind === kind) : this.all;
    return q ? all.filter((r) => (title(r) + ' ' + r.name + ' ' + r.doc).toLowerCase().includes(q)) : all;
  });
}
