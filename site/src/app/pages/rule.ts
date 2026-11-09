import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { AnchorRef, Cite, Item, anchor, callers, figuresByField, followed, readers, ruleKindLabels, rulesBySlug, sources, slug } from '../model';
import { contextFor, figureSetTitle, humanize, render, title, value } from '../format';
import { Excerpt } from '../excerpt';
import { Links } from '../links';
import { Crumbs, ruleCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-rule',
  imports: [RouterLink, Excerpt, Links, Crumbs],
  template: `
    @if (rule(); as r) {
      <app-crumbs [items]="crumbs()" />
      <p class="eyebrow">{{ ruleKindLabels[r.kind] }} · <code>{{ r.name }}</code></p>
      <h1>{{ title(r) }}</h1>
      @for (p of paragraphs(); track $index) {
        <p class="doc">{{ p }}</p>
      }

      @if (r.cites?.length) {
        <p class="cites">
          <span class="muted">Cites:</span>
          @for (c of r.cites; track $index) {
            @if (c.code && c.source) {
              <a [routerLink]="['/law', c.source, c.section]">{{ c.code }} § {{ c.section }}{{ c.sub }}</a>
            } @else if (c.source) {
              <a [routerLink]="c.anchor ? ['/sources', c.source, c.anchor] : ['/sources', c.source]">{{ docCite(c) }}</a>
            } @else {
              <span>{{ c.text }}</span>
            }
          }
        </p>
      }

      @if (r.kind === 'form' && followed.has(r.name)) {
        <p><a class="cta" [routerLink]="['/walkthrough', r.name]">Walk through this form for a household →</a></p>
      }

      @if (r.anchors?.length) {
        <section>
          <h2>Where it is published</h2>
          @for (a of r.anchors; track a.anchor) {
            <p>
              <a [routerLink]="['/sources', a.source, a.anchor]">{{ anchorTitle(a) }}</a>
              <span class="muted"> in {{ sourceTitle(a) }}</span>
            </p>
          }
          @if (firstAnchor(); as fa) {
            <app-excerpt [source]="fa.source" [anchor]="fa.anchor" />
          }
        </section>
      }

      @if (r.parts?.length) {
        <section appLinks>
          <h2>{{ r.kind === 'form' ? 'Lines' : 'The worksheet' }}</h2>
          @for (p of r.parts; track $index) {
            @if (p.caption) {
              <h3>{{ p.caption }}</h3>
            }
            <table class="lines"><tbody>
              @for (it of p.items; track $index) {
                @if (it.kind === 'line') {
                  <tr [id]="'line-' + it.number">
                    <td class="ln">{{ it.number }}</td>
                    <td>
                      <div class="cap">{{ it.caption }}</div>
                      <div class="formula" [innerHTML]="formula(it)"></div>
                    </td>
                    <td class="src">
                      @for (a of it.anchors ?? []; track a.anchor) {
                        <a [routerLink]="['/sources', a.source, a.anchor]">on the form</a>
                      }
                    </td>
                  </tr>
                } @else if (it.kind !== 'result') {
                  <tr class="aux">
                    <td class="ln"></td>
                    <td>
                      <div class="cap">{{ auxCaption(it) }}</div>
                      <div class="formula" [innerHTML]="formula(it)"></div>
                    </td>
                    <td></td>
                  </tr>
                }
              }
            </tbody></table>
          }
        </section>
      }

      @if (r.kind === 'struct') {
        <section>
          <h2>Fields</h2>
          <table class="figs"><tbody>
            @for (p of r.params ?? []; track p.name) {
              <tr><td><code>{{ p.name }}</code></td><td class="muted">{{ p.doc }}</td></tr>
            }
          </tbody></table>
        </section>
      } @else if (r.params?.length) {
        <section>
          <h2>What it takes</h2>
          <table class="figs"><tbody>
            @for (p of r.params ?? []; track p.name) {
              <tr><td><var>{{ humanize(p.name) }}</var></td><td class="muted"><code>{{ p.type }}</code></td></tr>
            }
          </tbody></table>
          <p class="muted small">It gives a <code>{{ r.returns }}</code>.</p>
        </section>
      }

      @if (figures().length) {
        <section>
          <h2>Figures it reads</h2>
          <ul class="index">
            @for (f of figures(); track f.key) {
              <li><a [routerLink]="['/figures', f.set]" [fragment]="f.field">{{ f.label }}</a> <span class="num muted">{{ f.value }}</span></li>
            }
          </ul>
        </section>
      }

      <section class="two-col">
        @if (r.calls?.length) {
          <div>
            <h2>Uses</h2>
            <ul class="index">
              @for (c of r.calls; track c) { <li><a [routerLink]="['/rules', slug(c)]">{{ title(c) }}</a></li> }
            </ul>
          </div>
        }
        @if (usedBy().length) {
          <div>
            <h2>Used by</h2>
            <ul class="index">
              @for (c of usedBy(); track c) { <li><a [routerLink]="['/rules', slug(c)]">{{ title(c) }}</a></li> }
            </ul>
          </div>
        }
      </section>

      <section>
        <h2>The rule as written</h2>
        <p class="muted small"><code>{{ r.file }}</code>, line {{ r.line }}</p>
        <pre class="code">{{ r.source }}</pre>
      </section>
    } @else {
      <h1>No such rule</h1>
    }
  `,
})
export class RulePage {
  protected readonly slug = slug;
  protected readonly followed = followed;
  /** The rule's slug, from the route. */
  readonly name = input.required<string>();

  protected readonly ruleKindLabels = ruleKindLabels;
  protected readonly title = title;
  protected readonly humanize = humanize;

  /** A document cite as it reads: "Form 4797 Instructions, Line 8". */
  protected docCite(c: Cite): string {
    const src = sources.get(c.source!);
    const name = src?.designation ?? c.source!;
    const part = c.anchor ? src?.anchors?.find((a) => a.id === c.anchor)?.title : undefined;
    return part ? `${name}, ${part}` : name;
  }

  protected readonly rule = computed(() => rulesBySlug.get(this.name()));
  protected readonly ctx = computed(() => contextFor(this.rule()!));
  protected readonly paragraphs = computed(() => (this.rule()?.doc ?? '').split('\n\n').filter((p) => p));
  protected readonly usedBy = computed(() => callers.get(this.rule()?.name ?? '') ?? []);
  protected readonly firstAnchor = computed(() => {
    for (const ref of this.rule()?.anchors ?? []) {
      const a = anchor(ref);
      if (a?.at?.page) return { source: sources.get(ref.source)!, anchor: a };
    }
    return undefined;
  });
  protected readonly figures = computed(() =>
    (this.rule()?.figures ?? []).map((key) => {
      const ref = figuresByField.get(key);
      const field = ref?.figure.field ?? key;
      return {
        key,
        set: ref?.set.name ?? '',
        field,
        label: ref && ref.set.figures.length === 1 ? figureSetTitle(ref.set) : cap(humanize(key.split('.').pop()!)),
        value: ref ? value(ref.figure.value) : '',
        readers: readers.get(key) ?? [],
      };
    }),
  );

  protected formula(it: Item): string {
    return render(it.expr, this.ctx());
  }
  protected auxCaption(it: Item): string {
    if (it.kind === 'attach') return `Attaches ${it.caption}`;
    if (it.kind === 'set') return `Reports ${humanize(it.field ?? it.name)}`;
    return cap(humanize(it.name));
  }
  protected anchorTitle(ref: AnchorRef): string {
    return anchor(ref)?.title ?? ref.anchor;
  }
  protected sourceTitle(ref: AnchorRef): string {
    return sources.get(ref.source)?.title ?? ref.source;
  }

  protected readonly crumbs = computed(() => (this.rule() ? [...ruleCrumbs(this.rule()!), { label: title(this.rule()!) }] : []));

  constructor() {
    pageTitle(() => (this.rule() ? title(this.rule()!) : 'No such rule'));
  }
}

function cap(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
