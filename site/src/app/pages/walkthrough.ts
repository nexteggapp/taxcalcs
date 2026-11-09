import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal } from '@angular/core';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { DerivationNode, Item, anchor, model, rules, sources, slug } from '../model';
import { contextFor, render, title, value } from '../format';
import { Derivation } from '../derivation';
import { Excerpt } from '../excerpt';
import { Links } from '../links';
import { Crumbs, ruleCrumbs } from '../crumbs';
import { pageTitle } from '../page-title';

interface Step {
  node?: DerivationNode;
  before: DerivationNode[];
}

/**
 * A form's structure, with one household's derivation laid over it: each
 * line's value, and the steps that produced it.
 */
@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-walkthrough',
  imports: [RouterLink, Derivation, Excerpt, Links, Crumbs],
  template: `
    @if (rule(); as r) {
      <app-crumbs [items]="crumbs()" />
      <p class="eyebrow">Walkthrough · tax year {{ model.year }}</p>
      <h1>{{ title(r) }}</h1>
      <p class="lede">
        Pick a household to see each line's value and how it was worked out, or look at the form's structure alone.
        Every caption, formula and link below comes from the rules themselves.
      </p>

      <div class="picker" role="radiogroup" aria-label="Household">
        @for (h of households(); track h.id) {
          <button type="button" role="radio" [attr.aria-checked]="h.id === selected()?.id" [class.sel]="h.id === selected()?.id" (click)="pick(h.id)">
            <strong>{{ h.label }}</strong>
            <span class="muted small">Total tax {{ h.totalTax }}</span>
          </button>
        }
        <button type="button" role="radio" [attr.aria-checked]="!selected()" [class.sel]="!selected()" (click)="pick('none')">
          <strong>Structure only</strong>
          <span class="muted small">No household</span>
        </button>
      </div>

      @if (selected(); as h) {
        <div class="household">
          <div>
            <p>{{ h.description }}</p>
            <dl class="facts">
              @for (f of h.facts; track f.label) {
                <div><dt>{{ f.label }}</dt><dd>{{ f.value }}</dd></div>
              }
            </dl>
          </div>
          <div class="totals">
            <div><span class="muted small">Total tax</span><strong>{{ h.totalTax }}</strong></div>
            <div><span class="muted small">Refund</span><strong>{{ h.refund }}</strong></div>
            <div><span class="muted small">Amount owed</span><strong>{{ h.owed }}</strong></div>
          </div>
        </div>
        @for (n of h.notes ?? []; track n) {
          <p class="note">{{ n }}</p>
        }
      }

      <div appLinks>
        @for (p of r.parts ?? []; track $index) {
          @if (p.caption) {
            <h2>{{ p.caption }}</h2>
            <table class="lines walk"><tbody>
              @for (it of lines(p.items); track it.number) {
                <tr class="lrow" [id]="'line-' + it.number" [class.open]="isOpen(it)" [class.zero]="isZero(it)" (click)="toggle(it)">
                  <td class="ln">{{ it.number }}</td>
                  <td>
                    <div class="cap">{{ it.caption }}</div>
                    <div class="formula" [innerHTML]="formula(it)"></div>
                  </td>
                  <td class="val">{{ selected() ? value(step(it)?.node?.value) : '' }}</td>
                  <td class="chev" aria-hidden="true">{{ isOpen(it) ? '▾' : '▸' }}</td>
                </tr>
                @if (isOpen(it)) {
                  <tr class="ldetail">
                    <td></td>
                    <td colspan="3">
                      @if (step(it); as s) {
                        @if (s.before.length) {
                          <h4>Worked out first</h4>
                          <app-derivation [nodes]="s.before" />
                        }
                        @if (s.node?.children?.length) {
                          <h4>How line {{ it.number }} was worked out</h4>
                          <app-derivation [nodes]="s.node!.children!" [open]="true" />
                        }
                      }
                      @if (excerpt(it); as ex) {
                        <h4>Line {{ it.number }} on the form</h4>
                        <app-excerpt [source]="ex.source" [anchor]="ex.anchor" />
                      }
                    </td>
                  </tr>
                }
              }
            </tbody></table>
          }
        }
      </div>
      <p class="muted small">
        The form's rules: <a [routerLink]="['/rules', slug(r.name)]">{{ title(r) }}</a>.
      </p>
    }
  `,
})
export class Walkthrough {
  protected readonly slug = slug;
  private readonly router = inject(Router);

  /** The form, from the route, and the household, from the query. */
  readonly form = input.required<string>();
  readonly household = input<string>();

  protected readonly model = model;
  protected readonly title = title;
  protected readonly value = value;
  private readonly opened = signal<Set<string>>(new Set());
  /** A link to #line-14 arrives with line 14 open. */
  private readonly fragment = toSignal(inject(ActivatedRoute).fragment);
  private readonly openLinked = effect(() => {
    const n = this.fragment()?.match(/^line-(.+)$/)?.[1];
    if (n) this.opened.update((s) => new Set(s).add(n));
  });

  protected readonly rule = computed(() => rules.get(this.form()));
  protected readonly ctx = computed(() => {
    const c = contextFor(this.rule()!);
    c.lineHref = (n) => `#line-${n}`;
    return c;
  });
  /** The sample households followed through this form. */
  protected readonly households = computed(() => model.households.filter((h) => h.form === this.form()));
  protected readonly selected = computed(() => {
    const id = this.household();
    const hs = this.households();
    if (id === 'none') return undefined;
    return hs.find((h) => h.id === id) ?? hs[0];
  });
  protected readonly steps = computed(() => {
    const out = new Map<string, Step>();
    let before: DerivationNode[] = [];
    for (const n of this.selected()?.derivation.children ?? []) {
      if (n.kind === 'line' && n.line) {
        out.set(n.line, { node: n, before });
        before = [];
      } else {
        before.push(n);
      }
    }
    return out;
  });

  protected lines(items: Item[]): Item[] {
    return items.filter((i) => i.kind === 'line');
  }
  protected step(it: Item): Step | undefined {
    return this.steps().get(it.number!);
  }
  protected formula(it: Item): string {
    return render(it.expr, this.ctx());
  }
  protected isZero(it: Item): boolean {
    const v = this.step(it)?.node?.value;
    return !!this.selected() && (v === '0.00' || v === '0' || v === 'false');
  }
  protected isOpen(it: Item): boolean {
    return this.opened().has(it.number!);
  }
  protected toggle(it: Item): void {
    const s = new Set(this.opened());
    if (s.has(it.number!)) s.delete(it.number!);
    else s.add(it.number!);
    this.opened.set(s);
  }
  protected pick(id: string): void {
    this.router.navigate([], { queryParams: { household: id }, replaceUrl: true });
  }
  protected excerpt(it: Item) {
    for (const ref of it.anchors ?? []) {
      const a = anchor(ref);
      if (a?.at?.page) return { source: sources.get(ref.source)!, anchor: a };
    }
    return undefined;
  }

  protected readonly crumbs = computed(() => (this.rule() ? [...ruleCrumbs(this.rule()!), { label: 'Walkthrough' }] : []));

  constructor() {
    pageTitle(() => (this.rule() ? `${title(this.rule()!).split(',')[0]} walkthrough` : 'No such form'));
  }
}
