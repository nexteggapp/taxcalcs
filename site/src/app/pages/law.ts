import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { citing, sources, slug } from '../model';
import { summary, title } from '../format';
import { Crumbs, sourceCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-law',
  imports: [RouterLink, Crumbs],
  template: `
    @if (statute(); as s) {
      <app-crumbs [items]="crumbs()" />
      <p class="eyebrow">{{ s.title }} · {{ s.revision }}</p>
      <h1>{{ s.designation }} § {{ section() }}</h1>
      <p>
        <a class="button" [href]="url()" target="_blank" rel="noopener">Read § {{ section() }} on the Legislature's site</a>
      </p>
      <h2>Rules that cite it</h2>
      <ul class="index">
        @for (r of rules(); track r.name) {
          <li>
            <a [routerLink]="['/rules', slug(r.name)]">{{ title(r) }}</a>
            <div class="muted small">{{ summary(r.doc) }}</div>
            @for (a of r.anchors ?? []; track a.anchor) {
              <div class="small">implements <a [routerLink]="['/sources', a.source, a.anchor]">{{ a.anchor }}</a> in {{ a.source }}</div>
            }
          </li>
        }
      </ul>
    } @else {
      <h1>No such source</h1>
    }
  `,
})
export class LawPage {
  protected readonly slug = slug;
  /** The statute source and the section cited, from the route. */
  readonly source = input.required<string>();
  readonly section = input.required<string>();

  protected readonly title = title;
  protected readonly summary = summary;
  protected readonly statute = computed(() => sources.get(this.source()));
  protected readonly url = computed(() => this.statute()?.anchorScheme?.urlTemplate.replace('{section}', this.section()) ?? '');
  protected readonly rules = computed(() => citing.get(`${this.source()}#${this.section()}`) ?? []);

  protected readonly crumbs = computed(() => (this.statute() ? [...sourceCrumbs(this.statute()!), { label: '§ ' + this.section() }] : []));

  constructor() {
    pageTitle(() => (this.statute() ? `${this.statute()!.designation} § ${this.section()}` : 'No such law'));
  }
}
