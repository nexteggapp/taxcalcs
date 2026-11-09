import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgTemplateOutlet } from '@angular/common';
import { citing, figureSets, figuresByField, kindLabels as sourceKinds, model, slug } from '../model';
import { byCode, documented, kindLabels } from '../jurisdictions';
import { figureSetTitle, money, title, value } from '../format';
import { TreeNode, formCoverage, formTree, leftOut, sourcesFor } from '../tree';
import { Crumbs, jurisdictionCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';

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
    } @else if (j()?.kind === 'none') {
      <p class="doc">This page documents how taxcalcs handles {{ name() }} income tax for {{ year() }}: what it computes, and why.</p>
      <div class="summary">
        <h2 class="summary-head">How {{ name() }} taxes income</h2>
        <p class="doc">{{ name() }} has no income tax on wages, so taxcalcs prepares no {{ name() }} return.</p>
      </div>
      <h2>What taxcalcs does for a {{ name() }} resident</h2>
      <ul class="checks">
        <li>Prepares no state return, and reports {{ name() }} as not required</li>
        <li>Expects no state withholding on a {{ name() }} W-2</li>
        <li>Plans no state estimated tax payments</li>
        <li>A move to or from another state is handled on that state's part-year return</li>
      </ul>
      <p class="muted small">The law that says so is not yet in the source catalog.</p>
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
      <p><a routerLink="/2025/ca">See California, which is documented →</a></p>
    }
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
  protected readonly name = computed(() => (this.code() === 'us' ? 'Federal' : (this.j()?.name ?? this.code().toUpperCase())));
  protected readonly crumbs = computed(() => jurisdictionCrumbs(this.code()));
  protected readonly doc = computed(() => documented[this.code()]);
  protected readonly cov = computed(() => formCoverage(this.doc()!.formSource));
  protected readonly srcs = computed(() => sourcesFor(this.code(), Number(this.year())));
  protected readonly tree = computed(() => formTree(this.doc()!.form));
  protected readonly omitted = computed(() => leftOut(this.srcs()));
  protected readonly glance = computed(() =>
    (this.doc()?.glance ?? []).map((g) => ({
      label: g.label,
      value: g.keys.map((k) => money(figuresByField.get(k)?.figure.value ?? '').replace(/\.00$/, '')).join(' / ') + (g.suffix ?? ''),
    })),
  );
  protected readonly figureSet = computed(() => model.figureSets.find((f) => f.type.startsWith(this.name().replace(/\s/g, '')))?.name);
  protected readonly topRate = computed(() => {
    const rows = model.figureSets.find((f) => f.name.startsWith(`${this.code()}-schedule-x`) || f.name.startsWith(`${this.code()}-chart-s`))?.figures[0]?.rows;
    const extra = figuresByField.get('CaliforniaYear.behavioral_health_rate_bp');
    if (!rows?.length) return '';
    const top = Number(rows.at(-1)![2]) / 100;
    return extra && this.code() === 'ca' ? `${top}% + ${Number(extra.figure.value) / 100}%` : `${top}%`;
  });

  protected tag(n: TreeNode): string {
    if (n.rule.kind === 'worksheet') return 'worksheet';
    if (n.rule.kind === 'predicate') return 'test';
    if (n.rule.name.endsWith('schedule-ca')) return 'schedule';
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
