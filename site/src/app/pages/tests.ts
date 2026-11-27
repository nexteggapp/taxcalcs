import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { CaseSource, model, rules, slug, sources, testCases } from '../model';
import { Crumbs } from '../crumbs';
import { pageTitle } from '../page-title';

const repo = 'https://github.com/nexteggapp/taxcalcs/blob/main';

/** Provenances an agency printed, which alone count as authoritative. */
const authoritative = new Set(['published example', 'published table']);

/** The source a case cites, as a link target and a label. */
function cite(s?: CaseSource): { link: string[]; label: string } | undefined {
  if (!s) return undefined;
  const title = sources.get(s.id)?.title ?? s.id;
  if (s.anchor) return { link: ['/sources', s.id, s.anchor], label: `${title}, ${s.anchor}` };
  return { link: ['/sources', s.id], label: s.page ? `${title}, PDF page ${s.page}` : title };
}

/** Every end-to-end test case, filtered by jurisdiction, provenance and tag, and each form's coverage. */
@Component({
  selector: 'app-tests-index',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    <app-crumbs [items]="[{ label: 'Home', link: ['/'] }, { label: 'Test cases' }]" />
    <h1>Test cases</h1>
    <p class="doc">
      Each case is a household and the figures expected on its returns, line by line. Every figure says where it came
      from. Only figures an agency printed, a worked example or a tax table, count as authoritative. A known gap is a
      figure the rules do not produce yet; the case starts passing when they do.
    </p>
    <p class="kinds">
      <a [class.on]="!jur()" (click)="jur.set('')">All</a>
      @for (j of jurisdictions; track j) {
        <a [class.on]="jur() === j" (click)="jur.set(j)">{{ j }}</a>
      }
    </p>
    <input class="filter" type="search" placeholder="Filter by title or tag" [value]="q()" (input)="q.set($any($event.target).value)" aria-label="Filter test cases" />
    <div class="table-wrap"><table class="figtable">
      <thead><tr><th>Case</th><th>Jurisdiction</th><th>Figures</th><th>Authoritative</th><th>Passing</th><th>Known gaps</th></tr></thead>
      <tbody>
        @for (c of shown(); track c.id) {
          <tr>
            <td><a [routerLink]="['/tests', c.id]">{{ c.title }}</a><div class="muted small">{{ c.tags.join(', ') }}</div></td>
            <td>{{ c.jurisdiction }}</td>
            <td class="num">{{ c.figures.length }}</td>
            <td class="num">{{ authCount(c.figures) }}</td>
            <td class="num">{{ c.passing }}</td>
            <td class="num">{{ c.gaps }}</td>
          </tr>
        } @empty {
          <tr><td colspan="6" class="muted">No case matches.</td></tr>
        }
      </tbody>
    </table></div>

    <h2>Coverage by form</h2>
    <p class="doc">Each line of the forms the cases reach, marked by the strongest source that checks it.</p>
    @for (f of model.testCoverage; track f.form) {
      <h3>@if (hasRule(f.form)) { <a [routerLink]="['/rules', slug(f.form)]">{{ f.form }}</a> } @else { {{ f.form }} }</h3>
      <p class="coverage">
        @for (l of f.lines; track l.line) {
          <span class="badge small" [class.on]="isAuth(l.provenance)" [class.muted]="!l.provenance" [title]="l.caption + (l.provenance ? ': ' + l.provenance : ': not checked')">{{ l.line }}</span>
        }
      </p>
    }
  `,
})
export class TestsIndex {
  constructor() {
    pageTitle(() => 'Test cases');
  }

  protected readonly model = model;
  protected readonly jurisdictions = [...new Set(model.testCases.map((c) => c.jurisdiction))].sort();
  protected readonly jur = signal('');
  protected readonly q = signal('');
  protected readonly shown = computed(() => {
    const q = this.q().toLowerCase();
    return model.testCases.filter(
      (c) =>
        (!this.jur() || c.jurisdiction === this.jur()) &&
        (!q || c.title.toLowerCase().includes(q) || c.tags.some((t) => t.includes(q))),
    );
  });

  protected authCount(fs: { provenance: string }[]): number {
    return fs.filter((f) => authoritative.has(f.provenance)).length;
  }

  protected isAuth(p?: string): boolean {
    return !!p && authoritative.has(p);
  }

  protected readonly slug = slug;

  protected hasRule(name: string): boolean {
    return rules.has(name);
  }
}

/** One test case: the household, then each expected figure with its source, working and outcome. */
@Component({
  selector: 'app-test-case',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Crumbs],
  template: `
    @if (c(); as c) {
      <app-crumbs [items]="[{ label: 'Home', link: ['/'] }, { label: 'Test cases', link: ['/tests'] }, { label: c.id }]" />
      <p class="eyebrow">Test case · {{ c.jurisdiction }} · tax year {{ c.year }}</p>
      <h1>{{ c.title }}</h1>
      <p class="lede">
        {{ c.passing }} of {{ c.figures.length }} figures pass.
        @if (c.gaps) { {{ c.gaps }} are known gaps: figures the rules do not produce yet. }
      </p>
      @if (c.note) { <p class="doc">{{ c.note }}</p> }

      <h2>Household</h2>
      <p class="muted small">
        Where the household comes from: {{ c.origin }}.
        @if (cite(c.originSource); as s) { <a [routerLink]="s.link">{{ s.label }}</a>. }
        {{ c.originNote }}
      </p>
      <dl class="facts">
        @for (f of c.facts; track $index) {
          <dt>{{ f.label }}</dt><dd>{{ f.value }}</dd>
        }
      </dl>

      <h2>Expected figures</h2>
      <div class="table-wrap"><table class="figtable">
        <thead><tr><th>Form</th><th>Line</th><th>Expected</th><th>Rules today</th><th>Source of the expected figure</th></tr></thead>
        <tbody>
          @for (f of c.figures; track $index) {
            <tr [class.gap]="f.outcome !== 'pass'">
              <td>@if (hasRule(f.form)) { <a [routerLink]="['/rules', slug(f.form)]">{{ f.form }}</a> } @else { {{ f.form }} }</td>
              <td>{{ f.line }}<div class="muted small">{{ f.caption }}</div></td>
              <td class="num">{{ f.expected }}</td>
              <td class="num">{{ f.got }}<div class="small" [class.muted]="f.outcome === 'pass'">{{ f.outcome }}</div></td>
              <td>
                <span class="badge small" [class.on]="isAuth(f.provenance)">{{ f.provenance }}</span>
                @if (cite(f.source); as s) { <div class="small"><a [routerLink]="s.link">{{ s.label }}</a></div> }
                @for (w of f.working ?? []; track $index) {
                  <div class="small">{{ w.what }}: <code>{{ w.arithmetic }}</code></div>
                }
                @if (f.gap) {
                  <div class="small muted">Known gap: {{ f.gap }} @if (f.gapIssue) { <a [href]="f.gapIssue">issue</a> }</div>
                }
              </td>
            </tr>
          }
        </tbody>
      </table></div>
      <p class="muted small"><a [href]="repo + '/' + c.path">The case file</a></p>
    } @else {
      <h1>No such test case</h1>
      <p><a routerLink="/tests">All test cases</a></p>
    }
  `,
})
export class TestCasePage {
  private readonly params = toSignal(inject(ActivatedRoute).paramMap);
  protected readonly c = computed(() => testCases.get(this.params()?.get('id') ?? ''));
  protected readonly repo = repo;
  protected readonly cite = cite;

  constructor() {
    pageTitle(() => this.c()?.title ?? 'Test case');
  }

  protected isAuth(p?: string): boolean {
    return !!p && authoritative.has(p);
  }

  protected readonly slug = slug;

  protected hasRule(name: string): boolean {
    return rules.has(name);
  }
}
