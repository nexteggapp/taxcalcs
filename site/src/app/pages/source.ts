import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Anchor, Source, citing, figuresAt, implementer, kindLabels, pdfPath, sources, slug } from '../model';
import { figureSetTitle, humanize, title, value } from '../format';
import { Excerpt } from '../excerpt';
import { Crumbs, sourceCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';

export function coverage(s: Source): { implemented: number; skipped: number; informational: number } {
  const a = s.anchors ?? [];
  return {
    implemented: a.filter((x) => x.implementedBy).length,
    skipped: a.filter((x) => x.notModeled).length,
    informational: a.filter((x) => x.informational).length,
  };
}

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-source',
  imports: [RouterLink, Excerpt, Crumbs],
  template: `
    @if (source(); as s) {
      <app-crumbs [items]="crumbs()" />
      <p class="eyebrow">{{ kindLabels[s.kind] }} · {{ s.publisher.agency }}</p>
      <h1>{{ s.title }}</h1>
      <dl class="meta">
        <div><dt>Designation</dt><dd>{{ s.designation }}</dd></div>
        @if (s.revision) { <div><dt>Revision</dt><dd>{{ s.revision }}</dd></div> }
        <div><dt>Tax years</dt><dd>{{ s.taxYears.join(', ') }}</dd></div>
        @for (c of s.copies ?? []; track c.url) {
          <div><dt>Retrieved</dt><dd>{{ c.retrieved }} from <a [href]="c.url" target="_blank" rel="noopener">{{ host(c.url) }}</a></dd></div>
          <div><dt>SHA-256</dt><dd><code class="hash">{{ c.sha256 }}</code></dd></div>
        }
        @if (s.publisher.reuseNote) { <div><dt>Reuse</dt><dd class="muted">{{ s.publisher.reuseNote }}</dd></div> }
      </dl>
      @if (pdf(); as p) {
        <p class="actions">
          <a class="button" [href]="p" target="_blank" rel="noopener">Open the PDF</a>
          <a class="button" [href]="p" download>Download the PDF</a>
        </p>
      }

      @if (s.anchorScheme) {
        <section>
          <h2>Sections cited</h2>
          <p class="muted">Sections of the {{ s.designation }} are cited by number, so none is declared in advance.</p>
          <ul class="index">
            @for (c of cited(); track c.section) {
              <li>
                <a [routerLink]="['/law', s.id, c.section]">§ {{ c.section }}</a>
                <span class="muted">— cited by {{ c.count }} {{ c.count === 1 ? 'rule' : 'rules' }}</span>
              </li>
            }
          </ul>
        </section>
      } @else {
        <section>
          <h2>Parts the rules rely on</h2>
          <div class="covbar big"><span class="cov-impl" [style.flex]="cov().implemented"></span><span class="cov-skip" [style.flex]="cov().skipped"></span></div>
          <p class="muted small">{{ cov().implemented }} implemented · {{ cov().skipped }} deliberately left out</p>
          <div class="split">
            <nav class="anchors">
              @for (a of s.anchors ?? []; track a.id) {
                <a [routerLink]="['/sources', s.id, a.id]" [class.sel]="a.id === selected()?.id">
                  <span>{{ a.title }}</span>
                  <span class="pill" [class.skip]="!!a.notModeled">{{ a.notModeled ? 'left out' : a.at?.page ? 'p. ' + a.at?.page : '' }}</span>
                </a>
              }
            </nav>
            <div class="anchor-detail">
              @if (selected(); as a) {
                <h3>{{ a.title }}</h3>
                <p class="mono muted">#{{ a.id }}</p>
                @if (a.notModeled) {
                  <p class="note skip">Left out of the rules. {{ a.notModeled }}</p>
                }
                @if (impl(a); as i) {
                  @if (i.rule) {
                    <h4>Implemented by</h4>
                    <p>
                      @if (i.line) {
                        <a [routerLink]="['/rules', slug(i.rule.name)]" [fragment]="'line-' + i.line.number">Line {{ i.line.number }} of {{ title(i.rule) }}</a>:
                        {{ i.line.caption }}
                      } @else {
                        <a [routerLink]="['/rules', slug(i.rule.name)]">{{ title(i.rule) }}</a>
                      }
                    </p>
                    @for (c of i.rule.cites ?? []; track $index) {
                      @if (c.source) {
                        <p class="small">which cites <a [routerLink]="['/law', c.source, c.section]">{{ c.code }} § {{ c.section }}{{ c.sub }}</a></p>
                      }
                    }
                  }
                }
                @if (figures(a).length) {
                  <h4>Figures printed here</h4>
                  <table class="figs"><tbody>
                    @for (f of figures(a); track f.figure.field) {
                      <tr>
                        <td><a [routerLink]="['/figures', f.set.name]" [fragment]="f.figure.field">{{ label(f.set.figures.length === 1 ? figureSetTitle(f.set) : humanize(f.figure.field)) }}</a></td>
                        <td class="num">{{ value(f.figure.value) || (f.figure.rows ? f.figure.rows.length + ' brackets' : f.figure.value) }}</td>
                      </tr>
                    }
                  </tbody></table>
                }
                @if (a.at?.page) {
                  <h4>In the source</h4>
                  <app-excerpt [source]="s" [anchor]="a" />
                }
              } @else {
                <p class="muted">Pick a part to see where it is in the document and what in the rules computes it.</p>
              }
            </div>
          </div>
        </section>
      }
    } @else {
      <h1>No such source</h1>
    }
  `,
})
export class SourcePage {
  protected readonly slug = slug;
  /** The source's id and, on an anchor's page, the anchor's, from the route. */
  readonly id = input.required<string>();
  readonly anchor = input<string>();

  protected readonly kindLabels = kindLabels;
  protected readonly title = title;
  protected readonly humanize = humanize;
  protected readonly value = value;
  protected readonly figureSetTitle = figureSetTitle;
  protected readonly impl = implementer;

  protected readonly source = computed(() => sources.get(this.id()));
  protected readonly pdf = computed(() => (this.source() ? pdfPath(this.source()!) : undefined));
  protected readonly cov = computed(() => coverage(this.source()!));
  protected readonly selected = computed(() => {
    const s = this.source();
    const id = this.anchor();
    return s?.anchors?.find((a) => a.id === id) ?? (id ? undefined : s?.anchors?.find((a) => a.at?.page));
  });
  protected readonly cited = computed(() => {
    const s = this.source();
    const out = new Map<string, number>();
    for (const [key, rs] of citing) {
      const [src, section] = key.split('#');
      if (src === s?.id) out.set(section, rs.length);
    }
    return [...out].map(([section, count]) => ({ section, count })).sort((a, b) => a.section.localeCompare(b.section, undefined, { numeric: true }));
  });

  protected figures(a: Anchor) {
    return figuresAt.get(`${this.source()!.id}#${a.id}`) ?? [];
  }
  protected label(s: string) {
    return s.charAt(0).toUpperCase() + s.slice(1);
  }
  protected host(url: string) {
    return new URL(url).host;
  }

  protected readonly crumbs = computed(() => (this.source() ? [...sourceCrumbs(this.source()!), { label: this.source()!.designation }] : []));

  constructor() {
    pageTitle(() => { const s = this.source(); const a = this.selected(); return s ? (this.anchor() && a ? `${a.title} · ${s.designation} ${s.taxYears.join(', ')}` : `${s.title}`) : 'No such source'; });
  }
}
