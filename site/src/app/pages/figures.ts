import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Figure, anchor, figureSets, readers, sources, slug } from '../model';
import { figureSetTitle, humanize, money, title, value } from '../format';
import { Crumbs, jurisdictionCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-figures',
  imports: [RouterLink, Crumbs],
  template: `
    @if (set(); as s) {
      <app-crumbs [items]="crumbs()" />
      <p class="eyebrow">Figures · {{ s.year }} · <code>{{ s.name }}</code></p>
      <h1>{{ figureSetTitle(s) }}</h1>
      @if (s.doc) { <p class="doc">{{ s.doc }}</p> }
      <p class="muted">Each figure names the part of the published source that prints it.</p>
      @if (s.figures.length === 1 && s.figures[0].rows; as rows) {
        <div class="table-wrap"><table class="rates">
          <thead><tr><th>Taxable income over</th><th>But not over</th><th>Rate on the excess</th></tr></thead>
          <tbody>
            @for (r of rows; track $index) {
              <tr><td class="num">{{ money(r[0]) }}</td><td class="num">{{ r[1].startsWith('9999') ? '—' : money(r[1]) }}</td><td class="num">{{ +r[2] / 100 }}%</td></tr>
            }
          </tbody>
        </table></div>
        <p>
          Printed in <a [routerLink]="['/sources', s.figures[0].source, s.figures[0].anchor]">{{ anchorTitle(s.figures[0]) }}</a>
          <span class="muted"> of {{ sourceTitle(s.figures[0]) }}</span>
        </p>
        @if (readBy(s.name).length) {
          <p>Read by @for (r of readBy(s.name); track r) { <a [routerLink]="['/rules', slug(r)]">{{ title(r) }}</a> }</p>
        }
      } @else {
        <div class="table-wrap"><table class="figtable">
          <thead><tr><th>Figure</th><th>Value</th><th>Printed in</th><th>Read by</th></tr></thead>
          <tbody>
            @for (f of s.figures; track f.field) {
              <tr [id]="f.field">
                <td>
                  <strong>{{ cap(humanize(f.field)) }}</strong>
                  @if (f.doc) { <div class="muted small">{{ f.doc }}</div> }
                </td>
                <td class="num">{{ value(f.value) || f.value }}</td>
                <td><a [routerLink]="['/sources', f.source, f.anchor]">{{ anchorTitle(f) }}</a>, p. {{ page(f) }}</td>
                <td>
                  @for (r of readBy(s.type + '.' + f.field); track r) {
                    <a [routerLink]="['/rules', slug(r)]" class="chip">{{ title(r) }}</a>
                  }
                </td>
              </tr>
            }
          </tbody>
        </table></div>
      }
    } @else {
      <h1>No such figures</h1>
    }
  `,
})
export class FiguresPage {
  protected readonly slug = slug;
  /** The figure set's name, from the route. */
  readonly name = input.required<string>();

  protected readonly set = computed(() => figureSets.get(this.name()));
  protected readonly figureSetTitle = figureSetTitle;
  protected readonly humanize = humanize;
  protected readonly value = value;
  protected readonly money = money;
  protected readonly title = title;

  protected readBy(key: string) {
    return readers.get(key) ?? [];
  }
  protected anchorTitle(f: Figure) {
    return anchor({ source: f.source, anchor: f.anchor })?.title ?? f.anchor;
  }
  protected sourceTitle(f: Figure) {
    return sources.get(f.source)?.title ?? f.source;
  }
  protected page(f: Figure) {
    return anchor({ source: f.source, anchor: f.anchor })?.at?.page ?? '';
  }
  protected cap(s: string) {
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  protected readonly crumbs = computed(() => [...jurisdictionCrumbs('ca'), { label: 'Figures' }]);

  constructor() {
    pageTitle(() => (this.set() ? figureSetTitle(this.set()!) : 'No such figures'));
  }
}
