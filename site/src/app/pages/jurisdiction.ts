import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgTemplateOutlet } from '@angular/common';
import { citing, figureSets, figuresByField, kindLabels as sourceKinds, model, slug } from '../model';
import { byCode, documented, kindLabels, untaxed } from '../jurisdictions';
import { figure, figureSetTitle, title, value } from '../format';
import { TreeNode, formCoverage, formTree, leftOut, sourcesFor } from '../tree';
import { Crumbs, jurisdictionCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';
import { estimatedColumns, estimatedRows, estimatedYear } from '../estimated';

/**
 * The note the return of a state with no income tax carries, and the law it
 * cites, read from that state's clause of the no-income-tax-state rule.
 */
function noIncomeTaxNote(code: string): { message: string; authorities: string[] } | undefined {
  const src = model.rules.find((r) => r.name === 'no-income-tax-state')?.source ?? '';
  const clause = src.match(
    new RegExp(`\\(is postal "${code.toUpperCase()}"\\)\\s*\\(NoIncomeTaxState "[^"]*" "[^"]*"\\s*"([^"]*)"\\s*\\[([^\\]]*)\\]`),
  );
  if (!clause) return undefined;
  return { message: clause[1], authorities: [...clause[2].matchAll(/"([^"]*)"/g)].map((a) => a[1]) };
}

/** What taxcalcs documents for one jurisdiction and year. */
@Component({
  selector: 'app-jurisdiction',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, NgTemplateOutlet, Crumbs],
  template: `
    <app-crumbs [items]="crumbs()" />
    <h1>{{ name() }}, tax year {{ year() }}</h1>

    @if (doc(); as d) {
      <p class="doc">
        This page documents how taxcalcs computes {{ name() }} income tax for {{ year() }}: the forms it prepares, how
        each line is calculated, the published figures and law each calculation relies on, and the parts of the
        return it leaves out. Follow a sample household through a form, or open any line to see its rules and sources.
      </p>
      <p class="muted small">{{ d.scope }}</p>

      <div class="summary">
        <h2 class="summary-head">How {{ name() }} taxes income</h2>
        <p class="doc">{{ d.summary }}</p>
        <p class="muted small">
          Computes {{ cov().computed }} of {{ cov().total }} lines on {{ d.forms[0].title }} · {{ cov().leftOut }} left
          out, each with a reason · {{ srcs().length }} sources
        </p>
      </div>

      <div class="jgrid">
        <div>
          <h2>Forms</h2>
          <div class="forms">
            @for (f of d.forms; track f.rule) {
              <div class="form-card">
                <div class="form-top">
                  <strong>{{ f.title }}</strong>
                  <span class="muted small">{{ f.feeds ? 'feeds ' + f.feeds : cov().computed + ' of ' + cov().total + ' lines' }}</span>
                </div>
                <div class="muted small">{{ f.subtitle }}</div>
                <div class="form-acts">
                  <a [routerLink]="['/rules', slug(f.rule)]">Lines and rules</a>
                  @if (f.walkthrough !== undefined) {
                    <a [routerLink]="['/walkthrough', d.form]" [fragment]="f.walkthrough || undefined">Follow a household through it</a>
                  }
                </div>
              </div>
            }
          </div>

          <h2>How {{ d.forms[0].title }} is computed</h2>
          <div class="tree">
            <div class="tree-row root">
              <a [routerLink]="['/rules', slug(d.form)]"><strong>{{ d.forms[0].title }}</strong></a>
            </div>
            <div class="tree-kids">
              @for (p of tree(); track p.caption) {
                <div class="tree-row">
                  <span>{{ p.caption }}</span>
                  <span class="tree-ln">{{ p.from === p.to ? 'line ' + p.from : 'lines ' + p.from + '–' + p.to }}</span>
                </div>
                @if (p.nodes.length) {
                  <div class="tree-kids">
                    @for (n of p.nodes; track n.rule.name) {
                      <ng-container [ngTemplateOutlet]="node" [ngTemplateOutletContext]="{ $implicit: n }" />
                    }
                  </div>
                }
              }
            </div>
          </div>

          <ng-template #node let-n>
            <div class="tree-row">
              <a [routerLink]="['/rules', slug(n.rule.name)]">{{ title(n.rule) }}</a>
              @if (tag(n); as t) {
                <span class="tag">{{ t }}</span>
              }
              @if (n.lines.length) {
                <span class="tree-ln">{{ n.lines.join(', ') }}</span>
              }
            </div>
            @if (n.children.length) {
              <div class="tree-kids">
                @for (c of n.children; track c.rule.name) {
                  <ng-container [ngTemplateOutlet]="node" [ngTemplateOutletContext]="{ $implicit: c }" />
                }
              </div>
            }
          </ng-template>

          <h2>Left out</h2>
          <ul class="leftout">
            @for (l of omitted(); track l.source.id + l.anchor) {
              <li>
                <a [routerLink]="['/sources', l.source.id, l.anchor]">{{ l.title }}</a>
                <span class="muted"> — {{ l.reason }}</span>
              </li>
            }
          </ul>
        </div>

        <aside>
          <h2>{{ year() }} at a glance</h2>
          <dl class="glance">
            @for (g of glance(); track g.label) {
              <div><dt>{{ g.label }}</dt><dd>{{ g.value }}</dd></div>
            }
            @if (topRate(); as r) {
              <div><dt>Top rate</dt><dd>{{ r }}</dd></div>
            }
          </dl>
          @if (figureSet(); as fs) {
            <a class="small" [routerLink]="['/figures', fs]">All {{ year() }} figures →</a>
          }

          <ng-container [ngTemplateOutlet]="estimated" />

          <h2>Sources</h2>
          <ul class="srclist">
            @for (s of srcs(); track s.id) {
              <li>
                <a [routerLink]="['/sources', s.id]">{{ s.designation }}</a>
                <div class="muted small">{{ sourceKinds[s.kind] }} · {{ sourceUse(s) }}</div>
              </li>
            }
          </ul>
        </aside>
      </div>
    } @else if (untaxedDoc(); as u) {
      <p class="doc">
        This page documents how taxcalcs handles {{ name() }} for {{ year() }}: the return it prepares, the rules behind
        it, and the law they rest on.
      </p>

      <div class="summary">
        <h2 class="summary-head">How {{ name() }} taxes income</h2>
        <p class="doc">{{ u.summary ?? note()?.message }}</p>
      </div>

      <div class="jgrid">
        <div>
          <h2>What taxcalcs prepares</h2>
          <div class="forms">
            @for (f of u.rules; track f.rule) {
              <div class="form-card">
                <div class="form-top"><strong>{{ f.title }}</strong></div>
                <div class="muted small">{{ f.subtitle }}</div>
                <div class="form-acts">
                  <a [routerLink]="['/rules', slug(f.rule)]">Rules</a>
                </div>
              </div>
            }
          </div>

          <h2>For a {{ name() }} resident</h2>
          <ul class="checks">
            @if (u.summary) {
              <li>Prepares {{ name() }}'s capital gains return, with no tax when no gain is above the standard deduction</li>
            } @else {
              <li>Prepares a complete {{ name() }} return with no tax, carrying the note below</li>
            }
            <li>Expects no state withholding on a {{ name() }} W-2</li>
            <li>Plans no state estimated tax payments on wages</li>
            <li>A move to or from another state is handled on that state's part-year return</li>
          </ul>

          @if (note(); as n) {
            <h2>The law</h2>
            <p class="muted small">The note on the return cites:</p>
            <ul class="srclist">
              @for (a of n.authorities; track a) {
                <li>{{ a }}</li>
              }
            </ul>
          }
        </div>

        <aside>
          <ng-container [ngTemplateOutlet]="estimated" />
          @if (glance().length) {
            <h2>{{ year() }} at a glance</h2>
            <dl class="glance">
              @for (g of glance(); track g.label) {
                <div><dt>{{ g.label }}</dt><dd>{{ g.value }}</dd></div>
              }
            </dl>
            @if (figureSet(); as fs) {
              <a class="small" [routerLink]="['/figures', fs]">All {{ year() }} figures →</a>
            }
          }
          @if (srcs().length) {
            <h2>Sources</h2>
            <ul class="srclist">
              @for (s of srcs(); track s.id) {
                <li>
                  <a [routerLink]="['/sources', s.id]">{{ s.designation }}</a>
                  <div class="muted small">{{ sourceKinds[s.kind] }} · {{ sourceUse(s) }}</div>
                </li>
              }
            </ul>
          }
        </aside>
      </div>
    } @else {
      <p class="doc">
        This page will document how taxcalcs computes {{ name() }} income tax for {{ year() }}.
        @if (j(); as s) {
          taxcalcs already estimates {{ name() }}'s quarterly tax, but its {{ s.form }} is not yet prepared from
          documented rules.
        } @else {
          Its rules are not yet documented.
        }
      </p>
      @if (j(); as s) {
        <p class="muted">{{ kindLabels[s.kind] }}.</p>
      }
      @if (est()) {
        <p><a [routerLink]="['/estimated-tax', code()]">{{ name() }}'s estimated tax →</a></p>
      }
      <p><a routerLink="/2025/ca">See California, which is documented →</a></p>
    }

    <ng-template #estimated>
      @if (est(); as row) {
        <h2>Estimated tax, {{ estYear }}</h2>
        <dl class="glance est-glance">
          @for (c of estColumns.slice(0, 3); track c; let i = $index) {
            @if (row[i]) {
              <div><dt>{{ c }}</dt><dd>{{ row[i] }}</dd></div>
            }
          }
        </dl>
        <a class="small" [routerLink]="['/estimated-tax', code()]">Rules, form and figures →</a>
      }
    </ng-template>
  `,
})
export class JurisdictionPage {
  /** The year and jurisdiction code, from the route. */
  readonly year = input.required<string>();
  readonly code = input.required<string>();

  protected readonly slug = slug;
  protected readonly title = title;
  protected readonly kindLabels = kindLabels;
  protected readonly sourceKinds = sourceKinds;

  protected readonly j = computed(() => byCode.get(this.code()));
  protected readonly est = computed(() => estimatedRows.get(this.code()));
  protected readonly estYear = estimatedYear;
  protected readonly estColumns = estimatedColumns;
  protected readonly name = computed(() => (this.code() === 'us' ? 'Federal' : (this.j()?.name ?? this.code().toUpperCase())));
  protected readonly crumbs = computed(() => jurisdictionCrumbs(this.code()));
  protected readonly doc = computed(() => documented[this.code()]);
  protected readonly cov = computed(() => formCoverage(this.doc()!.formSource));
  protected readonly srcs = computed(() => sourcesFor(this.code(), Number(this.year())));
  protected readonly tree = computed(() => formTree(this.doc()!.form));
  protected readonly omitted = computed(() => leftOut(this.srcs()));
  protected readonly untaxedDoc = computed(() => (this.doc() ? undefined : untaxed[this.code()]));
  protected readonly note = computed(() => noIncomeTaxNote(this.code()));
  protected readonly glance = computed(() =>
    (this.doc()?.glance ?? this.untaxedDoc()?.glance ?? []).map((g) => ({
      label: g.label,
      value: g.keys.map((k) => figure(k, figuresByField.get(k)?.figure.value ?? '')).join(' / ') + (g.suffix ?? ''),
    })),
  );
  protected readonly figureSet = computed(() => model.figureSets.find((f) => f.type.startsWith(this.name().replace(/\s/g, '')))?.name);
  protected readonly topRate = computed(() => {
    const d = this.doc();
    const code = this.code();
    // The state's named schedule; or, where the glance names no rate, its first bracket schedule.
    const named = (d?.glance ?? []).some((g) => /rate/i.test(g.label));
    const rows = model.figureSets.find(
      (f) =>
        f.name === d?.schedule ||
        (!d?.schedule && !named && f.name.startsWith(`${code}-`) && f.type === '(Slice StateBracket)'),
    )?.figures[0]?.rows;
    const extra = d?.surtax ? figuresByField.get(d.surtax) : undefined;
    if (!rows?.length) return '';
    const top = Number(rows.at(-1)![2]) / 100;
    return extra ? `${top}% + ${Number(extra.figure.value) / 100}%` : `${top}%`;
  });

  protected tag(n: TreeNode): string {
    if (n.rule.kind === 'worksheet') return 'worksheet';
    if (n.rule.kind === 'predicate') return 'test';
    if (n.rule.kind === 'form' || n.rule.name.endsWith('schedule-ca')) return 'schedule';
    if ((n.rule.figures ?? []).some((f) => figureSets.get(f)?.figures.length === 1)) return 'table';
    return '';
  }
  protected sourceUse(s: { id: string; anchors?: { implementedBy?: string; notModeled?: string }[]; anchorScheme?: unknown }): string {
    if (s.anchorScheme) {
      const n = [...citing.keys()].filter((k) => k.startsWith(s.id + '#')).length;
      return `${n} ${n === 1 ? 'section' : 'sections'} cited`;
    }
    const a = s.anchors ?? [];
    const used = a.filter((x) => x.implementedBy).length;
    const out = a.filter((x) => x.notModeled).length;
    return `${used} parts used${out ? `, ${out} left out` : ''}`;
  }
  protected readonly figureSetTitle = figureSetTitle;
  protected readonly value = value;

  constructor() {
    pageTitle(() => `${this.name()} ${this.year()}`);
  }
}
